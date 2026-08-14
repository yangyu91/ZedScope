package proto

import (
	"fmt"
	"net/url"
)

// ParseTrojan parses a trojan:// share link: trojan://password@host:port?type=
// ...&security=...&host=...&path=...&serviceName=...&flow=...&sni=...&fp=...&
// pbk=...&sid=...&spx=...&alpn=...&ps=...
// The password lives in the URL userinfo (username slot). Trojan normally rides
// TLS, so an absent security parameter defaults to "tls".
func ParseTrojan(link string) (*Config, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("trojan: parse url: %w", err)
	}
	if u.User == nil {
		return nil, fmt.Errorf("trojan: %w: password", ErrMissingField)
	}
	password := u.User.Username()
	host := u.Hostname()
	portStr := u.Port()
	if host == "" || portStr == "" {
		return nil, fmt.Errorf("trojan: %w: host or port", ErrMissingField)
	}
	port, ok := atoi(portStr)
	if !ok {
		return nil, fmt.Errorf("trojan: %w: port", ErrMissingField)
	}

	q := u.Query()
	net := orDefault(q.Get("type"), "tcp")
	security := q.Get("security")
	if security == "" {
		security = "tls"
	}
	hostHeader := q.Get("host")
	path := q.Get("path")
	if net == "grpc" {
		if sn := q.Get("serviceName"); sn != "" {
			path = sn
		}
	}
	flow := q.Get("flow")
	opts := tlsOpts{
		SNI:         q.Get("sni"),
		Fingerprint: q.Get("fp"),
		PublicKey:   q.Get("pbk"),
		ShortID:     q.Get("sid"),
		SpiderX:     q.Get("spx"),
		ALPN:        splitCSV(q.Get("alpn")),
	}

	user := map[string]interface{}{
		"address":  host,
		"port":     port,
		"password": password,
		"level":    0,
	}
	if flow != "" {
		user["flow"] = flow
	}
	settings := map[string]interface{}{
		"servers": []map[string]interface{}{user},
	}
	return &Config{
		Protocol:       "trojan",
		Tag:            orDefault(q.Get("ps"), "trojan"),
		Address:        host,
		Port:           port,
		Password:       password,
		Flow:           flow,
		Network:        net,
		Security:       security,
		Host:           hostHeader,
		Path:           path,
		SNI:            opts.SNI,
		Fingerprint:    opts.Fingerprint,
		PublicKey:      opts.PublicKey,
		ShortID:        opts.ShortID,
		SpiderX:        opts.SpiderX,
		Settings:       settings,
		StreamSettings: buildStream(net, security, hostHeader, path, opts),
	}, nil
}
