// Package proto parses multi-protocol proxy share links (vmess, vless, trojan,
// shadowsocks, socks5) into a structured, protocol-agnostic representation, and
// emits best-effort Xray-core compatible outbound blocks.
//
// It is deliberately dependency-free (standard library only) so the parent
// yami-UA proxy package can bundle it for Android via gomobile.
package proto

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Errors returned by the proto parsers. Callers can use errors.Is to branch on
// the failure category.
var (
	ErrUnsupportedScheme = errors.New("proto: unsupported share link scheme")
	ErrEmptyLink         = errors.New("proto: empty share link")
	ErrBadBase64         = errors.New("proto: base64 decode failed")
	ErrMissingField      = errors.New("proto: missing required field")
	ErrMalformed         = errors.New("proto: malformed share link")
)

// Config is a structured representation of a parsed proxy share link.
// Settings and StreamSettings are best-effort Xray-core outbound blocks
// (json-serializable maps) so callers can emit an "outbounds" entry directly.
type Config struct {
	Protocol string // xray protocol name: vmess, vless, trojan, shadowsocks, socks
	Tag      string

	// Endpoint
	Address string
	Port    int

	// Auth / identity
	UUID       string // vmess / vless user id
	AlterID    int    // vmess alterId
	Encryption string // vless encryption (usually "none")
	Flow       string // vless / trojan flow
	Password   string // trojan / socks / shadowsocks
	Method     string // shadowsocks cipher

	// Transport / security
	Network  string // tcp, ws, grpc, h2, quic, kcp
	Security string // none, tls, reality
	Host     string // ws/http Host header
	Path     string // ws path / grpc serviceName / http path

	// TLS / REALITY
	SNI         string
	Fingerprint string
	PublicKey   string
	ShortID     string
	SpiderX     string

	// Raw Xray-compatible blocks.
	Settings       map[string]interface{}
	StreamSettings map[string]interface{}
}

// Parse decodes any supported share link into a *Config. It dispatches on the
// URL scheme and returns a clear error for empty or unsupported input.
func Parse(link string) (*Config, error) {
	link = strings.TrimSpace(link)
	if link == "" {
		return nil, ErrEmptyLink
	}
	switch {
	case strings.HasPrefix(link, "vmess://"):
		return ParseVMess(link)
	case strings.HasPrefix(link, "vless://"):
		return ParseVless(link)
	case strings.HasPrefix(link, "trojan://"):
		return ParseTrojan(link)
	case strings.HasPrefix(link, "ss://"):
		return ParseSS(link)
	case strings.HasPrefix(link, "socks5://"), strings.HasPrefix(link, "socks://"):
		return ParseSocks5(link)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedScheme, schemeOf(link))
	}
}

// ToOutboundMap returns the xray outbounds entry for this config as a plain map,
// ready to be json-marshaled. Helper for callers that build raw xray configs.
func (c *Config) ToOutboundMap() map[string]interface{} {
	m := map[string]interface{}{
		"protocol": c.Protocol,
		"tag":      c.Tag,
		"settings": c.Settings,
	}
	if len(c.StreamSettings) > 0 {
		m["streamSettings"] = c.StreamSettings
	}
	return m
}

func schemeOf(link string) string {
	if i := strings.Index(link, "://"); i > 0 {
		return link[:i]
	}
	return link
}

// decodeBase64 tries the common base64 variants (std / raw / url / raw-url) and
// returns the first successful decode. Share links use inconsistent padding and
// alphabet, so being liberal here avoids spurious ErrBadBase64.
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if dec, err := enc.DecodeString(s); err == nil {
			return dec, nil
		}
	}
	return nil, ErrBadBase64
}

func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return n, true
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// tlsOpts carries the transport-security parameters that only apply when
// security is "tls" or "reality".
type tlsOpts struct {
	SNI         string
	Fingerprint string
	PublicKey   string
	ShortID     string
	SpiderX     string
	ALPN        []string
}

// buildStream builds an Xray streamSettings map from the transport parameters.
// network defaults to "tcp"; security "none" emits no security block. The
// per-network settings (ws/grpc/h2/quic/kcp) follow the Xray-core schema.
func buildStream(network, security, host, path string, o tlsOpts) map[string]interface{} {
	if network == "" {
		network = "tcp"
	}
	m := map[string]interface{}{"network": network}

	if security != "" && security != "none" {
		m["security"] = security
		switch security {
		case "tls":
			tls := map[string]interface{}{}
			if o.SNI != "" {
				tls["serverName"] = o.SNI
			}
			if o.Fingerprint != "" {
				tls["fingerprint"] = o.Fingerprint
			}
			if len(o.ALPN) > 0 {
				tls["alpn"] = o.ALPN
			}
			m["tlsSettings"] = tls
		case "reality":
			rt := map[string]interface{}{}
			if o.SNI != "" {
				rt["serverName"] = o.SNI
			}
			if o.Fingerprint != "" {
				rt["fingerprint"] = o.Fingerprint
			}
			if o.PublicKey != "" {
				rt["publicKey"] = o.PublicKey
			}
			if o.ShortID != "" {
				rt["shortId"] = o.ShortID
			}
			if o.SpiderX != "" {
				rt["spiderX"] = o.SpiderX
			}
			m["realitySettings"] = rt
		}
	}

	switch network {
	case "ws":
		ws := map[string]interface{}{"path": orDefault(path, "/")}
		if host != "" {
			ws["headers"] = map[string]interface{}{"Host": host}
		}
		m["wsSettings"] = ws
	case "grpc":
		m["grpcSettings"] = map[string]interface{}{"serviceName": orDefault(path, "")}
	case "h2":
		http := map[string]interface{}{"path": orDefault(path, "/")}
		if host != "" {
			http["host"] = []string{host}
		}
		m["httpSettings"] = http
	case "quic":
		quic := map[string]interface{}{}
		if security != "" && security != "none" {
			quic["security"] = security
		}
		m["quicSettings"] = quic
	case "kcp":
		m["kcpSettings"] = map[string]interface{}{}
	case "tcp":
		if path != "" {
			m["tcpSettings"] = map[string]interface{}{
				"header": map[string]interface{}{
					"type": "http",
					"request": map[string]interface{}{
						"path": []string{path},
					},
				},
			}
		}
	}
	return m
}

// splitCSV splits a comma-separated option list (used by alpn=...).
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
