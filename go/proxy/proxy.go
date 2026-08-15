// Package proxy implements a lightweight HTTP/HTTPS MITM capture proxy.
//
// Flow:
//   - The Android WebView is pointed at 127.0.0.1:<proxy port> as its proxy.
//   - Plain HTTP requests are forwarded directly.
//   - CONNECT tunnels are terminated by us: we present a leaf certificate
//     signed by our in-memory CA, decrypt the inner request, forward it to
//     the real server (over HTTP/2 when the origin supports it), capture +
//     optionally modify, then relay the response.
//
// v2 design notes:
//   - Upstream transport is HTTP/2-capable (ALPN h2) so modern origins are
//     reached over a real H2 connection instead of being silently downgraded
//     to HTTP/1.1.
//   - The inner MITM tunnel is served by net/http's own server, which speaks
//     both HTTP/1.1 and HTTP/2 to the client (negotiated via ALPN).
//   - Request and response bodies are streamed through a tee sink: the full
//     body is always forwarded to the client, while a bounded copy (memory +
//     spill-to-disk) is captured — there is no 2 MiB ceiling on capture.
//   - A default capture filter drops yami-UA's own machinery (localhost API,
//     AI relay, proxy core) and known AI/relay provider hosts, so the capture
//     list stays clean by default.
package proxy

import (
	"fmt"

	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yamiua/ca"
)

const (
	memBodyLimit  = 8 << 20   // 8 MiB kept in memory for preview / token scan
	diskBodyLimit = 256 << 20 // up to 256 MiB fully captured to disk (spill)
)

// Proxy is the MITM capture engine.
type Proxy struct {
	CA     *ca.CA
	Store  *Store
	Mods   *ModifyRules
	Filter *Filter
	Rules  *ProxyRules // enhancement rules: domain filter, block, map, decrypt

	addr     string
	upstream string // optional upstream proxy (http/socks5); "" = direct
	bodyDir  string // where large captured bodies are spilled

	client *http.Client // shared, HTTP/2-capable upstream transport
}

// New constructs a Proxy listening on addr (e.g. "127.0.0.1:8899").
func New(addr string) (*Proxy, error) {
	ca, err := ca.NewCA()
	if err != nil {
		return nil, fmt.Errorf("ca: %w", err)
	}
	p := &Proxy{
		CA:     ca,
		Store:  NewStore(2000),
		Mods:   NewModifyRules(),
		Filter: DefaultFilter(), // clean capture ON by default
		Rules:  NewProxyRules(), // all-pass until configured via /ai/rules
		addr:   addr,
		bodyDir: defaultBodyDir(),
	}
	p.client = newUpstreamClient("")
	return p, nil
}

// CAPEM returns the root CA certificate (PEM) for the user to install.
func (p *Proxy) CAPEM() string { return p.CA.CertPEM() }

// ListenAddr returns the address the proxy is configured to listen on.
func (p *Proxy) ListenAddr() string { return p.addr }

// SetUpstream routes all captured traffic through an upstream proxy
// (http:// or socks5://). Empty string restores direct connection. This is how
// "browsing always walks the proxy" is achieved: the WebView -> yami MITM ->
// user's v2ray/SOCKS5 chain.
func (p *Proxy) SetUpstream(upstream string) {
	p.upstream = upstream
	p.client = newUpstreamClient(upstream)
}

// SetBodyDir overrides where large captured bodies are spilled to disk.
func (p *Proxy) SetBodyDir(dir string) {
	if dir != "" {
		p.bodyDir = dir
	}
}

// SetRules replaces the active enhancement rule set (domain filter, block,
// map, decrypt). Intended to be called from the /ai/rules control-plane route.
func (p *Proxy) SetRules(cfg RulesConfig) error {
	return p.Rules.Set(cfg)
}

// GetRules returns the active enhancement rule set in its JSON form.
func (p *Proxy) GetRules() RulesConfig {
	return p.Rules.Config()
}

// Clear wipes the capture store and removes spilled body files.
func (p *Proxy) Clear() {
	p.Store.Clear()
	if p.bodyDir != "" {
		os.RemoveAll(p.bodyDir)
		os.MkdirAll(p.bodyDir, 0o755)
	}
}

// Listen starts the proxy server (blocking).
func (p *Proxy) Listen() error {
	srv := &http.Server{
		Handler:     http.HandlerFunc(p.serve),
		ReadTimeout: 0,
		WriteTimeout: 0,
		IdleTimeout: 0,
	}
	ln, err := net.Listen("tcp", p.addr)
	if err != nil {
		return err
	}
	log.Printf("[yami] proxy listening on %s", p.addr)
	return srv.Serve(ln)
}

func (p *Proxy) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.mitm(w, r, "http")
}

// handleConnect terminates a CONNECT tunnel and MITMs the inner traffic.
//
// After the "200 Connection Established" line the client starts a TLS session
// to our MITM CA. We hand that single connection to net/http's server, which
// transparently negotiates HTTP/1.1 or HTTP/2 with the client and dispatches
// each inner request to mitm().
func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", 500)
		return
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	defer clientConn.Close()

	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}

	tlsCfg := &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return p.CA.LeafFor(hello.ServerName)
		},
		// Only advertise HTTP/1.1 to the CLIENT. Chromium/WebView's HTTP/2
		// support through a MITM CONNECT tunnel is notoriously flaky — with
		// "h2" advertised, most HTTPS sites fail to load or hang while a few
		// (e.g. passthrough-filtered hosts) happen to work. The upstream side
		// still negotiates h2 independently (see newUpstreamClient).
		NextProtos: []string{"http/1.1"},
	}

	ln := &singleConnListener{conn: clientConn}
	srv := &http.Server{
		TLSConfig:   tlsCfg,
		Handler:     http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) { p.mitm(rw, req, "https") }),
		ReadTimeout: 0,
		WriteTimeout: 0,
		IdleTimeout: 0,
	}
	if err := srv.ServeTLS(ln, "", ""); err != nil {
		log.Printf("[yami] tunnel closed: %v", err)
	}
}

// mitm forwards a single (already decrypted) request, relays the response to
// the client, and captures it. Filtered traffic is forwarded but not captured.
func (p *Proxy) mitm(w http.ResponseWriter, req *http.Request, scheme string) {
	rawURL := absURL(req, scheme, hostOf(req, scheme))

	// 1) Blocking: drop matching requests with a 403, never forwarded.
	if p.Rules != nil && p.Rules.Blocked(rawURL) {
		writeBlocked(w)
		return
	}

	// 2) Mapping: serve a locally-constructed response, never forwarded.
	if p.Rules != nil {
		if m, ok := p.Rules.MatchMap(rawURL); ok {
			writeMapped(w, m)
			return
		}
	}

	if p.Filter != nil && p.Filter.Drop(rawURL, req.Host) {
		p.passthrough(w, req, rawURL)
		return
	}

	// 3) Domain white/black list: excluded hosts are forwarded but not
	//    captured (keeps the store clean). Default config captures all.
	if p.Rules != nil && !p.Rules.ShouldCapture(req.Host) {
		p.passthrough(w, req, rawURL)
		return
	}

	// Stream the request body to the origin while teeing a copy into a sink.
	reqSink := newBodySink(p.bodyDir)
	origLen := req.ContentLength
	if req.Body != nil {
		req.Body = io.NopCloser(io.TeeReader(req.Body, reqSink))
	}

	resp, err := p.roundTrip(req, rawURL, origLen)
	if err != nil {
		http.Error(w, "yami upstream error: "+err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	// Stream the response body to the client while teeing a copy into a sink.
	respSink := newBodySink(p.bodyDir)
	resp.Body = io.NopCloser(io.TeeReader(resp.Body, respSink))

	reqHeaders := headerDump(req.Header)
	respHeaders := headerDump(resp.Header)

	// Relay to the client first — this fills respSink (and reqSink, already
	// filled while the origin read the request).
	writeResponse(w, resp)

	// Capture is best-effort and MUST never affect the already-relayed
	// response. Any panic here (store, regex, decrypt, sinks) is contained so
	// the tunnel stays healthy for the next request.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[yami] capture panic (ignored): %v", r)
		}
	}()

	// Now capture from the sinks.
	reqBody := reqSink.Preview()
	respBody := respSink.Preview()

	// Best-effort AES decryption of the response body for display only.
	// The relayed stream above is untouched; a decrypt failure leaves the
	// original body in place.
	if p.Rules != nil {
		respBody = p.Rules.DecryptBody(rawURL, respBody)
	}

	isLogin := detectLogin(req.Method, rawURL, reqHeaders, respHeaders, respBody)
	toks := ExtractTokens(&Record{
		Method: req.Method, URL: rawURL, Host: req.Host,
		ReqHeaders: reqHeaders, ReqBody: reqBody,
		StatusCode: resp.StatusCode, RespHeaders: respHeaders, RespBody: respBody,
		IsHTTPS: scheme == "https", IsLogin: isLogin,
	})
	p.Store.Add(&Record{
		Method: req.Method, URL: rawURL, Host: req.Host,
		ReqHeaders: reqHeaders, ReqBody: reqBody, ReqBodyFile: reqSink.File(),
		StatusCode: resp.StatusCode, RespHeaders: respHeaders, RespBody: respBody,
		RespBodyFile: respSink.File(), RespBodySize: respSink.Size(),
		IsHTTPS: scheme == "https", IsLogin: isLogin, Time: time.Now(), Tokens: toks,
	})
}

// passthrough forwards filtered traffic without capturing it.
func (p *Proxy) passthrough(w http.ResponseWriter, req *http.Request, rawURL string) {
	resp, err := p.roundTrip(req, rawURL, req.ContentLength)
	if err != nil {
		http.Error(w, "yami upstream error: "+err.Error(), 502)
		return
	}
	defer resp.Body.Close()
	writeResponse(w, resp)
}

// roundTrip builds an upstream request and executes it via the shared,
// HTTP/2-capable client. Hop-by-hop headers are stripped.
func (p *Proxy) roundTrip(req *http.Request, rawURL string, contentLen int64) (*http.Response, error) {
	up, err := http.NewRequest(req.Method, rawURL, req.Body)
	if err != nil {
		return nil, err
	}
	copyHeaders(up.Header, req.Header)
	p.Mods.ApplyRequest(up)
	up.Host = req.Host
	up.ContentLength = contentLen
	return p.client.Do(up)
}

// writeResponse relays a response to an http.ResponseWriter, stripping
// hop-by-hop headers so it is valid on both HTTP/1.1 and HTTP/2.
func writeResponse(w http.ResponseWriter, resp *http.Response) {
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		if shouldDropHop(k) {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

var hopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive",
	"Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer",
	"Transfer-Encoding",
}

func shouldDropHop(k string) bool {
	k = strings.ToLower(k)
	for _, h := range hopHeaders {
		if k == strings.ToLower(h) {
			return true
		}
	}
	return false
}

func absURL(req *http.Request, scheme, host string) string {
	if req.URL != nil && req.URL.IsAbs() {
		return req.URL.String()
	}
	u := scheme + "://" + host
	if req.URL != nil {
		u += req.URL.Path
		if req.URL.RawQuery != "" {
			u += "?" + req.URL.RawQuery
		}
	}
	return u
}

func hostOf(req *http.Request, scheme string) string {
	if req.Host != "" {
		return req.Host
	}
	if req.URL != nil && req.URL.Host != "" {
		return req.URL.Host
	}
	return ""
}

// singleConnListener adapts one already-accepted connection into a
// net.Listener so http.Server.ServeTLS can serve a single CONNECT tunnel.
type singleConnListener struct {
	conn net.Conn
	done bool
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	if l.done {
		return nil, io.EOF
	}
	l.done = true
	return l.conn, nil
}

func (l *singleConnListener) Close() error { return nil }

func (l *singleConnListener) Addr() net.Addr {
	if l.conn != nil {
		return l.conn.LocalAddr()
	}
	return dummyAddr{}
}

type dummyAddr struct{}

func (dummyAddr) Network() string { return "tcp" }
func (dummyAddr) String() string  { return "127.0.0.1:0" }

// newUpstreamClient builds a shared HTTP client whose transport negotiates
// HTTP/2 to the origin (ALPN h2) and pools connections.
func newUpstreamClient(upstream string) *http.Client {
	t := &http.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos:         []string{"h2", "http/1.1"}, // enable H2 to origin
			InsecureSkipVerify: false,
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if upstream != "" {
		if u, err := url.Parse(upstream); err == nil {
			t.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Transport: t, Timeout: 0, CheckRedirect: nil}
}

func defaultBodyDir() string {
	return filepath.Join(os.TempDir(), "yami-ua-bodies")
}
