package proto

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ParseSS parses a shadowsocks:// share link. It supports both SIP002
// (ss://base64(method:password)@host:port) and the legacy all-in-one form
// (ss://base64(method:password@host:port)). The userinfo may also be plain
// (ss://method:password@host:port). Fragment (#tag) and query (?plugin=...) are
// stripped; the fragment becomes the outbound tag.
func ParseSS(link string) (*Config, error) {
	body := strings.TrimPrefix(link, "ss://")

	var tag string
	if i := strings.Index(body, "#"); i >= 0 {
		tag, body = body[i+1:], body[:i]
		if t, err := url.QueryUnescape(tag); err == nil {
			tag = t
		}
	}
	if i := strings.Index(body, "?"); i >= 0 { // drop plugin / extra query
		body = body[:i]
	}

	var method, password, host string
	var port int

	if at := strings.Index(body, "@"); at >= 0 {
		user := body[:at]
		rest := body[at+1:]
		up := decodeSSUserinfo(user)
		sp := strings.SplitN(up, ":", 2)
		if len(sp) != 2 {
			return nil, fmt.Errorf("ss: %w: method:password in userinfo", ErrMissingField)
		}
		method, password = sp[0], sp[1]
		h, p, err := splitHostPort(rest)
		if err != nil {
			return nil, fmt.Errorf("ss: %w: host:port (%v)", ErrMalformed, err)
		}
		host, port = h, p
	} else {
		// Legacy: entire body is base64(method:password@host:port).
		raw, err := decodeBase64(body)
		if err != nil {
			return nil, fmt.Errorf("ss: %w", err)
		}
		s := string(raw)
		at := strings.LastIndex(s, "@")
		if at < 0 {
			return nil, fmt.Errorf("ss: %w: missing @", ErrMalformed)
		}
		up := s[:at]
		rest := s[at+1:]
		sp := strings.SplitN(up, ":", 2)
		if len(sp) != 2 {
			return nil, fmt.Errorf("ss: %w: method:password", ErrMissingField)
		}
		method, password = sp[0], sp[1]
		h, p, err := splitHostPort(rest)
		if err != nil {
			return nil, fmt.Errorf("ss: %w: host:port (%v)", ErrMalformed, err)
		}
		host, port = h, p
	}

	if method == "" || password == "" {
		return nil, fmt.Errorf("ss: %w: method/password", ErrMissingField)
	}
	if host == "" || port == 0 {
		return nil, fmt.Errorf("ss: %w: host/port", ErrMissingField)
	}

	settings := map[string]interface{}{
		"servers": []map[string]interface{}{{
			"address":  host,
			"port":     port,
			"method":   method,
			"password": password,
			"level":    0,
		}},
	}
	return &Config{
		Protocol: "shadowsocks",
		Tag:      orDefault(tag, host),
		Address:  host,
		Port:     port,
		Method:   method,
		Password: password,
		Settings: settings,
	}, nil
}

// decodeSSUserinfo decodes a SIP002 ss:// userinfo, which may be
// base64(method:password) (possibly percent-encoded) or plain method:password.
func decodeSSUserinfo(user string) string {
	decoded := user
	if u2, err := url.QueryUnescape(user); err == nil {
		decoded = u2
	}
	if raw, err := decodeBase64(decoded); err == nil {
		if s := string(raw); strings.Contains(s, ":") {
			return s
		}
	}
	return decoded
}

func splitHostPort(s string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, err
	}
	p, ok := atoi(portStr)
	if !ok {
		return "", 0, fmt.Errorf("invalid port %q", portStr)
	}
	return host, p, nil
}
