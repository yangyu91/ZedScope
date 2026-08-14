package proto

import (
	"fmt"
	"net/url"
)

// ParseVless parses a vless:// share link: vless://uuid@host:port?type=...&
// security=...&flow=...&host=...&path=...&serviceName=...&sni=...&fp=...&
// pbk=...&sid=...&spx=...&alpn=...&encryption=...&ps=...
func ParseVless(link string) (*Config, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("vless: parse url: %w", err)
	}
	if u.User == nil || u.User.Username() == "" {
		return nil, fmt.Errorf("vless: %w: uuid", ErrMissingField)
	}
	uuid := u.User.Username()
	host := u.Hostname()
	portStr := u.Port()
	if host == "" || portStr == "" {
		return nil, fmt.Errorf("vless: %w: host or port", ErrMissingField)
	}
	port, ok := atoi(portStr)
	if !ok {
		return nil, fmt.Errorf("vless: %w: port", ErrMissingField)
	}

	q := u.Query()
	net := orDefault(q.Get("type"), "tcp")
	security := orDefault(q.Get("security"), "none")
	hostHeader := q.Get("host")
	path := q.Get("path")
	if net == "grpc" {
		if sn := q.Get("serviceName"); sn != "" {
			path = sn
		}
	}
	encryption := orDefault(q.Get("encryption"), "none")
	opts := tlsOpts{
		SNI:         q.Get("sni"),
		Fingerprint: q.Get("fp"),
		PublicKey:   q.Get("pbk"),
		ShortID:     q.Get("sid"),
		SpiderX:     q.Get("spx"),
		ALPN:        splitCSV(q.Get("alpn")),
	}

	settings := map[string]interface{}{
		"vnext": []map[string]interface{}{{
			"address": host,
			"port":    port,
			"users": []map[string]interface{}{{
				"id":         uuid,
				"encryption": encryption,
				"level":      0,
				"flow":       q.Get("flow"),
			}},
		}},
	}
	return &Config{
		Protocol:       "vless",
		Tag:            orDefault(q.Get("ps"), "vless"),
		Address:        host,
		Port:           port,
		UUID:           uuid,
		Encryption:     encryption,
		Flow:           q.Get("flow"),
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
