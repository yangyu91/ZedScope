package proto

import "testing"

func TestVlessTLS(t *testing.T) {
	link := "vless://uuid-abc@example.com:8443?type=ws&security=tls&path=%2Fp&host=h.com&flow=xtls-rprx-vision&ps=vl"
	c, err := ParseVless(link)
	if err != nil {
		t.Fatalf("ParseVless: %v", err)
	}
	if c.Protocol != "vless" || c.UUID != "uuid-abc" || c.Address != "example.com" || c.Port != 8443 {
		t.Fatalf("bad fields: %+v", c)
	}
	if c.Encryption != "none" || c.Flow != "xtls-rprx-vision" || c.Network != "ws" || c.Security != "tls" {
		t.Fatalf("bad transport: %+v", c)
	}
	ss := mustJSON(t, c.StreamSettings)
	if !hasAll(ss, `"network":"ws"`, `"security":"tls"`, `"path":"/p"`, `"Host":"h.com"`) {
		t.Fatalf("bad streamSettings: %s", ss)
	}
}

func TestVlessReality(t *testing.T) {
	link := "vless://uuid@host:443?type=tcp&security=reality&sni=www.google.com&fp=chrome&pbk=PUBKEY&sid=abcd&spx=%2F"
	c, err := ParseVless(link)
	if err != nil {
		t.Fatalf("ParseVless reality: %v", err)
	}
	if c.Security != "reality" || c.SNI != "www.google.com" || c.PublicKey != "PUBKEY" || c.ShortID != "abcd" {
		t.Fatalf("bad reality fields: %+v", c)
	}
	ss := mustJSON(t, c.StreamSettings)
	if !hasAll(ss, `"security":"reality"`, `"realitySettings"`, `"publicKey":"PUBKEY"`, `"shortId":"abcd"`, `"spiderX":"/"`) {
		t.Fatalf("bad reality streamSettings: %s", ss)
	}
}

func TestVlessGRPC(t *testing.T) {
	link := "vless://uuid@host:443?type=grpc&security=tls&serviceName=my.svc&sni=h.com"
	c, err := ParseVless(link)
	if err != nil {
		t.Fatalf("ParseVless grpc: %v", err)
	}
	if c.Network != "grpc" || c.Path != "my.svc" {
		t.Fatalf("bad grpc path: %+v", c)
	}
	ss := mustJSON(t, c.StreamSettings)
	if !hasAll(ss, `"grpcSettings"`, `"serviceName":"my.svc"`) {
		t.Fatalf("bad grpc streamSettings: %s", ss)
	}
}

func TestVlessDefaults(t *testing.T) {
	c, err := ParseVless("vless://uuid@host:443")
	if err != nil {
		t.Fatalf("ParseVless defaults: %v", err)
	}
	if c.Network != "tcp" || c.Security != "none" {
		t.Fatalf("bad defaults: %+v", c)
	}
	if hasAll(mustJSON(t, c.StreamSettings), `"security"`) {
		t.Fatalf("expected no security block for none: %s", mustJSON(t, c.StreamSettings))
	}
}

func TestVlessErrors(t *testing.T) {
	if _, err := ParseVless("vless://@host:443"); err == nil {
		t.Fatal("expected missing uuid error")
	}
	if _, err := ParseVless("vless://uuid@host"); err == nil {
		t.Fatal("expected missing port error")
	}
}
