package proto

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestVMessStandard(t *testing.T) {
	raw := `{"v":"2","ps":"test","add":"1.2.3.4","port":"443","id":"uuid-123","aid":"0","net":"ws","type":"none","host":"edge.com","path":"/wspath","tls":"tls"}`
	link := "vmess://" + base64.StdEncoding.EncodeToString([]byte(raw))
	c, err := ParseVMess(link)
	if err != nil {
		t.Fatalf("ParseVMess: %v", err)
	}
	if c.Protocol != "vmess" || c.Address != "1.2.3.4" || c.Port != 443 {
		t.Fatalf("bad endpoint: %+v", c)
	}
	if c.UUID != "uuid-123" || c.AlterID != 0 {
		t.Fatalf("bad identity: %+v", c)
	}
	if c.Network != "ws" || c.Security != "tls" || c.Tag != "test" {
		t.Fatalf("bad transport: %+v", c)
	}
	ss := mustJSON(t, c.StreamSettings)
	if !hasAll(ss, `"network":"ws"`, `"tlsSettings"`, `"path":"/wspath"`, `"Host":"edge.com"`) {
		t.Fatalf("bad streamSettings: %s", ss)
	}
}

func TestVMessRawBase64(t *testing.T) {
	raw := `{"add":"10.0.0.1","port":"8443","id":"id-x","aid":"64","net":"tcp","tls":""}`
	link := "vmess://" + base64.RawStdEncoding.EncodeToString([]byte(raw))
	c, err := ParseVMess(link)
	if err != nil {
		t.Fatalf("ParseVMess raw: %v", err)
	}
	if c.Address != "10.0.0.1" || c.Port != 8443 || c.AlterID != 64 {
		t.Fatalf("bad fields: %+v", c)
	}
	if c.Network != "tcp" || c.Security != "" {
		t.Fatalf("bad transport: %+v", c)
	}
}

func TestVMessErrors(t *testing.T) {
	if _, err := ParseVMess("vmess://not-base64!!"); err == nil {
		t.Fatal("expected base64 error")
	}
	if _, err := ParseVMess("vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"h"}`))); err == nil {
		t.Fatal("expected missing id error")
	}
	if _, err := ParseVMess("vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"id":"u"}`))); err == nil {
		t.Fatal("expected missing add error")
	}
}

func TestVMessBadPort(t *testing.T) {
	raw := `{"add":"h","port":"abc","id":"u"}`
	link := "vmess://" + base64.StdEncoding.EncodeToString([]byte(raw))
	if _, err := ParseVMess(link); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("expected port error, got %v", err)
	}
}
