package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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

	body := buildDsWebCompletionBody(session, prompt, modelType)
	buf, err := json.Marshal(body)
	if err != nil {
		return session, "", "", err
	}

	hreq, err := http.NewRequest(http.MethodPost, dsWebCompletionURL(), bytes.NewReader(buf))
	if err != nil {
		return session, "", "", err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "text/event-stream")
	if p.Cookies == "" {
		return session, "", "", ErrDeepseekLoginRequired
	}
	hreq.Header.Set("Cookie", p.Cookies)

	resp, err := httpClientFor(reg, p).Do(hreq)
	if err != nil {
		return session, "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if IsDeepseekLoginRequired(resp.StatusCode, string(snippet)) {
			return session, "", "", ErrDeepseekLoginRequired
		}
		return session, "", "", fmt.Errorf("deepseek-web %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

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
