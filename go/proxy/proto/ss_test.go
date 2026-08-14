package proto

import (
	"encoding/base64"
	"testing"
)

func TestSSPlainUserinfo(t *testing.T) {
	link := "ss://aes-256-gcm:passwd@1.1.1.1:8388"
	c, err := ParseSS(link)
	if err != nil {
		t.Fatalf("ParseSS plain: %v", err)
	}
	if c.Protocol != "shadowsocks" || c.Method != "aes-256-gcm" || c.Password != "passwd" {
		t.Fatalf("bad fields: %+v", c)
	}
	if c.Address != "1.1.1.1" || c.Port != 8388 {
		t.Fatalf("bad endpoint: %+v", c)
	}
}

func TestSSSIP002Base64(t *testing.T) {
	user := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:secret"))
	link := "ss://" + user + "@2.2.2.2:8388#MyNode"
	c, err := ParseSS(link)
	if err != nil {
		t.Fatalf("ParseSS sip002: %v", err)
	}
	if c.Method != "aes-256-gcm" || c.Password != "secret" || c.Address != "2.2.2.2" || c.Port != 8388 {
		t.Fatalf("bad fields: %+v", c)
	}
	if c.Tag != "MyNode" {
		t.Fatalf("bad tag: %q", c.Tag)
	}
}

func TestSSLegacyAllInOne(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:Secret123@3.3.3.3:9999"))
	link := "ss://" + b64
	c, err := ParseSS(link)
	if err != nil {
		t.Fatalf("ParseSS legacy: %v", err)
	}
	if c.Method != "chacha20-ietf-poly1305" || c.Password != "Secret123" || c.Address != "3.3.3.3" || c.Port != 9999 {
		t.Fatalf("bad fields: %+v", c)
	}
}

func TestSSErrors(t *testing.T) {
	// missing @ in legacy form and not valid base64
	if _, err := ParseSS("ss://garbage-without-at-sign"); err == nil {
		t.Fatal("expected error for malformed ss link")
	}
	// base64 decodes but no @ separator
	if _, err := ParseSS("ss://" + base64.StdEncoding.EncodeToString([]byte("no-at-here"))); err == nil {
		t.Fatal("expected error for missing @ in legacy ss")
	}
	// empty userinfo branch
	if _, err := ParseSS("ss://@1.1.1.1:8388"); err == nil {
		t.Fatal("expected error for empty method:password")
	}
}
