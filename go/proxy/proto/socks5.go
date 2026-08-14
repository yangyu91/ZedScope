package proto

import (
	"fmt"
	"net/url"
)

// ParseSocks5 parses a socks:// or socks5:// share link:
// socks5://[user:pass@]host:port[#tag]. Username/password are optional and only
// emitted when present. The fragment (if any) is used as the outbound tag.
func ParseSocks5(link string) (*Config, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("socks: parse url: %w", err)
	}
	host := u.Hostname()
	portStr := u.Port()
	if host == "" || portStr == "" {
		return nil, fmt.Errorf("socks: %w: host or port", ErrMissingField)
	}
	port, ok := atoi(portStr)
	if !ok {
		return nil, fmt.Errorf("socks: %w: port", ErrMissingField)
	}

	user := ""
	pass := ""
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}

	server := map[string]interface{}{
		"address": host,
		"port":    port,
		"level":   0,
	}
	if user != "" {
		server["username"] = user
	}
	if pass != "" {
		server["password"] = pass
	}
	settings := map[string]interface{}{
		"servers": []map[string]interface{}{server},
	}

	tag := u.Fragment
	if tag == "" {
		tag = "socks5"
	}
	return &Config{
		Protocol: "socks",
		Tag:      tag,
		Address:  host,
		Port:     port,
		Password: pass,
		Settings: settings,
	}, nil
}
