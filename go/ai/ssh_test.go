package ai

import "testing"

func TestSshManagerEmptyList(t *testing.T) {
	m := NewSshManager()
	if got := m.List(); len(got) != 0 {
		t.Fatalf("expected empty list, got %v", got)
	}
	if ok := m.Close("nope"); ok {
		t.Fatal("closing a missing session should return false")
	}
}

func TestSshConnectBadHost(t *testing.T) {
	m := NewSshManager()
	// Port 1 is not listening; ssh.Dial should fail fast (connection refused).
	_, err := m.Connect(SshAuth{Host: "127.0.0.1:1", User: "u", AuthType: "password", Secret: "x"})
	if err == nil {
		t.Fatal("expected dial error for an unreachable host")
	}
}

func TestSshConnectBadKey(t *testing.T) {
	m := NewSshManager()
	_, err := m.Connect(SshAuth{Host: "127.0.0.1:1", User: "u", AuthType: "key", Secret: "not-a-pem"})
	if err == nil {
		t.Fatal("expected a private-key parse error")
	}
}

func TestSshConnectEmptyPassword(t *testing.T) {
	m := NewSshManager()
	_, err := m.Connect(SshAuth{Host: "127.0.0.1:1", User: "u", AuthType: "password", Secret: ""})
	if err == nil {
		t.Fatal("expected an error for an empty password")
	}
}
