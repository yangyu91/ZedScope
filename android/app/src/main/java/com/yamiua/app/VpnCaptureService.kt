package com.yamiua.app

import android.content.Intent
import android.net.VpnService
import java.io.FileDescriptor

/**
 * v2 DEMO — global (all-app) capture via VpnService.
 *
 * Establishes a TUN, excludes yami-UA itself from the VPN (so the local MITM
 * proxy's egress never loops back), and hands the TUN fd to [VpnForwarder],
 * which relays every app's TCP through the local proxy and proxies DNS.
 *
 * The WebView-only capture path remains the supported default; this service is
 * an opt-in demonstration of whole-device capture. Validate on-device.
 */
class VpnCaptureService : VpnService() {

    private var forwarder: VpnForwarder? = null
    private var thread: Thread? = null
    private var fd: FileDescriptor? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val proxy = intent?.getStringExtra("proxy") ?: "127.0.0.1:8899"
        val host = proxy.substringBefore(":").ifBlank { "127.0.0.1" }
        val port = proxy.substringAfter(":").toIntOrNull() ?: 8899

        val builder = Builder()
            .setSession("yami-UA 全局抓包")
            .addAddress("10.0.0.2", 24)
            .addRoute("0.0.0.0", 0)
            .addDnsServer("10.0.0.1")
            .setMtu(1500)
            .allowFamily(android.system.OsConstants.AF_INET)
        try {
            // exclude our own app so the proxy/AI egress doesn't loop
            builder.addDisallowedApplication(packageName)
        } catch (_: Exception) {
        }

        val established = builder.establish()
        if (established == null) {
            stopSelf()
            return START_NOT_STICKY
        }
        fd = established.fileDescriptor
        forwarder = VpnForwarder(established.fileDescriptor, host, port, this)
        thread = Thread(forwarder, "yami-vpn").also { it.start() }
        return START_STICKY
    }

    override fun onDestroy() {
        forwarder?.stop()
        thread?.interrupt()
        try {
            fd?.close()
        } catch (_: Exception) {
        }
        super.onDestroy()
    }
}
