package ai

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ---- OpenAI-compatible wire types ----

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ChatMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Name      string     `json:"name,omitempty"`
}

type Tool struct {
	Type     string `json:"type"`
	Function ToolDef `json:"function"`
}

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Tools       []Tool        `json:"tools,omitempty"`
	Provider    string        `json:"provider,omitempty"` // routing override
	Temperature float64       `json:"temperature,omitempty"`
	SessionID   string        `json:"session_id,omitempty"` // enables the 省token session layer
}

type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type StreamChoice struct {
	Index        int    `json:"index"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}

type StreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

// Relay is the local OpenAI-compatible server.
type Relay struct {
	reg      *Registry
	browser  BrowserDriver
	caps     CaptureSource
	sessions *SessionStore // 省token 模式：会话记忆 + 压缩
}

// NewRelay builds a relay over a provider registry.
func NewRelay(reg *Registry) *Relay {
	s := NewSessionStore()
	rl := &Relay{reg: reg, sessions: s}
	// Model-backed summarizer for compaction (uses the active provider). If it
	// fails, compaction falls back to an extractive summary (see session.go).
	s.SetSummarizer(func(ctx string) (string, error) {
		req := &ChatCompletionRequest{
			Model: modelOrDefault(rl),
			Messages: []ChatMessage{
				{Role: "system", Content: "你是上下文压缩器。把对话压缩为简洁中文摘要，保留关键结论、数据、决策与未完成任务，删除寒暄与冗余。"},
				{Role: "user", Content: ctx},
			},
			Stream: false,
		}
		resp, err := rl.complete(req)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("empty summary")
		}
		return resp.Choices[0].Message.Content, nil
	})
	return rl
}

// Sessions exposes the session store (used by the agent and HTTP handlers).
func (rl *Relay) Sessions() *SessionStore { return rl.sessions }

// ApplyDefaultCompact turns on compaction when the active provider is a paid
// key (openai/anthropic with api_key), and leaves it off for the free
// deepseek-web bridge — i.e. "用密钥 token 时直接省 token".
func (rl *Relay) ApplyDefaultCompact() {
	a := rl.reg.Active()
	on := a != nil && a.Protocol != ProtoDeepseekWeb && a.APIKey != ""
	rl.sessions.SetCompactEnabled(on)
}

// SetBrowser wires the in-app browser driver (implemented by the Android layer
// via the WebView JS bridge). Nil means browser tools are unavailable.
func (rl *Relay) SetBrowser(b BrowserDriver) { rl.browser = b }

// SetCaptureSource wires the captured-traffic source for analysis tools.
func (rl *Relay) SetCaptureSource(c CaptureSource) { rl.caps = c }

// CaptureSource returns the wired capture source (or nil).
func (rl *Relay) CaptureSource() CaptureSource { return rl.caps }

// Handler returns the relay mux.
func (rl *Relay) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", rl.chatCompletions)
	mux.HandleFunc("/v1/models", rl.models)
	mux.HandleFunc("/ai/analyze", rl.analyze) // capture analysis helper
	mux.HandleFunc("/ai/chat", rl.agentChat)  // agent loop (browser automation)
	// 省token 模式 / 会话管理
	mux.HandleFunc("/ai/compact", rl.setCompact)
	mux.HandleFunc("/ai/compact_ratio", rl.setCompactRatio)
	mux.HandleFunc("/ai/session/new", rl.sessionNew)
	mux.HandleFunc("/ai/session/list", rl.sessionList)
	mux.HandleFunc("/ai/session/clear", rl.sessionClear)
	// 会话导出/导入、Provider 健康、Agent 动作清单（差距补全收口）
	mux.HandleFunc("/ai/session/export", rl.sessionExport)
	mux.HandleFunc("/ai/session/import", rl.sessionImport)
	mux.HandleFunc("/ai/providers", rl.providers)
	mux.HandleFunc("/ai/agent/actions", rl.agentActions)
	return mux
}

// ChatSession runs a single user turn inside a persistent session (省token).
// It appends the prompt, completes, appends the reply, and compacts.
func (rl *Relay) ChatSession(sessionID, prompt string) (string, error) {
	if sessionID == "" {
		sessionID = rl.sessions.GetOrCreate("").ID
	}
	ses := rl.sessions.GetOrCreate(sessionID)
	ses.Messages = append(ses.Messages, ChatMessage{Role: "user", Content: prompt})
	req := &ChatCompletionRequest{Model: modelOrDefault(rl), Messages: ses.Messages, Stream: false}
	resp, err := rl.complete(req)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty completion")
	}
	ses.Messages = append(ses.Messages, resp.Choices[0].Message)
	rl.sessions.Compact(ses)
	return resp.Choices[0].Message.Content, nil
}

// ---- 省token / session HTTP helpers (served on the AI relay port) ----

func (rl *Relay) setCompact(w http.ResponseWriter, r *http.Request) {
	on := r.FormValue("on") == "1" || r.FormValue("on") == "true"
	rl.sessions.SetCompactEnabled(on)
	writeJSON(w, map[string]bool{"compact": rl.sessions.IsCompactEnabled()})
}

func (rl *Relay) setCompactRatio(w http.ResponseWriter, r *http.Request) {
	if v := r.FormValue("ratio"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			rl.sessions.SetCompactRatio(f)
		}
	}
	writeJSON(w, map[string]float64{"ratio": rl.sessions.CompactRatio()})
}

func (rl *Relay) sessionNew(w http.ResponseWriter, r *http.Request) {
	id := rl.sessions.GetOrCreate("").ID
	writeJSON(w, map[string]string{"session_id": id})
}

func (rl *Relay) sessionList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string][]string{"sessions": rl.sessions.List()})
}

func (rl *Relay) sessionClear(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("id")
	before := len(rl.sessions.List())
	rl.sessions.Clear(id)
	writeJSON(w, map[string]int{"cleared": before})
}

// sessionExport returns one session as JSON (id from "id" form value).
func (rl *Relay) sessionExport(w http.ResponseWriter, r *http.Request) {
	ses := rl.sessions.GetOrCreate(r.FormValue("id"))
	b, err := ses.ExportJSON()
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// sessionImport loads a session JSON (body {"json":"<session json>"}) into id.
func (rl *Relay) sessionImport(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("id")
	var body struct {
		JSON string `json:"json"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ses := rl.sessions.GetOrCreate(id)
	if err := ses.ImportJSON([]byte(body.JSON)); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"ok": "1"})
}

// providers reports each provider's health and whether it is active.
func (rl *Relay) providers(w http.ResponseWriter, r *http.Request) {
	type ph struct {
		Name    string `json:"name"`
		Healthy bool   `json:"healthy"`
		Active  bool   `json:"active"`
	}
	active := rl.reg.Active()
	out := make([]ph, 0, len(rl.reg.List()))
	for _, p := range rl.reg.List() {
		a := active != nil && active.Name == p.Name
		out = append(out, ph{p.Name, p.Healthy, a})
	}
	writeJSON(w, out)
}

// agentActions lists the browser/agent tool schemas the agent can call.
func (rl *Relay) agentActions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, AvailableActions(true, true))
}

// Listen starts the relay (blocking).
func (rl *Relay) Listen(addr string) error {
	return http.ListenAndServe(addr, rl.Handler())
}

func (rl *Relay) models(w http.ResponseWriter, r *http.Request) {
	var names []string
	for _, p := range rl.reg.List() {
		names = append(names, fmt.Sprintf("%s/%s", p.Name, p.Model))
	}
	writeJSON(w, map[string]interface{}{"object": "list", "data": names})
}

// analyze runs the capture-analyst persona over a captured transaction.
func (rl *Relay) analyze(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt     string `json:"prompt"`
		CaptureID  string `json:"capture_id"`
		CaptureCtx string `json:"capture_ctx"` // optional pre-rendered context
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx := in.CaptureCtx
	if ctx == "" && rl.caps != nil && in.CaptureID != "" {
		for _, c := range rl.caps.Captures() {
			if c.ID == in.CaptureID {
				ctx = renderCapture(c)
			}
		}
	}
	prompt := in.Prompt
	if ctx != "" {
		prompt = "下面是抓到的请求：\n" + ctx + "\n\n用户问题：" + in.Prompt
	}
	out, err := rl.Ask(SystemPromptCaptureAnalyst, prompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"result": out})
}

// agentChat runs the browser-operator agent loop over a task.
func (rl *Relay) agentChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Task      string `json:"task"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	agent := NewAgent(rl, rl.browser, rl.caps)
	agent.SetSession(in.SessionID)
	out, err := agent.Run(in.Task)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"result": out})
}

// Ask is a plain (no-tool) completion used by analyze and simple chat.
func (rl *Relay) Ask(system, user string) (string, error) {
	req := &ChatCompletionRequest{
		Model:    modelOrDefault(rl),
		Messages: []ChatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Stream:   false,
	}
	resp, err := rl.complete(req)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty completion")
	}
	return resp.Choices[0].Message.Content, nil
}

func renderCapture(c Capture) string {
	return fmt.Sprintf("METHOD=%s URL=%s\nREQ_HEADERS=%v\nREQ_BODY=%s\nRESP_HEADERS=%v\nRESP_BODY=%s",
		c.Method, c.URL, c.ReqHeaders, c.ReqBody, c.RespHeaders, truncate(c.RespBody, 4000))
}

func (rl *Relay) chatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		if a := rl.reg.Active(); a != nil {
			req.Model = a.Model
		}
	}

	// ---- 省token 会话层 ----
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = r.Header.Get("X-Yami-Session")
	}
	if sessionID != "" {
		ses := rl.sessions.GetOrCreate(sessionID)
		// new user turn = last user message in the request
		if len(req.Messages) > 0 {
			if last := req.Messages[len(req.Messages)-1]; last.Role == "user" {
				ses.Messages = append(ses.Messages, ChatMessage{Role: "user", Content: last.Content})
			}
		}
		req.Messages = ses.Messages // full history goes to the model
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			full := rl.stream(w, &req)
			if full != "" {
				ses.Messages = append(ses.Messages, ChatMessage{Role: "assistant", Content: full})
				rl.sessions.Compact(ses)
			}
			return
		}
		resp, err := rl.complete(&req)
		if err != nil {
			http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
			return
		}
		if len(resp.Choices) > 0 {
			ses.Messages = append(ses.Messages, resp.Choices[0].Message)
			rl.sessions.Compact(ses)
		}
		writeJSON(w, resp)
		return
	}

	// ---- stateless (backward compatible) ----
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		rl.stream(w, &req)
		return
	}
	resp, err := rl.complete(&req)
	if err != nil {
		http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, resp)
}

// complete tries candidates in failover order and returns the first success.
func (rl *Relay) complete(req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	// Optional explicit provider routing.
	if req.Provider != "" {
		rl.reg.mu.RLock()
		p := rl.reg.providers[req.Provider]
		rl.reg.mu.RUnlock()
		if p != nil {
			return rl.callProvider(p, req)
		}
	}
	var lastErr error
	for _, p := range rl.reg.candidateOrder() {
		if !p.Healthy {
			continue
		}
		resp, err := rl.callProvider(p, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		p.Healthy = false
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no healthy provider")
	}
	return nil, lastErr
}

// stream proxies/synthesizes an SSE response and returns the aggregated
// assistant content (empty on error), so callers can persist it to a session.
func (rl *Relay) stream(w http.ResponseWriter, req *ChatCompletionRequest) string {
	fw := &flushWriter{w: w}
	resp, err := rl.complete(req)
	if err != nil {
		fmt.Fprintf(w, "data: {\"error\":%q}\n\n", err.Error())
		fw.flush()
		return ""
	}
	var full strings.Builder
	for i, c := range resp.Choices {
		full.WriteString(c.Message.Content)
		chunk := StreamChunk{
			ID:      resp.ID,
			Object:  "chat.completion.chunk",
			Created: resp.Created,
			Model:   resp.Model,
			Choices: []StreamChoice{{
				Index: i,
				Delta: Delta{Role: "assistant", Content: c.Message.Content},
				// FinishReason only on the last choice for simplicity.
			}},
		}
		if i == len(resp.Choices)-1 {
			chunk.Choices[0].FinishReason = "stop"
		}
		fmt.Fprintf(fw.w, "data: %s\n\n", string(mustJSON(chunk)))
		fw.flush()
	}
	fmt.Fprintf(fw.w, "data: [DONE]\n\n")
	fw.flush()
	return full.String()
}

// callProvider dispatches to the right backend based on protocol.
func (rl *Relay) callProvider(p *Provider, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	switch p.Protocol {
	case ProtoAnthropic:
		return callAnthropic(rl.reg, p, req)
	case ProtoDeepseekWeb:
		return callDeepseekWeb(rl.reg, p, req)
	default:
		return callOpenAI(rl.reg, p, req)
	}
}

// ---- small helpers ----

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

type flushWriter struct{ w io.Writer }

func (f *flushWriter) flush() {
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
}

// sseReader yields data payloads from an SSE byte stream.
func sseReader(r io.Reader) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				continue
			}
			out = append(out, payload)
		}
	}
	return out, sc.Err()
}

// nowSeconds is a tiny helper for stamping responses.
func nowSeconds() int64 { return time.Now().Unix() }
