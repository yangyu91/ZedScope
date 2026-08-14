package proto

import "testing"

func TestSocks5WithAuth(t *testing.T) {
	link := "socks5://user:p@ss@proxy.example:1080#office"
	c, err := ParseSocks5(link)
	if err != nil {
		t.Fatalf("ParseSocks5: %v", err)
	}
	if c.Protocol != "socks" || c.Address != "proxy.example" || c.Port != 1080 {
		t.Fatalf("bad endpoint: %+v", c)
	}
	if c.Tag != "office" {
		t.Fatalf("bad tag: %q", c.Tag)
	}
	settings := mustJSON(t, c.Settings)
	if !hasAll(settings, `"username":"user"`, `"password":"p@ss"`) {
		t.Fatalf("missing auth in settings: %s", settings)
	}
}

func TestSocks5NoAuth(t *testing.T) {
	link := "socks5://10.0.0.5:1080"
	c, err := ParseSocks5(link)
	if err != nil {
		t.Fatalf("ParseSocks5 no-auth: %v", err)
	}
	settings := mustJSON(t, c.Settings)
	if hasAll(settings, `"username"`, `"password"`) {
		t.Fatalf("did not expect auth in settings: %s", settings)
	}
}

func TestSocksAlias(t *testing.T) {
	c, err := ParseSocks5("socks://10.0.0.5:1080")
	if err != nil {
		t.Fatalf("socks:// alias: %v", err)
	}
	if c.Protocol != "socks" || c.Port != 1080 {
		t.Fatalf("bad alias parse: %+v", c)
	}
}

func TestSocks5Errors(t *testing.T) {
	if _, err := ParseSocks5("socks5://10.0.0.5"); err == nil {
		t.Fatal("expected missing port error")
	}
	if _, err := ParseSocks5("socks5://:1080"); err == nil {
		t.Fatal("expected missing host error")
	}
}
