package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// TestMITMCaptureLocalHTTP proves the full loop without external egress:
// a client routed through the proxy captures a transaction, flags the login
// and extracts the session cookie + JSON tokens. The default clean-capture
// filter is disabled here because the target binds to 127.0.0.1.
func TestMITMCaptureLocalHTTP(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "sessid=abc123; Path=/")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"tok_xyz","user":"bob"}`)
	}))
	defer target.Close()

	p, _ := New("127.0.0.1:8897")
	p.Filter.SetEnabled(false) // target is on localhost; keep it visible
	go func() { _ = p.Listen() }()
	time.Sleep(300 * time.Millisecond)

	proxyURL, _ := url.Parse("http://127.0.0.1:8897")
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
	resp, err := client.Get(target.URL + "/login")
	if err != nil {
		t.Fatalf("client get via proxy: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	time.Sleep(150 * time.Millisecond)
	recs := p.Store.List()
	if len(recs) == 0 {
		t.Fatal("expected at least one captured record")
	}
	rec := recs[len(recs)-1]
	if !rec.IsLogin {
		t.Errorf("expected login detection for /login, got IsLogin=%v", rec.IsLogin)
	}
	foundCookie, foundToken := false, false
	for _, tk := range rec.Tokens {
		if tk.Key == "Cookie:sessid" && tk.Value == "abc123" {
			foundCookie = true
		}
		if tk.Key == "access_token" && tk.Value == "tok_xyz" {
			foundToken = true
		}
	}
	if !foundCookie {
		t.Errorf("session cookie not extracted: %+v", rec.Tokens)
	}
	if !foundToken {
		t.Errorf("access_token not extracted: %+v", rec.Tokens)
	}
}

// TestUpstreamHTTP2Enabled verifies the v2 fix: the shared upstream transport
// negotiates HTTP/2 to the origin instead of downgrading to HTTP/1.1.
func TestUpstreamHTTP2Enabled(t *testing.T) {
	p, _ := New("127.0.0.1:8894")
	tr, ok := p.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", p.client.Transport)
	}
	if !tr.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 should be true")
	}
	if tr.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig must be set to enable H2")
	}
	hasH2 := false
	for _, np := range tr.TLSClientConfig.NextProtos {
		if np == "h2" {
			hasH2 = true
		}
	}
	if !hasH2 {
		t.Errorf("NextProtos should include h2, got %v", tr.TLSClientConfig.NextProtos)
	}
}

// TestLargeBodyCaptured verifies the v2 fix for the 2 MiB ceiling: a response
// larger than 2 MiB is still fully captured (in memory at this size) and the
// authoritative size is recorded.
func TestLargeBodyCaptured(t *testing.T) {
	big := bytes.Repeat([]byte("yami-capture-large-body-padding-"), 1<<18) // ~4 MiB
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(big)
	}))
	defer target.Close()

	p, _ := New("127.0.0.1:8893")
	p.Filter.SetEnabled(false)
	go func() { _ = p.Listen() }()
	time.Sleep(300 * time.Millisecond)

	proxyURL, _ := url.Parse("http://127.0.0.1:8893")
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
	resp, err := client.Get(target.URL + "/big")
	if err != nil {
		t.Fatalf("get big body: %v", err)
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if n < 2<<20 {
		t.Fatalf("client did not receive the full large body: got %d bytes", n)
	}

	time.Sleep(200 * time.Millisecond)
	recs := p.Store.List()
	if len(recs) == 0 {
		t.Fatal("large body was not captured")
	}
	rec := recs[len(recs)-1]
	if rec.RespBodySize < 2<<20 {
		t.Errorf("RespBodySize should exceed 2 MiB, got %d", rec.RespBodySize)
	}
	if rec.RespBody == "" && rec.RespBodyFile == "" {
		t.Error("large body was neither kept in memory nor spilled to disk")
	}
}

// TestFilterDropsLocalhost verifies the default clean-capture filter hides
// yami-UA's own local machinery while still forwarding the traffic.
func TestFilterDropsLocalhost(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	}))
	defer target.Close()

	p, _ := New("127.0.0.1:8892")
	// filter is ON by default
	go func() { _ = p.Listen() }()
	time.Sleep(300 * time.Millisecond)

	proxyURL, _ := url.Parse("http://127.0.0.1:8892")
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
	resp, err := client.Get(target.URL + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	time.Sleep(150 * time.Millisecond)
	if len(p.Store.List()) != 0 {
		t.Fatalf("localhost traffic should be filtered out, got %d records", len(p.Store.List()))
	}

	// turn the filter off -> it should now be captured
	p.Filter.SetEnabled(false)
	resp, err = client.Get(target.URL + "/")
	if err != nil {
		t.Fatalf("get (filter off): %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	time.Sleep(150 * time.Millisecond)
	if len(p.Store.List()) == 0 {
		t.Fatal("expected a capture after disabling the filter")
	}
}
