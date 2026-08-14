package com.yamiua.app

import android.net.VpnService
import java.io.FileInputStream
import java.io.FileOutputStream
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.Inet4Address
import java.net.InetSocketAddress
import java.net.Socket
import java.nio.ByteBuffer
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors

/**
 * v2 DEMO — global (all-app) capture via VpnService.
 *
 * This is a lightweight TUN -> local-proxy forwarder. It is intentionally a
 * demonstration of the "capture every app, not just the WebView" path; the
 * WebView-only path remains the supported default. Architecture:
 *
 *   app TCP/UDP --> TUN (fd) --> VpnForwarder
 *                                      |
 *      DNS query  --> forward to real resolver, learn name<->ip
 *      TCP stream --> CONNECT <host:port> to local yami MITM proxy (127.0.0.1:8899)
 *                     then NAT-splice the bytes both ways (forged IP/TCP)
 *
 * yami-UA itself is excluded from the VPN (addDisallowedApplication), so the
 * proxy's own egress never loops back through the TUN.
 *
 * NOTE: unverified on a device in the build sandbox; validate on-device.
 */
class VpnForwarder(
    private val fd: java.io.FileDescriptor,
    private val proxyHost: String,
    private val proxyPort: Int,
    private val service: VpnService
) : Runnable {

    private val `in` = FileInputStream(fd)
    private val out = FileOutputStream(fd)
    private val mtu = 1500
    private val executor = Executors.newCachedThreadPool()

    // name -> ip  and  ip -> name  learned from DNS answers
    private val nameToIp = ConcurrentHashMap<String, Int>()
    private val ipToName = ConcurrentHashMap<Int, String>()

    // active TCP connections keyed by (srcIp,srcPort,dstIp,dstPort)
    private val conns = ConcurrentHashMap<ConnKey, Connection>()
    private var stopped = false

    // virtual DNS server we advertised in the VPN builder
    private val dnsServerIp = ipToInt("10.0.0.1")

    override fun run() {
        val buf = ByteArray(mtu)
        while (!stopped) {
            val n = try {
                `in`.read(buf)
            } catch (_: Exception) {
                break
            }
            if (n <= 0) break
            try {
                handlePacket(buf, n)
            } catch (_: Exception) {
                // drop malformed packet, keep the tunnel alive
            }
        }
    }

    fun stop() {
        stopped = true
        conns.values.forEach { it.close() }
        conns.clear()
    }

    // ---------------- packet dispatch ----------------

    private fun handlePacket(pkt: ByteArray, len: Int) {
        if (len < 20) return
        val verIhl = pkt[0].toInt() and 0xff
        val version = verIhl shr 4
        if (version != 4) return
        val ihl = (verIhl and 0x0f) * 4
        if (len < ihl + 1) return
        val proto = pkt[9].toInt() and 0xff
        val srcIp = readInt(pkt, 12)
        val dstIp = readInt(pkt, 16)
        when (proto) {
            6 -> handleTcp(pkt, ihl, len, srcIp, dstIp)
            17 -> handleUdp(pkt, ihl, len, srcIp, dstIp)
        }
    }

    // ---------------- TCP ----------------

    private fun handleTcp(pkt: ByteArray, ihl: Int, len: Int, srcIp: Int, dstIp: Int) {
        val off = ihl
        if (len < off + 20) return
        val srcPort = (pkt[off].toInt() and 0xff shl 8) or (pkt[off + 1].toInt() and 0xff)
        val dstPort = (pkt[off + 2].toInt() and 0xff shl 8) or (pkt[off + 3].toInt() and 0xff)
        val seq = readInt(pkt, off + 4).toLong() and 0xffffffffL
        val ack = readInt(pkt, off + 8).toLong() and 0xffffffffL
        val dataOff = (pkt[off + 12].toInt() and 0xf0) shr 4
        val flags = pkt[off + 13].toInt() and 0xff
        val syn = (flags and 0x02) != 0
        val ackF = (flags and 0x10) != 0
        val fin = (flags and 0x01) != 0
        val payloadLen = len - off - dataOff * 4
        val payload = if (payloadLen > 0) pkt.copyOfRange(off + dataOff * 4, len) else ByteArray(0)

        val key = ConnKey(srcIp, srcPort, dstIp, dstPort)
        var conn = conns[key]

        if (syn && conn == null) {
            conn = openConnection(key, dstIp, dstPort, seq)
            if (conn == null) return
            conns[key] = conn
            // SYN-ACK: seq = our ISN (serverSeq - 1), ack = client seq + 1
            sendTcp(conn, (conn.serverSeq - 1) and 0xffffffffL, seq + 1, syn = true, ackF = true, payload = ByteArray(0))
            return
        }
        if (conn == null) return

        conn.clientSeq = seq + payload.size
        if (fin) {
            conn.clientSeq += 1
            sendTcp(conn, conn.serverSeq, conn.clientSeq, syn = false, ackF = true, fin = true, payload = ByteArray(0))
            conn.close()
            conns.remove(key)
            return
        }
        if (payload.isNotEmpty()) {
            conn.queue.add(payload)
            // ACK the data we just received
            sendTcp(conn, conn.serverSeq, conn.clientSeq, syn = false, ackF = true, payload = ByteArray(0))
            pumpToProxy(conn)
        } else if (ackF) {
            // pure ACK
        }
    }

    private fun openConnection(key: ConnKey, dstIp: Int, dstPort: Int, clientSeq: Long): Connection? {
        val host = ipToName[dstIp] ?: intToIp(dstIp)
        return try {
            val sock = Socket()
            sock.connect(InetSocketAddress(proxyHost, proxyPort), 8000)
            val os = sock.getOutputStream()
            val req = "CONNECT $host:$dstPort HTTP/1.1\r\nHost: $host:$dstPort\r\n\r\n"
            os.write(req.toByteArray())
            val ins = sock.getInputStream()
            val hdr = ByteArray(256)
            var total = 0
            while (total < hdr.size) {
                val r = ins.read(hdr, total, hdr.size - total)
                if (r < 0) break
                total += r
                if (hdr.copyOfRange(0, total).toString(Charsets.US_ASCII).contains("\r\n\r\n")) break
            }
            if (!hdr.copyOfRange(0, total).toString(Charsets.US_ASCII).contains(" 200")) {
                sock.close()
                return null
            }
            val isn = (System.nanoTime() and 0x7fffffff).toInt().toLong() and 0xffffffffL
            // serverSeq is the NEXT sequence number to send; the SYN itself
            // consumes one number, so the first data/ACK uses isn+1.
            val conn = Connection(key, sock, os, clientSeq + 1, (isn + 1) and 0xffffffffL)
            // proxy -> app direction
            executor.execute {
                try {
                    val bin = sock.getInputStream()
                    val buf = ByteArray(8192)
                    while (!conn.closed) {
                        val r = bin.read(buf)
                        if (r <= 0) break
                        var off = 0
                        while (off < r) {
                            val seg = kotlin.math.min(1460, r - off)
                            sendTcp(conn, conn.serverSeq, conn.clientSeq, syn = false, ackF = true,
                                payload = buf.copyOfRange(off, off + seg))
                            conn.serverSeq = (conn.serverSeq + seg) and 0xffffffffL
                            off += seg
                        }
                    }
                } catch (_: Exception) {
                }
                // proxy closed -> FIN to app
                try {
                    sendTcp(conn, conn.serverSeq, conn.clientSeq, syn = false, ackF = true, fin = true, payload = ByteArray(0))
                } catch (_: Exception) {
                }
            }
            conn
        } catch (_: Exception) {
            null
        }
    }

    private fun pumpToProxy(conn: Connection) {
        while (conn.queue.isNotEmpty()) {
            val p = conn.queue.poll() ?: break
            try {
                conn.out.write(p)
                conn.out.flush()
            } catch (_: Exception) {
                conn.close()
                conns.remove(conn.key)
                return
            }
        }
    }

    // ---------------- UDP / DNS ----------------

    private fun handleUdp(pkt: ByteArray, ihl: Int, len: Int, srcIp: Int, dstIp: Int) {
        val off = ihl
        if (len < off + 8) return
        val srcPort = (pkt[off].toInt() and 0xff shl 8) or (pkt[off + 1].toInt() and 0xff)
        val dstPort = (pkt[off + 2].toInt() and 0xff shl 8) or (pkt[off + 3].toInt() and 0xff)
        val udpLen = (pkt[off + 4].toInt() and 0xff shl 8) or (pkt[off + 5].toInt() and 0xff)
        val payload = pkt.copyOfRange(off + 8, off + udpLen)

        if (dstIp == dnsServerIp && dstPort == 53) {
            forwardDns(payload, srcIp, srcPort)
        }
    }

    private fun forwardDns(query: ByteArray, clientIp: Int, clientPort: Int) {
        try {
            val sock = DatagramSocket()
            sock.soTimeout = 5000
            val realDns = InetSocketAddress("223.5.5.5", 53)
            sock.send(DatagramPacket(query, query.size, realDns))
            val resp = ByteArray(1024)
            val rp = DatagramPacket(resp, resp.size)
            sock.receive(rp)
            val rlen = rp.length
            learnDnsNames(query, resp.copyOf(rlen))
            // forge UDP reply from our DNS server to the app
            sendUdp(dnsServerIp, 53, clientIp, clientPort, resp.copyOf(rlen))
            sock.close()
        } catch (_: Exception) {
        }
    }

    // parse the question name from a DNS message and the A-record answers
    private fun learnDnsNames(query: ByteArray, resp: ByteArray) {
        try {
            val qname = parseName(query, 12) ?: return
            var p = 12 + nameLen(query, 12) + 4 // skip question (name + QTYPE + QCLASS)
            val ancount = (resp[6].toInt() and 0xff shl 8) or (resp[7].toInt() and 0xff)
            for (i in 0 until ancount) {
                if (p + 10 > resp.size) break
                p = skipName(resp, p)
                val type = (resp[p].toInt() and 0xff shl 8) or (resp[p + 1].toInt() and 0xff)
                p += 8 // TYPE(2) CLASS(2) TTL(4)
                val rdlen = (resp[p].toInt() and 0xff shl 8) or (resp[p + 1].toInt() and 0xff)
                p += 2
                if (type == 1 && p + 4 <= resp.size) { // A record
                    val ip = readInt(resp, p)
                    ipToName[ip] = qname
                    nameToIp[qname] = ip
                }
                p += rdlen
            }
        } catch (_: Exception) {
        }
    }

    // ---------------- packet forging ----------------

    // Build a TCP segment (20-byte header + payload) and emit it as a forged
    // IPv4 packet: src = the real server (original dst), dst = the app.
    private fun sendTcp(conn: Connection, seq: Long, ack: Long, syn: Boolean, ackF: Boolean, fin: Boolean, payload: ByteArray) {
        val tcpTotal = 20 + payload.size
        val seg = ByteArray(tcpTotal)
        writeShort(seg, 0, conn.key.dstPort) // src port = original destination port
        writeShort(seg, 2, conn.key.srcPort) // dst port = app port
        writeInt(seg, 4, seq.toInt())
        writeInt(seg, 8, ack.toInt())
        seg[12] = 0x50.toByte() // data offset = 5 (no options)
        var flags = 0x10 // PSH (we always carry/ack data)
        if (syn) flags = flags or 0x02
        if (ackF) flags = flags or 0x10
        if (fin) flags = flags or 0x01
        seg[13] = flags.toByte()
        writeShort(seg, 14, 65535) // window
        writeShort(seg, 16, 0)     // checksum (filled below)
        if (payload.isNotEmpty()) System.arraycopy(payload, 0, seg, 20, payload.size)
        val csum = tcpChecksum(conn.key.dstIp, conn.key.srcIp, seg)
        writeShort(seg, 16, csum)
        sendIp(conn.key.srcIp, conn.key.dstIp, seg, 6)
    }

    private fun sendUdp(srcIp: Int, srcPort: Int, dstIp: Int, dstPort: Int, payload: ByteArray) {
        val udpLen = 8 + payload.size
        val seg = ByteArray(udpLen)
        writeShort(seg, 0, srcPort)
        writeShort(seg, 2, dstPort)
        writeShort(seg, 4, udpLen)
        writeShort(seg, 6, 0) // checksum (filled below)
        System.arraycopy(payload, 0, seg, 8, payload.size)
        val csum = udpChecksum(srcIp, dstIp, seg)
        writeShort(seg, 6, csum)
        sendIp(dstIp, srcIp, seg, 17)
    }

    // Wrap a transport segment into an IPv4 packet and write it to the TUN.
    private fun sendIp(dstIp: Int, srcIp: Int, seg: ByteArray, protocol: Int) {
        val ihl = 20
        val total = ihl + seg.size
        val pkt = ByteArray(total)
        pkt[0] = 0x45.toByte()
        pkt[1] = 0
        writeShort(pkt, 2, total)
        writeShort(pkt, 4, 0)       // identification
        writeShort(pkt, 6, 0x4000)  // don't fragment
        pkt[8] = 64                 // ttl
        pkt[9] = protocol.toByte()
        writeInt(pkt, 12, srcIp)
        writeInt(pkt, 16, dstIp)
        System.arraycopy(seg, 0, pkt, ihl, seg.size)
        writeShort(pkt, 10, ipChecksum(pkt, ihl))
        synchronized(out) {
            try {
                out.write(pkt)
                out.flush()
            } catch (_: Exception) {
            }
        }
    }

    // ---------------- checksums ----------------

    private fun ipChecksum(buf: ByteArray, len: Int): Int {
        var sum = 0
        var i = 0
        while (i < len) {
            sum += ((buf[i].toInt() and 0xff) shl 8) or (buf[i + 1].toInt() and 0xff)
            i += 2
        }
        sum = (sum and 0xffff) + (sum ushr 16)
        sum = (sum and 0xffff) + (sum ushr 16)
        return (sum.inv() and 0xffff)
    }

    private fun tcpChecksum(srcIp: Int, dstIp: Int, tcp: ByteArray): Int {
        var sum = 0
        sum += (srcIp ushr 16) and 0xffff
        sum += srcIp and 0xffff
        sum += (dstIp ushr 16) and 0xffff
        sum += dstIp and 0xffff
        sum += 6 shl 8 // protocol
        sum += tcp.size
        var i = 0
        while (i < tcp.size) {
            sum += ((tcp[i].toInt() and 0xff) shl 8) or (tcp[i + 1].toInt() and 0xff)
            i += 2
        }
        if (tcp.size % 2 == 1) {
            sum += (tcp[tcp.size - 1].toInt() and 0xff) shl 8
        }
        sum = (sum and 0xffff) + (sum ushr 16)
        sum = (sum and 0xffff) + (sum ushr 16)
        return (sum.inv() and 0xffff)
    }

    private fun udpChecksum(srcIp: Int, dstIp: Int, udp: ByteArray): Int {
        var sum = 0
        sum += (srcIp ushr 16) and 0xffff
        sum += srcIp and 0xffff
        sum += (dstIp ushr 16) and 0xffff
        sum += dstIp and 0xffff
        sum += 17 shl 8
        sum += udp.size
        var i = 0
        while (i < udp.size) {
            sum += ((udp[i].toInt() and 0xff) shl 8) or (udp[i + 1].toInt() and 0xff)
            i += 2
        }
        if (udp.size % 2 == 1) {
            sum += (udp[udp.size - 1].toInt() and 0xff) shl 8
        }
        sum = (sum and 0xffff) + (sum ushr 16)
        sum = (sum and 0xffff) + (sum ushr 16)
        return (sum.inv() and 0xffff)
    }

    // ---------------- DNS name parsing helpers ----------------

    private fun parseName(msg: ByteArray, off: Int): String? {
        val sb = StringBuilder()
        var p = off
        while (p < msg.size) {
            val len = msg[p].toInt() and 0xff
            if (len == 0) return sb.toString().trimEnd('.')
            if ((len and 0xc0) == 0xc0) break // compression not expected here
            p++
            if (sb.isNotEmpty()) sb.append('.')
            sb.append(String(msg, p, len, Charsets.US_ASCII))
            p += len
        }
        return sb.toString().trimEnd('.')
    }

    private fun nameLen(msg: ByteArray, off: Int): Int {
        var p = off
        while (p < msg.size) {
            val len = msg[p].toInt() and 0xff
            if (len == 0) return p - off + 1
            if ((len and 0xc0) == 0xc0) return p - off + 2
            p += len + 1
        }
        return 1
    }

    private fun skipName(msg: ByteArray, off: Int): Int {
        var p = off
        while (p < msg.size) {
            val len = msg[p].toInt() and 0xff
            if (len == 0) return p + 1
            if ((len and 0xc0) == 0xc0) return p + 2
            p += len + 1
        }
        return p + 1
    }

    // ---------------- byte utils ----------------

    private fun readInt(b: ByteArray, off: Int): Int =
        ((b[off].toInt() and 0xff) shl 24) or ((b[off + 1].toInt() and 0xff) shl 16) or
                ((b[off + 2].toInt() and 0xff) shl 8) or (b[off + 3].toInt() and 0xff)

    private fun writeInt(b: ByteArray, off: Int, v: Int) {
        b[off] = (v ushr 24).toByte()
        b[off + 1] = (v ushr 16).toByte()
        b[off + 2] = (v ushr 8).toByte()
        b[off + 3] = v.toByte()
    }

    private fun writeShort(b: ByteArray, off: Int, v: Int) {
        b[off] = (v ushr 8).toByte()
        b[off + 1] = v.toByte()
    }

    private fun ipToInt(s: String): Int {
        val parts = s.split('.')
        var v = 0
        for (p in parts) v = (v shl 8) or (p.toInt() and 0xff)
        return v
    }

    private fun intToIp(v: Int): String =
        "${(v ushr 24) and 0xff}.${(v ushr 16) and 0xff}.${(v ushr 8) and 0xff}.${v and 0xff}"

    data class ConnKey(val srcIp: Int, val srcPort: Int, val dstIp: Int, val dstPort: Int)

    class Connection(
        val key: ConnKey,
        private val sock: Socket,
        val out: java.io.OutputStream,
        var clientSeq: Long,
        var serverSeq: Long
    ) {
        val queue = java.util.concurrent.ConcurrentLinkedQueue<ByteArray>()
        @Volatile var closed = false
        fun close() {
            closed = true
            try { sock.close() } catch (_: Exception) {}
        }
    }
}
