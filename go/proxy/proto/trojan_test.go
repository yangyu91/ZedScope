package proto

import "testing"

func TestTrojanBasic(t *testing.T) {
	link := "trojan://secretpw@node.net:443?type=tcp&security=tls&ps=tr"
	c, err := ParseTrojan(link)
	if err != nil {
		t.Fatalf("ParseTrojan: %v", err)
	}
	if c.Protocol != "trojan" || c.Password != "secretpw" || c.Address != "node.net" || c.Port != 443 {
		t.Fatalf("bad fields: %+v", c)
	}
	// trojan normally defaults to tls
	if c.Security != "tls" || c.Network != "tcp" {
		t.Fatalf("bad transport: %+v", c)
	}
	ss := mustJSON(t, c.StreamSettings)
	if !hasAll(ss, `"security":"tls"`) {
		t.Fatalf("expected tls security: %s", ss)
	}
}

func TestTrojanGRPCReality(t *testing.T) {
	link := "trojan://pw@host:443?type=grpc&security=reality&serviceName=svc&sni=site.com&pbk=K&sid=ff"
	c, err := ParseTrojan(link)
	if err != nil {
		t.Fatalf("ParseTrojan grpc/reality: %v", err)
	}
	if c.Network != "grpc" || c.Path != "svc" || c.Security != "reality" {
		t.Fatalf("bad grpc/reality: %+v", c)
	}
	ss := mustJSON(t, c.StreamSettings)
	if !hasAll(ss, `"grpcSettings"`, `"serviceName":"svc"`, `"realitySettings"`, `"publicKey":"K"`, `"shortId":"ff"`) {
		t.Fatalf("bad streamSettings: %s", ss)
	}
}

func TestTrojanErrors(t *testing.T) {
	if _, err := ParseTrojan("trojan://node.net:443"); err == nil {
		t.Fatal("expected missing password error")
	}
	if _, err := ParseTrojan("trojan://pw@node.net"); err == nil {
		t.Fatal("expected missing port error")
	}
}
