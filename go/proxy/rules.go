package proxy

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"sync"
)

// BlockRule drops a request that matches URLRegex before it is forwarded
// upstream — a 403 is returned to the client instead.
type BlockRule struct {
	URLRegex string `json:"url_regex"`
}

// MapRule serves a locally-constructed response for any request matching
// URLRegex, without contacting the remote origin.
type MapRule struct {
	URLRegex    string `json:"url_regex"`
	Status      int    `json:"status"`
	Body        string `json:"body"`
	ContentType string `json:"content_type"`
}

// DecryptRule best-effort decrypts a captured response body with AES so the
// plaintext can be shown in the UI. If decryption fails the original body is
// preserved and no error is surfaced to the caller.
type DecryptRule struct {
	URLRegex string `json:"url_regex"`
	Key      string `json:"key"`   // hex or base64 encoded key
	IV       string `json:"iv"`    // hex or base64 encoded IV (CBC/CFB/OFB)
	Algo     string `json:"algo"`  // e.g. "aes-128-cbc", "aes-256-cbc", "aes-128-ecb"
	Mode     string `json:"mode"`  // "cbc" (default) | "ecb" | "cfb" | "ofb"
	Encoding string `json:"encoding"` // "base64" | "hex" (how the body is decoded); "" => raw
}

// RulesConfig is the JSON-serializable rule set edited by the control plane
// (relay.go registers /ai/rules with it).
type RulesConfig struct {
	DomainInclude []string      `json:"domain_include"`
	DomainExclude []string      `json:"domain_exclude"`
	Block         []BlockRule   `json:"block"`
	Map           []MapRule     `json:"map"`
	Decrypt       []DecryptRule `json:"decrypt"`
}

// ProxyRules is the compiled, concurrency-safe holder for every enhancement
// rule. A single instance is owned by Proxy and consulted at the request and
// capture stages.
type ProxyRules struct {
	mu     sync.RWMutex
	cfg    RulesConfig
	block  []*regexp.Regexp
	maps   []mapEntry
	decrypt []decryptEntry
}

type mapEntry struct {
	re   *regexp.Regexp
	rule MapRule
}

type decryptEntry struct {
	re   *regexp.Regexp
	rule DecryptRule
}

// NewProxyRules returns an empty (all-pass) rule set.
func NewProxyRules() *ProxyRules { return &ProxyRules{} }

// Set replaces the active rule set, compiling all regular expressions. A
// compile error aborts the whole update and is returned to the caller.
func (pr *ProxyRules) Set(cfg RulesConfig) error {
	blockRe := make([]*regexp.Regexp, 0, len(cfg.Block))
	for _, b := range cfg.Block {
		if b.URLRegex == "" {
			continue
		}
		re, err := regexp.Compile(b.URLRegex)
		if err != nil {
			return errors.New("block rule regex: " + err.Error())
		}
		blockRe = append(blockRe, re)
	}

	maps := make([]mapEntry, 0, len(cfg.Map))
	for _, m := range cfg.Map {
		if m.URLRegex == "" {
			continue
		}
		re, err := regexp.Compile(m.URLRegex)
		if err != nil {
			return errors.New("map rule regex: " + err.Error())
		}
		if m.Status == 0 {
			m.Status = 200
		}
		if m.ContentType == "" {
			m.ContentType = "application/octet-stream"
		}
		maps = append(maps, mapEntry{re: re, rule: m})
	}

	dec := make([]decryptEntry, 0, len(cfg.Decrypt))
	for _, d := range cfg.Decrypt {
		if d.URLRegex == "" {
			continue
		}
		re, err := regexp.Compile(d.URLRegex)
		if err != nil {
			return errors.New("decrypt rule regex: " + err.Error())
		}
		dec = append(dec, decryptEntry{re: re, rule: d})
	}

	pr.mu.Lock()
	pr.cfg = cfg
	pr.block = blockRe
	pr.maps = maps
	pr.decrypt = dec
	pr.mu.Unlock()
	return nil
}

// Config returns a snapshot of the active rule set in its JSON form.
func (pr *ProxyRules) Config() RulesConfig {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.cfg
}

// ShouldCapture applies the domain white/black list to a request host.
//   - No include/exclude configured          -> capture (true).
//   - Host matches any exclude pattern         -> drop (false).
//   - Include list non-empty & no match        -> drop (false).
//   - Otherwise                                -> capture (true).
func (pr *ProxyRules) ShouldCapture(host string) bool {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	if len(pr.cfg.DomainInclude) == 0 && len(pr.cfg.DomainExclude) == 0 {
		return true
	}
	for _, p := range pr.cfg.DomainExclude {
		if domainMatch(host, p) {
			return false
		}
	}
	if len(pr.cfg.DomainInclude) > 0 {
		for _, p := range pr.cfg.DomainInclude {
			if domainMatch(host, p) {
				return true
			}
		}
		return false
	}
	return true
}

// Blocked reports whether rawURL matches any active block rule.
func (pr *ProxyRules) Blocked(rawURL string) bool {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	for _, re := range pr.block {
		if re.MatchString(rawURL) {
			return true
		}
	}
	return false
}

// MatchMap returns the first map rule matching rawURL, if any.
func (pr *ProxyRules) MatchMap(rawURL string) (MapRule, bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	for _, m := range pr.maps {
		if m.re.MatchString(rawURL) {
			return m.rule, true
		}
	}
	return MapRule{}, false
}

// DecryptBody returns the AES-decrypted body for rawURL when a matching
// decrypt rule exists and decryption succeeds; otherwise it returns the input
// unchanged. It never panics.
func (pr *ProxyRules) DecryptBody(rawURL, body string) string {
	pr.mu.RLock()
	rules := pr.decrypt
	pr.mu.RUnlock()
	for _, d := range rules {
		if !d.re.MatchString(rawURL) {
			continue
		}
		if out, err := decryptAES(d.rule, body); err == nil && len(out) > 0 {
			return string(out)
		}
	}
	return body
}

// domainMatch reports whether host satisfies a domain pattern. A leading
// "*." matches the domain and all of its subdomains; otherwise an exact match
// or a subdomain (host ends with "."+pattern) counts.
func domainMatch(host, pattern string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" || host == "" {
		return false
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		return host == pattern[2:] || strings.HasSuffix(host, suffix)
	}
	return host == pattern || strings.HasSuffix(host, "."+pattern)
}

// decryptAES applies a single DecryptRule. The body is decoded per
// rule.Encoding ("base64" | "hex" | ""/raw), decrypted with AES and, for
// block modes with padding, PKCS#7-unpadded.
func decryptAES(rule DecryptRule, body string) ([]byte, error) {
	var ct []byte
	switch strings.ToLower(rule.Encoding) {
	case "base64":
		ct = []byte(body)
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body))
		if err != nil {
			return nil, err
		}
		ct = b
	case "hex":
		b, err := hex.DecodeString(strings.TrimSpace(body))
		if err != nil {
			return nil, err
		}
		ct = b
	default:
		ct = []byte(body)
	}

	key, err := decodeKey(rule.Key)
	if err != nil {
		return nil, err
	}
	iv, err := decodeKey(rule.IV)
	if err != nil {
		return nil, err
	}

	mode := strings.ToLower(rule.Mode)
	if mode == "" {
		// derive mode from algo suffix if present
		a := strings.ToLower(rule.Algo)
		switch {
		case strings.HasSuffix(a, "ecb"):
			mode = "ecb"
		case strings.HasSuffix(a, "cfb"):
			mode = "cfb"
		case strings.HasSuffix(a, "ofb"):
			mode = "ofb"
		default:
			mode = "cbc"
		}
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	var pt []byte
	switch mode {
	case "ecb":
		if len(ct)%aes.BlockSize != 0 {
			return nil, errors.New("ecb ciphertext not block-aligned")
		}
		pt = make([]byte, len(ct))
		for i := 0; i < len(ct); i += aes.BlockSize {
			block.Decrypt(pt[i:i+aes.BlockSize], ct[i:i+aes.BlockSize])
		}
		pt = pkcs7Unpad(pt)
	case "cfb":
		if len(iv) != block.BlockSize() {
			return nil, errors.New("cfb iv length mismatch")
		}
		stream := cipher.NewCFBDecrypter(block, iv)
		pt = make([]byte, len(ct))
		stream.XORKeyStream(pt, ct)
	case "ofb":
		if len(iv) != block.BlockSize() {
			return nil, errors.New("ofb iv length mismatch")
		}
		stream := cipher.NewOFB(block, iv)
		pt = make([]byte, len(ct))
		stream.XORKeyStream(pt, ct)
	default: // cbc
		if len(iv) != aes.BlockSize {
			return nil, errors.New("cbc iv must be 16 bytes")
		}
		if len(ct)%aes.BlockSize != 0 {
			return nil, errors.New("cbc ciphertext not block-aligned")
		}
		pt = make([]byte, len(ct))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(pt, ct)
		pt = pkcs7Unpad(pt)
	}
	return pt, nil
}

// decodeKey parses a hex or base64 encoded key/iv string.
func decodeKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("empty key/iv")
	}
	if b, err := hex.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return []byte(s), nil
}

// pkcs7Unpad removes PKCS#7 padding, returning the original plaintext. Invalid
// padding is left untouched (best-effort) rather than panicking.
func pkcs7Unpad(b []byte) []byte {
	n := len(b)
	if n == 0 {
		return b
	}
	pad := int(b[n-1])
	if pad < 1 || pad > aes.BlockSize || pad > n {
		return b
	}
	for _, v := range b[n-pad:] {
		if int(v) != pad {
			return b
		}
	}
	return b[:n-pad]
}
