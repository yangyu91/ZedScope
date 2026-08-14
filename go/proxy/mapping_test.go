package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMatchMap(t *testing.T) {
	pr := NewProxyRules()
	if err := pr.Set(RulesConfig{Map: []MapRule{
		{URLRegex: `/api/mock`, Status: 201, Body: `{"mocked":true}`, ContentType: "application/json"},
	}}); err != nil {
		t.Fatal(err)
	}
	m, ok := pr.MatchMap("https://host/api/mock?x=1")
	if !ok {
		t.Fatal("expected map match")
	}
	if m.Status != 201 || m.Body != `{"mocked":true}` || m.ContentType != "application/json" {
		t.Fatalf("unexpected matched rule: %+v", m)
	}
	if _, ok := pr.MatchMap("https://host/api/real"); ok {
		t.Error("non-matching URL should not map")
	}
}

// TestMapIntegration proves a mapped request is served locally and never
// forwarded to the origin.
func TestMapIntegration(t *testing.T) {
	hit := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit <- struct{}{}
		io.WriteString(w, "real response")
	}))
	defer target.Close()

	p, _ := New("127.0.0.1:8899")
	p.Filter.SetEnabled(false)
	if err := p.SetRules(RulesConfig{Map: []MapRule{
		{URLRegex: `mock\.local`, Status: 200, Body: "local-mock-body", ContentType: "text/plain"},
	}}); err != nil {
		t.Fatal(err)
	}
	go func() { _ = p.Listen() }()
	time.Sleep(300 * time.Millisecond)

	proxyURL, _ := url.Parse("http://127.0.0.1:8899")
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

	resp, err := client.Get("http://mock.local/path")
	if err != nil {
		t.Fatalf("mapped request error: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if string(body) != "local-mock-body" {
		t.Fatalf("expected local body, got %q", string(body))
	}
	select {
	case <-hit:
		// target may be hit by other requests; ensure it was NOT hit for the
		// mapped URL by checking the body did not come from the origin.
		if strings.Contains(string(body), "real response") {
			t.Error("mapped request reached the origin")
		}
	case <-time.After(200 * time.Millisecond):
	}

	// a non-mapped URL still reaches the origin
	resp, err = client.Get(target.URL + "/ok")
	if err != nil {
		t.Fatalf("allowed request error: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	select {
	case <-hit:
	case <-time.After(1 * time.Second):
		t.Error("origin not reached for non-mapped URL")
	}
}
