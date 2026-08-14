package proxy

import (
	"io"
	"net/http"
	"regexp"
	"strings"
)

// ModifyRule describes a single rewrite applied to matching traffic.
type ModifyRule struct {
	MatchURL    string            `json:"match_url"`    // regex on full URL; empty = match all
	HeaderAdd   map[string]string `json:"header_add"`   // request headers to inject/override
	BodyRegex   string            `json:"body_regex"`   // pattern to replace in request body
	BodyReplace string            `json:"body_replace"` // replacement (supports $1..$9)
}

// ModifyRules holds the active rule set.
type ModifyRules struct {
	rules []ModifyRule
}

// NewModifyRules returns an empty rule set.
func NewModifyRules() *ModifyRules { return &ModifyRules{} }

// Set replaces the active rule set.
func (m *ModifyRules) Set(rules []ModifyRule) {
	if rules == nil {
		rules = []ModifyRule{}
	}
	m.rules = rules
}

// ApplyRequest mutates an outgoing *http.Request according to the rules.
func (m *ModifyRules) ApplyRequest(req *http.Request) {
	for _, rule := range m.rules {
		if rule.MatchURL != "" {
			re, err := regexp.Compile(rule.MatchURL)
			if err != nil || !re.MatchString(req.URL.String()) {
				continue
			}
		}
		for k, v := range rule.HeaderAdd {
			req.Header.Set(k, v)
		}
		if rule.BodyRegex != "" && req.Body != nil {
			body, err := io.ReadAll(req.Body)
			req.Body.Close()
			if err == nil {
				re, err := regexp.Compile(rule.BodyRegex)
				if err == nil {
					body = re.ReplaceAll(body, []byte(rule.BodyReplace))
					req.Body = io.NopCloser(strings.NewReader(string(body)))
					req.ContentLength = int64(len(body))
				}
			}
		}
	}
}
