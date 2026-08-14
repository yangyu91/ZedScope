package proto

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// shared test helpers -------------------------------------------------------

func mustJSON(t *testing.T, m map[string]interface{}) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func hasAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// dispatcher ----------------------------------------------------------------

func TestParseEmpty(t *testing.T) {
	if _, err := Parse("   "); err == nil {
		t.Fatal("expected ErrEmptyLink for blank link")
	}
}

func TestParseUnsupported(t *testing.T) {
	_, err := Parse("http://example.com")
	if !errors.Is(err, ErrUnsupportedScheme) {
		t.Fatalf("expected ErrUnsupportedScheme, got %v", err)
	}
}

func TestParseSchemeDispatch(t *testing.T) {
	cases := []struct {
		link string
		want string
	}{
		{"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"1.2.3.4","port":"1","id":"u"}`)), "vmess"},
		{"vless://u@example.com:1", "vless"},
		{"trojan://p@example.com:1", "trojan"},
		{"ss://aes-256-gcm:p@1.1.1.1:8388", "shadowsocks"},
		{"socks5://1.1.1.1:1080", "socks"},
		{"socks://1.1.1.1:1080", "socks"},
	}
	for _, c := range cases {
		cfg, err := Parse(c.link)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.link, err)
		}
		if cfg.Protocol != c.want {
			t.Fatalf("scheme %q -> protocol %q, want %q", c.link, cfg.Protocol, c.want)
		}
	}
}
