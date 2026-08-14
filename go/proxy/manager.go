package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"yamiua/proxy/proto"
)

// Outbound is a minimal xray outbound description. It serializes directly into
// an xray-core config's "outbounds" array.
type Outbound struct {
	Protocol       string          `json:"protocol"`
	Tag            string          `json:"tag"`
	Settings       json.RawMessage `json:"settings"`
	StreamSettings json.RawMessage `json:"streamSettings,omitempty"`
}

// XrayConfig is the subset of an xray-core config yami-UA generates.
type XrayConfig struct {
	Inbounds  []map[string]interface{} `json:"inbounds"`
	Outbounds []*Outbound              `json:"outbounds"`
	Routing   map[string]interface{}   `json:"routing,omitempty"`
}

// ParseShareLink decodes a proxy share link into an xray Outbound.
// Supported: vmess://, vless://, trojan://, ss://, socks://, socks5://.
// Parsing is delegated to the proto subpackage; this wrapper only adapts the
// proto.Config into the proxy.Outbound shape kept for backward compatibility.
func ParseShareLink(link string) (*Outbound, error) {
	c, err := proto.Parse(link)
	if err != nil {
		return nil, err
	}
	return protoToOutbound(c), nil
}

// protoToOutbound converts a parsed proto.Config into a proxy.Outbound. The
// StreamSettings field is omitted when empty (e.g. for shadowsocks) to keep the
// generated xray config clean.
func protoToOutbound(c *proto.Config) *Outbound {
	out := &Outbound{
		Protocol: c.Protocol,
		Tag:      c.Tag,
		Settings: mustJSONRaw(c.Settings),
	}
	if len(c.StreamSettings) > 0 {
		out.StreamSettings = mustJSONRaw(c.StreamSettings)
	}
	return out
}

// WriteXrayConfig writes a runnable xray config that listens on listenPort
// (socks5) and routes through the given outbounds. Returns the file path.
func WriteXrayConfig(dir string, listenPort int, outs ...*Outbound) (string, error) {
	outs = append(outs, &Outbound{Protocol: "freedom", Tag: "direct"})
	cfg := XrayConfig{
		Inbounds: []map[string]interface{}{{
			"port":     listenPort,
			"protocol": "socks",
			"listen":   "127.0.0.1",
			"settings": map[string]interface{}{"udp": true},
		}},
		Outbounds: outs,
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := dir + "/xray-config.json"
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// WriteXrayConfigFromLinks parses one or more share links via the proto
// subpackage and writes an xray config routing through them. This is the
// end-to-end entry point that reuses proto parsing for config generation.
func WriteXrayConfigFromLinks(dir string, listenPort int, links ...string) (string, error) {
	outs := make([]*Outbound, 0, len(links))
	for _, l := range links {
		c, err := proto.Parse(l)
		if err != nil {
			return "", err
		}
		outs = append(outs, protoToOutbound(c))
	}
	return WriteXrayConfig(dir, listenPort, outs...)
}

// LaunchCore starts an xray/v2ray core binary with the given config. The
// caller must supply the core binary (yami-UA does not bundle it). The
// returned *exec.Cmd is already started; call cmd.Process.Kill to stop.
func LaunchCore(bin, configPath string) (*exec.Cmd, error) {
	if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("core binary not found: %s (supply xray/v2ray-core)", bin)
	}
	cmd := exec.Command(bin, "-c", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// ---- helpers ----

func mustJSONRaw(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
