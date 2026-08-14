package proxy

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProxyRulesBlocked(t *testing.T) {
	pr := NewProxyRules()
	if err := pr.Set(RulesConfig{Block: []BlockRule{{URLRegex: `ads\.example\.com`}}}); err != nil {
		t.Fatalf("set rules: %v", err)
	}
	if !pr.Blocked("https://ads.example.com/banner") {
		t.Error("ads.example.com should be blocked")
	}
	if pr.Blocked("https://api.example.com/data") {
		t.Error("api.example.com should NOT be blocked")
	}
}

func TestProxyRulesSetRejectsBadRegex(t *testing.T) {
	pr := NewProxyRules()
	if err := pr.Set(RulesConfig{Block: []BlockRule{{URLRegex: "([a-z"}}}); err == nil {
		t.Error("expected error for invalid block regex")
	}
}

func TestDomainFilter(t *testing.T) {
	pr := NewProxyRules()

	// No rules -> everything captured.
	pr.Set(RulesConfig{})
	if !pr.ShouldCapture("anything.example.com") {
		t.Error("empty config should capture all")
	}

	// Exclude list.
	pr.Set(RulesConfig{DomainExclude: []string{"tracker.com", "*.ads.net"}})
	if pr.ShouldCapture("tracker.com") {
		t.Error("tracker.com should be excluded")
	}
	if pr.ShouldCapture("a.ads.net") {
		t.Error("subdomain of *.ads.net should be excluded")
	}
	if !pr.ShouldCapture("api.example.com") {
		t.Error("non-excluded host should be captured")
	}

	// Include list (takes precedence: only matching hosts captured).
	pr.Set(RulesConfig{DomainInclude: []string{"*.example.com"}})
	if !pr.ShouldCapture("api.example.com") {
		t.Error("api.example.com should match include")
	}
	if pr.ShouldCapture("evil.other.com") {
		t.Error("non-matching host should be dropped by include")
	}
}

func TestDecryptBodyAESCBC(t *testing.T) {
	key := []byte("0123456789abcdef") // 16 bytes -> aes-128
	iv := []byte("abcdef0123456789")  // 16 bytes
	plain := []byte("hello yami secret")

	padded := pkcs7Pad(plain)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)
	enc := base64.StdEncoding.EncodeToString(ct)

	pr := NewProxyRules()
	if err := pr.Set(RulesConfig{Decrypt: []DecryptRule{
		{URLRegex: "secret", Key: hex.EncodeToString(key), IV: hex.EncodeToString(iv),
			Algo: "aes-128-cbc", Encoding: "base64"},
	}}); err != nil {
		t.Fatal(err)
	}
	out := pr.DecryptBody("https://x.com/secret", enc)
	if out != string(plain) {
		t.Fatalf("decrypt mismatch: got %q want %q", out, plain)
	}
}

func TestDecryptBodyBestEffort(t *testing.T) {
	pr := NewProxyRules()
	pr.Set(RulesConfig{Decrypt: []DecryptRule{
		{URLRegex: ".*", Key: "deadbeef", IV: "feedface", Algo: "aes-128-cbc", Encoding: "base64"},
	}})
	// Garbage body must be returned unchanged and must not panic.
	in := "not-valid-base64-###"
	if out := pr.DecryptBody("https://x.com/any", in); out != in {
		t.Fatalf("expected unchanged body on decrypt failure, got %q", out)
	}
}

// TestBlockIntegration proves the block rule is enforced inside the real mitm
// flow: a blocked URL returns 403 and is never forwarded to the target.
func TestBlockIntegration(t *testing.T) {
	hit := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit <- struct{}{}
		io.WriteString(w, "real response")
	}))
	defer target.Close()

	p := New("127.0.0.1:8898")
	p.Filter.SetEnabled(false)
	if err := p.SetRules(RulesConfig{Block: []BlockRule{{URLRegex: `blocked\.test`}}}); err != nil {
		t.Fatal(err)
	}
	go func() { _ = p.Listen() }()
	time.Sleep(300 * time.Millisecond)

	proxyURL, _ := url.Parse("http://127.0.0.1:8898")
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

	// blocked URL
	resp, err := client.Get("http://blocked.test/foo")
	if err != nil {
		t.Fatalf("blocked request error: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	if strings.Contains(string(body), "real response") {
		t.Error("blocked request must not reach the target")
	}
	select {
	case <-hit:
		t.Error("target was hit for a blocked URL")
	case <-time.After(200 * time.Millisecond):
	}

	// non-blocked URL still works
	resp, err = client.Get(target.URL + "/ok")
	if err != nil {
		t.Fatalf("allowed request error: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	select {
	case <-hit:
	case <-time.After(1 * time.Second):
		t.Error("target was not hit for an allowed URL")
	}
}

// pkcs7Pad mirrors the padding used by standard AES-CBC encryptors.
func pkcs7Pad(b []byte) []byte {
	pad := aes.BlockSize - len(b)%aes.BlockSize
	out := make([]byte, len(b)+pad)
	copy(out, b)
	for i := len(b); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}
