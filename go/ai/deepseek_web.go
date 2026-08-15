package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrDeepseekLoginRequired is returned when the DeepSeek web session cookie is
// missing or expired. Callers (UI) should prompt the user to sign in again at
// chat.deepseek.com inside the WebView and re-export the cookie — no API key is
// ever needed for this free bridge.
var ErrDeepseekLoginRequired = errors.New("deepseek web session invalid or expired: please sign in at https://chat.deepseek.com and re-export the WebView cookie")

// dsWebBaseURL is the DeepSeek web origin used by the free bridge. It is a
// package variable so tests can repoint it at a local mock server.
var dsWebBaseURL = "https://chat.deepseek.com"

func dsWebCompletionURL() string {
	return strings.TrimRight(dsWebBaseURL, "/") + "/api/v0/chat/completion"
}

func dsWebAPIURL(path string) string {
	return strings.TrimRight(dsWebBaseURL, "/") + path
}

// applyDsWebClientHeaders mirrors deepseek-pp's createClientHeaders so the free
// bridge is indistinguishable from the logged-in web page: Bearer userToken
// (localStorage) plus x-client-* headers. Cookies are added separately when
// present (older sessions / browsers that still rely on the cookie jar).
func applyDsWebClientHeaders(hreq *http.Request, p *Provider) {
	if p == nil {
		return
	}
	if p.Token != "" {
		hreq.Header.Set("Authorization", "Bearer "+p.Token)
	}
	hreq.Header.Set("X-App-Version", "2.0.0")
	hreq.Header.Set("x-client-platform", "web")
	hreq.Header.Set("x-client-version", "2.0.0")
	hreq.Header.Set("x-client-locale", "zh-CN")
	_, offset := time.Now().Zone()
	hreq.Header.Set("x-client-timezone-offset", strconv.FormatInt(int64(offset), 10))
	if p.Cookies != "" {
		hreq.Header.Set("Cookie", p.Cookies)
	}
}

// dsWebPostJSON posts a JSON body to a DeepSeek web API path with the
// provider's cookies and returns the parsed JSON object. Login-expired
// responses map to ErrDeepseekLoginRequired.
func dsWebPostJSON(reg *Registry, p *Provider, path string, body any) (map[string]json.RawMessage, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequest(http.MethodPost, dsWebAPIURL(path), bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json")
	hreq.Header.Set("accept-charset", "UTF-8")
	if p.Cookies == "" && p.Token == "" {
		return nil, ErrDeepseekLoginRequired
	}
	applyDsWebClientHeaders(hreq, p)

	resp, err := httpClientFor(reg, p).Do(hreq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	snippet, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		if IsDeepseekLoginRequired(resp.StatusCode, string(snippet)) {
			return nil, ErrDeepseekLoginRequired
		}
		return nil, fmt.Errorf("deepseek-web %s: %d: %s", path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(snippet, &raw); err != nil {
		return nil, fmt.Errorf("deepseek-web %s: bad json: %s", path, strings.TrimSpace(string(snippet)))
	}
	return raw, nil
}

// dsWebCreateSession creates a server-side chat session (mirrors what the web
// page / deepseek-pp does before the first completion). Best-effort: a failure
// returns "" so callers can still fall back to a temporary session.
func dsWebCreateSession(reg *Registry, p *Provider) (string, error) {
	raw, err := dsWebPostJSON(reg, p, "/api/v0/chat_session/create", map[string]any{"agent": "chat"})
	if err != nil {
		return "", err
	}
	var data struct {
		BizData struct {
			ID          string `json:"id"`
			ChatSession struct {
				ID string `json:"id"`
			} `json:"chat_session"`
		} `json:"biz_data"`
	}
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		return "", err
	}
	if id := strings.TrimSpace(data.BizData.ID); id != "" {
		return id, nil
	}
	if id := strings.TrimSpace(data.BizData.ChatSession.ID); id != "" {
		return id, nil
	}
	return "", errors.New("deepseek-web: session create returned no id")
}

// dsWebEnsureSessionID creates a server-side chat session when the session has
// none yet. Failure is non-fatal (temporary sessions are still accepted by the
// completion endpoint in most cases).
func dsWebEnsureSessionID(reg *Registry, p *Provider, s *DeepseekWebSession) {
	if s.ChatSessionID != "" {
		return
	}
	if id, err := dsWebCreateSession(reg, p); err == nil && id != "" {
		s.ChatSessionID = id
	}
}

// dsWebFetchPowChallenge obtains a fresh PoW challenge for the completion
// target from POST /api/v0/chat/create_pow_challenge.
func dsWebFetchPowChallenge(reg *Registry, p *Provider) (*deepseekPowChallenge, error) {
	raw, err := dsWebPostJSON(reg, p, "/api/v0/chat/create_pow_challenge", map[string]any{"target_path": "/api/v0/chat/completion"})
	if err != nil {
		return nil, err
	}
	var resp struct {
		BizData struct {
			Challenge map[string]json.RawMessage `json:"challenge"`
		} `json:"biz_data"`
	}
	if err := json.Unmarshal(raw["data"], &resp); err != nil {
		return nil, err
	}
	ch := resp.BizData.Challenge
	if len(ch) == 0 {
		return nil, errors.New("deepseek-web: pow challenge missing in response")
	}
	getStr := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := ch[k]; ok {
				var s string
				if json.Unmarshal(v, &s) == nil {
					return s
				}
			}
		}
		return ""
	}
	getInt := func(keys ...string) int64 {
		for _, k := range keys {
			if v, ok := ch[k]; ok {
				var n int64
				if json.Unmarshal(v, &n) == nil {
					return n
				}
				var f float64
				if json.Unmarshal(v, &f) == nil {
					return int64(f)
				}
			}
		}
		return 0
	}
	return &deepseekPowChallenge{
		Algorithm:  getStr("algorithm"),
		Challenge:  getStr("challenge"),
		Salt:       getStr("salt"),
		ExpireAt:   getInt("expireAt", "expire_at"),
		Difficulty: getInt("difficulty"),
		Signature:  getStr("signature"),
		TargetPath: getStr("target_path", "targetPath"),
	}, nil
}

// dsWebEnsurePowHeader returns a valid x-ds-pow-response header for the
// completion target, solving a fresh challenge when the cached one has expired
// (or is missing). The header is cached on the session until expireAt.
func dsWebEnsurePowHeader(reg *Registry, p *Provider, s *DeepseekWebSession) (string, error) {
	now := time.Now().Unix()
	if s.PowHeader != "" && s.PowExpireAt > now+5 {
		return s.PowHeader, nil
	}
	ch, err := dsWebFetchPowChallenge(reg, p)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	header, err := deepseekPowSolveAndBuildHeader(ctx, ch)
	if err != nil {
		return "", err
	}
	s.PowHeader = header
	s.PowExpireAt = ch.ExpireAt
	return header, nil
}

// dsWebNeedsPowRefresh reports whether a completion failure hints at an
// expired/invalid PoW (so the caller should solve a fresh challenge and retry).
func dsWebNeedsPowRefresh(status int, body string) bool {
	if status == http.StatusForbidden || status == http.StatusTooManyRequests {
		return true
	}
	b := strings.ToLower(body)
	for _, m := range []string{"pow", "challenge", "verification failed", "proof-of-work", "anti-crawl"} {
		if strings.Contains(b, m) {
			return true
		}
	}
	return false
}

// ---- SSE event parsing (pure, testable) ----

// parseDsWebEvent extracts one SSE payload's answer and reasoning deltas. It
// supports both the OpenAI-style stream (choices[].delta.content /
// choices[].delta.reasoning_content, with a top-level "v" carrying reasoning
// in the web UI flavor) and DeepSeek's JSON-patch flavor used by deepseek-pp
// ({"p":".../reasoning_content"|".../content","o":"APPEND","v":"..."} and the
// bare {"v":"..."} shorthand). Returns ok=false only when the payload is not
// valid DeepSeek web JSON.
func parseDsWebEvent(payload []byte) (answer, reasoning, reqMsgID, respMsgID string, finished, ok bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return "", "", "", "", false, false
	}
	if len(raw) == 0 {
		return "", "", "", "", false, false
	}

	// JSON-patch flavor (deepseek-pp): {"p":"...","o":"APPEND","v":"..."}.
	if p, has := raw["p"]; has {
		var path string
		if json.Unmarshal(p, &path) != nil {
			return "", "", "", "", false, true
		}
		var v string
		if vv, ok2 := raw["v"]; ok2 {
			_ = json.Unmarshal(vv, &v)
		}
		reqMsgID, respMsgID = extractMsgIDs(raw)
		if v == "" {
			return "", "", reqMsgID, respMsgID, false, true
		}
		if isThinkingPath(path) {
			return "", v, reqMsgID, respMsgID, false, true
		}
		if isContentPath(path) {
			return v, "", reqMsgID, respMsgID, false, true
		}
		// unrecognized patch path: skip but still a valid event.
		return "", "", reqMsgID, respMsgID, false, true
	}

	// OpenAI-style flavor: choices[].delta.{content,reasoning_content}.
	if ch, has := raw["choices"]; has {
		var choices []struct {
			Delta struct {
				Content    string `json:"content"`
				Reasoning  string `json:"reasoning_content"`
				Reasoning2 string `json:"reasoning"`
			} `json:"delta"`
			FinishReason interface{} `json:"finish_reason"`
		}
		if json.Unmarshal(ch, &choices) == nil && len(choices) > 0 {
			c := choices[0]
			answer = c.Delta.Content
			reasoning = c.Delta.Reasoning
			if reasoning == "" {
				reasoning = c.Delta.Reasoning2
			}
			// Top-level "v" carries reasoning in the web UI flavor when no
			// reasoning_content is present (faithful to prior yami behavior).
			if reasoning == "" {
				if vv, ok2 := raw["v"]; ok2 {
					var v string
					if json.Unmarshal(vv, &v) == nil && v != "" {
						reasoning = v
					}
				}
			}
			reqMsgID, respMsgID = extractMsgIDs(raw)
			if c.FinishReason != nil {
				if s, ok := c.FinishReason.(string); ok && s != "" {
					finished = true
				}
			}
			return answer, reasoning, reqMsgID, respMsgID, finished, true
		}
	}

	// Bare shorthand {"v":"..."} (no p, no choices): append to the answer
	// channel by default (deepseek-pp routes unknown fragments to answer).
	if vv, has := raw["v"]; has {
		var v string
		if json.Unmarshal(vv, &v) == nil && v != "" {
			reqMsgID, respMsgID = extractMsgIDs(raw)
			return v, "", reqMsgID, respMsgID, false, true
		}
	}

	// Event carrying only message ids (e.g. parent linkage).
	if _, has := raw["response_message_id"]; has {
		reqMsgID, respMsgID = extractMsgIDs(raw)
		return "", "", reqMsgID, respMsgID, false, true
	}
	if _, has := raw["request_message_id"]; has {
		reqMsgID, respMsgID = extractMsgIDs(raw)
		return "", "", reqMsgID, respMsgID, false, true
	}

	return "", "", "", "", false, true
}

// dsWebEventIsAuth reports whether a payload is a DeepSeek auth/error event.
func dsWebEventIsAuth(payload []byte) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return false
	}
	for _, key := range []string{"code", "biz_code"} {
		if v, ok := raw[key]; ok {
			var c int64
			if json.Unmarshal(v, &c) == nil && (c == 40002 || c == 40003) {
				return true
			}
		}
	}
	for _, key := range []string{"message", "msg"} {
		if v, ok := raw[key]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil && loginMarker(s) {
				return true
			}
		}
	}
	return false
}

func isThinkingPath(p string) bool {
	last := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		last = p[i+1:]
	}
	return last == "reasoning_content" || last == "thinking_content"
}

func isContentPath(p string) bool {
	last := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		last = p[i+1:]
	}
	return last == "content" || last == "text" || last == "markdown" || last == "delta"
}

func extractMsgIDs(raw map[string]json.RawMessage) (reqID, respID string) {
	for _, key := range []string{"response_message_id", "responseMessageId"} {
		if v, ok := raw[key]; ok {
			if s := jsonScalarString(v); s != "" {
				respID = s
			}
		}
	}
	for _, key := range []string{"request_message_id", "requestMessageId"} {
		if v, ok := raw[key]; ok {
			if s := jsonScalarString(v); s != "" {
				reqID = s
			}
		}
	}
	return
}

func jsonScalarString(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	var n int64
	if json.Unmarshal(v, &n) == nil {
		return strconv.FormatInt(n, 10)
	}
	return ""
}

// IsDeepseekLoginRequired inspects an HTTP status / body for login-expired
// signals so the UI can tell the user to re-authenticate in the WebView.
func IsDeepseekLoginRequired(status int, body string) bool {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return true
	}
	b := strings.ToLower(body)
	for _, m := range []string{
		"请登录", "未授权", "登录失效", "需要登录",
		"needs login", "please log in", "please sign in",
		"not logged in", "unauthorized",
	} {
		if strings.Contains(b, m) {
			return true
		}
	}
	if strings.Contains(b, "\"code\":40002") || strings.Contains(b, "\"code\":40003") ||
		strings.Contains(b, "\"biz_code\":40002") || strings.Contains(b, "\"biz_code\":40003") {
		return true
	}
	return false
}

func loginMarker(s string) bool {
	b := strings.ToLower(s)
	for _, m := range []string{
		"请登录", "未授权", "登录失效", "needs login", "please log in",
		"please sign in", "not logged in", "unauthorized", "login required",
	} {
		if strings.Contains(b, m) {
			return true
		}
	}
	return false
}

// formatDeepseekPrompt renders the full conversation as a single prompt so the
// free bridge keeps multi-turn context without depending on server-side
// session ids (DeepSeek web keeps context server-side too, but sending the
// transcript guarantees continuity and costs no API key).
func formatDeepseekPrompt(msgs []ChatMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		role := m.Role
		if role == "" {
			role = "user"
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		switch role {
		case "system":
			b.WriteString("System: " + content + "\n\n")
		case "assistant":
			b.WriteString("Assistant: " + content + "\n\n")
		default:
			b.WriteString("User: " + content + "\n\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func buildDsWebCompletionBody(s *DeepseekWebSession, prompt, modelType string) map[string]interface{} {
	body := map[string]interface{}{
		"chat_session_id":  s.ChatSessionID,
		"model_type":       modelType,
		"prompt":           prompt,
		"ref_file_ids":     []string{},
		"thinking_enabled": true,
		"search_enabled":   false,
		"action":           nil,
		"preempt":          false,
	}
	if s.ParentMessageID != "" {
		if id, err := strconv.ParseInt(s.ParentMessageID, 10, 64); err == nil {
			body["parent_message_id"] = id
		} else {
			body["parent_message_id"] = s.ParentMessageID
		}
	} else {
		body["parent_message_id"] = nil
	}
	body["temporary"] = s.ChatSessionID == ""
	return body
}

// callDeepseekWebStream drives the logged-in DeepSeek *web* session (the free
// ride path: no API key, it reuses the user's browser session cookie exported
// from the in-app WebView, exactly like deepseek-pp reuses the page login
// state). It parses the SSE stream, maintains server-side session state, and
// forwards answer/reasoning deltas to onChunk. It returns the updated session
// plus the full answer and reasoning texts.
func callDeepseekWebStream(reg *Registry, p *Provider, req *ChatCompletionRequest, onChunk func(answer, reasoning string)) (*DeepseekWebSession, string, string, error) {
	store := DefaultDeepseekWebSessions
	session := store.GetOrCreate(req.SessionID)

	prompt := formatDeepseekPrompt(req.Messages)

	modelType := "default"
	modelName := "deepseek-chat"
	if strings.Contains(strings.ToLower(req.Model), "reasoner") {
		modelType = "expert"
		modelName = "deepseek-reasoner"
	}

	// chat.deepseek.com now requires a server-side chat session + PoW header on
	// completion (same as the web page / deepseek-pp flow). Bootstrap both and
	// cache the PoW answer on the session until it expires.
	dsWebEnsureSessionID(reg, p, session)

	// Build the body AFTER session bootstrap so the first request already
	// carries the server-side chat_session_id (temporary sessions with an empty
	// id are accepted in most cases but the real session is safer).
	var (
		body    map[string]interface{}
		buf     []byte
		resp    *http.Response
		respErr error
	)
	body = buildDsWebCompletionBody(session, prompt, modelType)
	buf, err := json.Marshal(body)
	if err != nil {
		return session, "", "", err
	}

	for attempt := 0; attempt < 2; attempt++ {
		powHeader, err := dsWebEnsurePowHeader(reg, p, session)
		if err != nil {
			return session, "", "", fmt.Errorf("deepseek-web pow: %w", err)
		}

		hreq, err := http.NewRequest(http.MethodPost, dsWebCompletionURL(), bytes.NewReader(buf))
		if err != nil {
			return session, "", "", err
		}
		hreq.Header.Set("Content-Type", "application/json")
		hreq.Header.Set("Accept", "text/event-stream")
		hreq.Header.Set("x-ds-pow-response", powHeader)
		if p.Cookies == "" && p.Token == "" {
			return session, "", "", ErrDeepseekLoginRequired
		}
		applyDsWebClientHeaders(hreq, p)

		resp, respErr = httpClientFor(reg, p).Do(hreq)
		if respErr != nil {
			return session, "", "", respErr
		}
		if resp.StatusCode == http.StatusOK {
			break
		}
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if IsDeepseekLoginRequired(resp.StatusCode, string(snippet)) {
			return session, "", "", ErrDeepseekLoginRequired
		}
		// PoW expired mid-flight: solve a fresh challenge and retry once.
		if dsWebNeedsPowRefresh(resp.StatusCode, string(snippet)) && attempt == 0 {
			session.PowHeader = ""
			session.PowExpireAt = 0
			continue
		}
		return session, "", "", fmt.Errorf("deepseek-web %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	if resp == nil {
		return session, "", "", respErr
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if IsDeepseekLoginRequired(resp.StatusCode, string(snippet)) {
			return session, "", "", ErrDeepseekLoginRequired
		}
		return session, "", "", fmt.Errorf("deepseek-web unexpected response: %s", strings.TrimSpace(string(snippet)))
	}

	var answer, reasoning strings.Builder
	lines, err := sseReader(resp.Body)
	if err != nil {
		return session, "", "", err
	}
	for _, line := range lines {
		if dsWebEventIsAuth([]byte(line)) {
			return session, "", "", ErrDeepseekLoginRequired
		}
		a, r, reqID, respID, _, ok := parseDsWebEvent([]byte(line))
		if !ok {
			continue
		}
		if a != "" {
			answer.WriteString(a)
			if onChunk != nil {
				onChunk(a, "")
			}
		}
		if r != "" {
			reasoning.WriteString(r)
			if onChunk != nil {
				onChunk("", r)
			}
		}
		if respID != "" {
			session.ParentMessageID = respID
		}
		if reqID != "" {
			session.RequestMessageID = reqID
		}
	}

	session.Model = modelName
	session.Reasoning = reasoning.String()
	if session.Key != "" {
		store.Put(session)
	}
	return session, answer.String(), reasoning.String(), nil
}

// callDeepseekWeb is the non-streaming bridge used by relay's failover
// dispatch. Signature is preserved for总办's routing; it buffers the stream and
// returns a standard ChatCompletionResponse (answer only; reasoning is kept in
// the session store for retrieval).
func callDeepseekWeb(reg *Registry, p *Provider, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	_, answer, _, err := callDeepseekWebStream(reg, p, req, nil)
	if err != nil {
		return nil, err
	}
	modelName := "deepseek-chat"
	if strings.Contains(strings.ToLower(req.Model), "reasoner") {
		modelName = "deepseek-reasoner"
	}
	return &ChatCompletionResponse{
		ID:      "dsweb-yami",
		Object:  "chat.completion",
		Created: nowSeconds(),
		Model:   modelName,
		Choices: []Choice{{Index: 0, Message: ChatMessage{Role: "assistant", Content: answer}, FinishReason: "stop"}},
	}, nil
}

// StreamDeepseekWeb is the streaming entry point for the free DeepSeek web
// bridge. It maintains the conversation via req.SessionID (server-side session
// + full-history prompt) and forwards answer/reasoning deltas to onChunk.
// 总办 should call this (instead of complete) when streaming a deepseek-web
// provider to the client for true token-by-token forwarding.
func StreamDeepseekWeb(reg *Registry, p *Provider, req *ChatCompletionRequest, onChunk func(answer, reasoning string)) (answer, reasoning string, err error) {
	_, a, r, e := callDeepseekWebStream(reg, p, req, onChunk)
	return a, r, e
}
