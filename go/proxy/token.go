package proxy

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// Pre-compiled patterns for credential discovery.
var (
	reJWT       = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`)
	reLoginPath = regexp.MustCompile(`(?i)(/login|/signin|/sign-in|/auth|/oauth|/token|/session|/sso|/api/auth|/user/login|/passport)`)
	// JSON-ish key:value pairs where the key looks credential-ish.
	// (RE2 has no backreferences, so the optional quote is not captured.)
	reJSONToken = regexp.MustCompile(`(?i)"([a-z0-9_]*(?:token|session|jwt|auth|sid|csrf|xsrf|secret|cookie|ticket)[a-z0-9_]*)"\s*:\s*"?([A-Za-z0-9_\-\.=]{6,512})`)
)

// ExtractTokens scans a captured Record for credentials in headers, cookies,
// URL and body, marking login flows when detected.
func ExtractTokens(r *Record) []Token {
	var toks []Token
	now := time.Now().Unix()

	add := func(key, value, source string) {
		v := strings.TrimSpace(value)
		if v == "" {
			return
		}
		if len(v) > 2000 {
			v = v[:2000]
		}
		toks = append(toks, Token{
			Key:        key,
			Value:      v,
			Source:     source,
			URL:        r.URL,
			IsLogin:    r.IsLogin,
			CapturedAt: now,
		})
	}

	// --- Headers (request + response) ---
	for _, raw := range []string{r.ReqHeaders, r.RespHeaders} {
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			lower := strings.ToLower(line)
			switch {
			case strings.HasPrefix(lower, "authorization:"):
				add("Authorization", strings.TrimSpace(line[len("authorization:"):]), "header")
			case strings.HasPrefix(lower, "set-cookie:"):
				// capture session-like cookies
				cv := strings.TrimSpace(line[len("set-cookie:"):])
				for _, part := range strings.Split(cv, ";") {
					kv := strings.SplitN(part, "=", 2)
					if len(kv) != 2 {
						continue
					}
					k := strings.TrimSpace(kv[0])
					if isSessionCookie(k) {
						add("Cookie:"+k, strings.TrimSpace(kv[1]), "cookie")
					}
				}
			case strings.HasPrefix(lower, "cookie:"):
				cv := strings.TrimSpace(line[len("cookie:"):])
				for _, part := range strings.Split(cv, ";") {
					kv := strings.SplitN(part, "=", 2)
					if len(kv) != 2 {
						continue
					}
					k := strings.TrimSpace(kv[0])
					if isSessionCookie(k) {
						add("Cookie:"+k, strings.TrimSpace(kv[1]), "cookie")
					}
				}
			case strings.Contains(lower, "token") || strings.Contains(lower, "auth"):
				kv := strings.SplitN(line, ":", 2)
				if len(kv) == 2 {
					add(strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1]), "header")
				}
			}
		}
	}

	// --- JWTs anywhere in headers or body ---
	for _, m := range reJWT.FindAllString(r.ReqHeaders+r.RespHeaders+r.ReqBody+r.RespBody, -1) {
		add("JWT", m, "header")
	}

	// --- JSON-ish key:value in body ---
	for _, mm := range reJSONToken.FindAllStringSubmatch(r.ReqBody+r.RespBody, -1) {
		if len(mm) >= 3 {
			add(mm[1], mm[2], "body")
		}
	}

	return toks
}

func isSessionCookie(name string) bool {
	n := strings.ToLower(name)
	for _, p := range []string{"session", "sid", "token", "auth", "ticket", "jwt", "csrf", "xsrf", "login", "pass"} {
		if strings.Contains(n, p) {
			return true
		}
	}
	return false
}

// detectLogin decides whether a transaction looks like an auth/login exchange.
func detectLogin(method, url, reqHeaders, respHeaders, respBody string) bool {
	if reLoginPath.MatchString(url) {
		return true
	}
	// server issued a session cookie
	for _, line := range strings.Split(respHeaders, "\n") {
		if strings.HasPrefix(strings.ToLower(line), "set-cookie:") &&
			isSessionCookie(strings.TrimSpace(strings.SplitN(line, ":", 2)[1])) {
			return true
		}
	}
	// response body carries token-like JSON
	if reJSONToken.MatchString(respBody) {
		return true
	}
	return false
}

// summarizeBody is a helper used by tests/cli to pretty-print a record.
func summarizeBody(r *Record) string {
	b, _ := json.Marshal(map[string]interface{}{
		"url":    r.URL,
		"status": r.StatusCode,
		"login":  r.IsLogin,
		"tokens": len(r.Tokens),
	})
	return string(b)
}
