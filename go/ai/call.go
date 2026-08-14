package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// callOpenAI forwards a non-streaming request to an OpenAI-compatible backend.
func callOpenAI(reg *Registry, p *Provider, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	body := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   false,
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequest(http.MethodPost, chatCompletionURL(p), bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	resp, err := httpClientFor(reg, p).Do(hreq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(snippet))
	}
	var out ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		out.ID = "chatcmpl-yami"
	}
	if out.Created == 0 {
		out.Created = nowSeconds()
	}
	if out.Object == "" {
		out.Object = "chat.completion"
	}
	return &out, nil
}

// callAnthropic converts to Anthropic's /v1/messages format (tool use is
// best-effort: tools are summarized into the system prompt since the two
// tool-calling schemas differ substantially).
func callAnthropic(reg *Registry, p *Provider, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	var sys string
	var msgs []map[string]string
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			sys += m.Content + "\n"
		case "tool":
			msgs = append(msgs, map[string]string{"role": "user", "content": "[tool result] " + m.Content})
		default:
			msgs = append(msgs, map[string]string{"role": m.Role, "content": m.Content})
		}
	}
	if len(req.Tools) > 0 {
		sys += "\nAvailable tools: " + toolSummary(req.Tools) + "\n"
	}
	body := map[string]interface{}{
		"model":     req.Model,
		"max_tokens": 8192,
		"messages":  msgs,
	}
	if sys != "" {
		body["system"] = sys
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequest(http.MethodPost, chatCompletionURL(p), bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("anthropic-version", "2023-06-01")
	if p.APIKey != "" {
		hreq.Header.Set("x-api-key", p.APIKey)
	}
	resp, err := httpClientFor(reg, p).Do(hreq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(snippet))
	}
	var ar struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, err
	}
	var sb bytes.Buffer
	for _, c := range ar.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return &ChatCompletionResponse{
		ID:      "msg-yami",
		Object:  "chat.completion",
		Created: nowSeconds(),
		Model:   req.Model,
		Choices: []Choice{{Index: 0, Message: ChatMessage{Role: "assistant", Content: sb.String()}, FinishReason: "stop"}},
	}, nil
}

func toolSummary(tools []Tool) string {
	var parts []string
	for _, t := range tools {
		parts = append(parts, t.Function.Name+": "+t.Function.Description)
	}
	return joinComma(parts)
}

func lastUserContent(msgs []ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

var errNoToolCall = errors.New("no tool call in response")
