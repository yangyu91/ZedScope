package proxy

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseVMess(t *testing.T) {
	// standard vmess v2 json, base64 encoded
	raw := `{"v":"2","ps":"test","add":"1.2.3.4","port":"443","id":"uuid-123","aid":"0","net":"ws","type":"none","host":"edge.com","path":"/wspath","tls":"tls"}`
	link := "vmess://" + base64.StdEncoding.EncodeToString([]byte(raw))
	out, err := ParseShareLink(link)
	if err != nil {
		t.Fatalf("parse vmess: %v", err)
	}
	if out.Protocol != "vmess" {
		t.Fatalf("protocol=%s", out.Protocol)
	}
	var s struct {
		Vnext []struct {
			Address string `json:"address"`
			Port    int    `json:"port"`
		} `json:"vnext"`
	}
	_ = json.Unmarshal(out.Settings, &s)
	if s.Vnext[0].Address != "1.2.3.4" || s.Vnext[0].Port != 443 {
		t.Fatalf("bad vnext: %+v", s.Vnext)
	}
	if !strings.Contains(string(out.StreamSettings), "\"network\":\"ws\"") {
		t.Fatalf("stream settings missing ws: %s", out.StreamSettings)
	}
}

func TestParseVless(t *testing.T) {
	link := "vless://uuid-abc@example.com:8443?type=ws&security=tls&path=%2Fp&host=h.com&flow=xtls-rprx-vision&ps=vl"
	out, err := ParseShareLink(link)
	if err != nil {
		t.Fatalf("parse vless: %v", err)
	}
	if out.Protocol != "vless" {
		t.Fatalf("protocol=%s", out.Protocol)
	}
	if !strings.Contains(string(out.StreamSettings), "\"security\":\"tls\"") {
		t.Fatalf("no tls: %s", out.StreamSettings)
	}
}

func TestParseTrojan(t *testing.T) {
	link := "trojan://secretpw@node.net:443?type=tcp&security=tls&ps=tr"
	out, err := ParseShareLink(link)
	if err != nil {
		t.Fatalf("parse trojan: %v", err)
	}
	if out.Protocol != "trojan" {
		t.Fatalf("protocol=%s", out.Protocol)
	}
	var s struct {
		Servers []struct {
			Address  string `json:"address"`
			Password string `json:"password"`
		} `json:"servers"`
	}
	_ = json.Unmarshal(out.Settings, &s)
	if s.Servers[0].Password != "secretpw" {
		t.Fatalf("bad pw: %+v", s.Servers)
	}
}

func TestParseSS(t *testing.T) {
	// plain userinfo method:password
	link := "ss://aes-256-gcm:passwd@1.1.1.1:8388"
	out, err := ParseShareLink(link)
	if err != nil {
		t.Fatalf("parse ss: %v", err)
	}
	if out.Protocol != "shadowsocks" {
		t.Fatalf("protocol=%s", out.Protocol)
	}
	var s struct {
		Servers []struct {
			Method string `json:"method"`
		} `json:"servers"`
	}
	_ = json.Unmarshal(out.Settings, &s)
	if s.Servers[0].Method != "aes-256-gcm" {
		t.Fatalf("bad method: %+v", s.Servers)
	}
}

func TestUnsupportedLink(t *testing.T) {
	if _, err := ParseShareLink("http://example.com"); err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}
