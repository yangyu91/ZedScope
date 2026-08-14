package proxy

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Filter decides whether a captured transaction is "noise" that should be
// hidden from the user by default. The noise is yami-UA's own machinery
// (localhost control plane, AI relay, proxy core) and known AI / relay
// provider hosts — traffic the user did not intentionally browse.
type Filter struct {
	enabled bool
	rules   []*regexp.Regexp // compiled deny patterns, matched against the host
}

// DefaultFilter returns the clean-capture filter, enabled.
func DefaultFilter() *Filter {
	patterns := []string{
		// yami-UA's own local control plane
		`^127\.0\.0\.1$`, `^localhost$`, `^::1$`, `^0\.0\.0\.0$`, `\.local$`,
		`(^|[-.])yami-ua(\.local)?$`,
		// AI providers / relays / model backends
		`(^|[-.])openai\.com$`,
		`(^|[-.])anthropic\.com$`, `(^|[-.])claude\.ai$`,
		`(^|[-.])deepseek\.com$`,
		`(^|[-.])googleapis\.com$`, `(^|[-.])generativelanguage\.googleapis\.com$`,
		`(^|[-.])api\.mistral\.ai$`, `(^|[-.])cohere\.ai$`, `(^|[-.])ai21\.com$`,
		`(^|[-.])openrouter\.ai$`, `(^|[-.])huggingface\.co$`,
		// proxy pool / v2ray / xray control & metadata
		`(^|[-.])v2ray\.com$`, `(^|[-.])xray\.com$`,
	}
	f := &Filter{enabled: true}
	for _, p := range patterns {
		f.rules = append(f.rules, regexp.MustCompile("(?i)"+p))
	}
	return f
}

// SetEnabled turns the filter on/off.
func (f *Filter) SetEnabled(v bool) { f.enabled = v }

// Drop reports whether a transaction to rawURL/host should be filtered out.
func (f *Filter) Drop(rawURL, host string) bool {
	if !f.enabled {
		return false
	}
	h := hostnameOf(rawURL, host)
	for _, re := range f.rules {
		if re.MatchString(h) {
			return true
		}
	}
	return false
}

// hostnameOf returns the lower-cased hostname (port stripped) for matching.
func hostnameOf(rawURL, host string) string {
	if host != "" {
		if h, _, err := net.SplitHostPort(host); err == nil {
			return strings.ToLower(h)
		}
		return strings.ToLower(host)
	}
	if u, err := url.Parse(rawURL); err == nil {
		return strings.ToLower(u.Hostname())
	}
	return ""
}
