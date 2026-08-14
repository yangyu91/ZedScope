package proto

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseVMess parses a vmess:// share link. The payload is a base64-encoded JSON
// object (the v2rayN "vmess v2" schema) with fields add/port/id/aid/net/type/
// host/path/tls/sni/ps. The base64 alphabet/padding varies across clients, so
// decodeBase64 tries every common variant.
func ParseVMess(link string) (*Config, error) {
	b64 := strings.TrimPrefix(link, "vmess://")
	if i := strings.Index(b64, "#"); i >= 0 { // drop optional fragment
		b64 = b64[:i]
	}
	raw, err := decodeBase64(b64)
	if err != nil {
		return nil, fmt.Errorf("vmess: %w", err)
	}
	var v struct {
		V    string `json:"v"`
		PS   string `json:"ps"`
		Add  string `json:"add"`
		Port string `json:"port"`
		ID   string `json:"id"`
		Aid  string `json:"aid"`
		Net  string `json:"net"`
		Type string `json:"type"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		SNI  string `json:"sni"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("vmess: json: %w", err)
	}
	if v.Add == "" {
		return nil, fmt.Errorf("vmess: %w: add", ErrMissingField)
	}
	if v.ID == "" {
		return nil, fmt.Errorf("vmess: %w: id", ErrMissingField)
	}
	port, ok := atoi(v.Port)
	if !ok {
		return nil, fmt.Errorf("vmess: %w: port", ErrMissingField)
	}
	aid, _ := atoi(v.Aid)
	net := orDefault(v.Net, "tcp")
	security := v.TLS // "" or "tls" (rarely "reality")

	settings := map[string]interface{}{
		"vnext": []map[string]interface{}{{
			"address": v.Add,
			"port":    port,
			"users": []map[string]interface{}{{
				"id":       v.ID,
				"alterId":  aid,
				"security": "auto",
				"level":    0,
			}},
		}},
	}
	return &Config{
		Protocol:       "vmess",
		Tag:            orDefault(v.PS, "vmess"),
		Address:        v.Add,
		Port:           port,
		UUID:           v.ID,
		AlterID:        aid,
		Network:        net,
		Security:       security,
		Host:           v.Host,
		Path:           v.Path,
		SNI:            v.SNI,
		Settings:       settings,
		StreamSettings: buildStream(net, security, v.Host, v.Path, tlsOpts{SNI: v.SNI}),
	}, nil
}
