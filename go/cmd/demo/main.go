// Command demo proves the yami-UA closed loop without any external network:
// it stands up the AI relay + capture store, feeds a synthetic "login" capture,
// and asks the (mock) LLM to analyze it. Run with: go run ./cmd/demo
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"yamiua/ai"
	"yamiua/proxy"
)

// localCaptureSource adapts proxy.Store for the demo.
type localCaptureSource struct{ store *proxy.Store }

func (c *localCaptureSource) Captures() []ai.Capture {
	var out []ai.Capture
	for _, r := range c.store.List() {
		out = append(out, ai.Capture{
			ID: fmt.Sprintf("%d", r.ID), Method: r.Method, URL: r.URL,
			ReqHeaders: r.ReqHeaders, RespHeaders: r.RespHeaders,
			ReqBody: r.ReqBody, RespBody: r.RespBody,
		})
	}
	return out
}
func (c *localCaptureSource) Tokens() []ai.Token {
	var out []ai.Token
	for _, r := range c.store.List() {
		for _, t := range r.Tokens {
			out = append(out, ai.Token{Key: t.Key, Value: t.Value, Source: t.Source})
		}
	}
	return out
}

func main() {
	// A mock OpenAI-compatible backend that behaves like a capture analyst.
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ai.ChatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		answer := "【抓包分析】\n1. 该 POST 请求在 Authorization 头里带明文 Bearer 令牌：leaked-token-123，属凭证泄露，建议改用短期令牌并走 HTTPS 且仅本机可见。\n2. 未发现明显 SQL 注入点（参数经编码）。\n3. 可复制 curl：\n   curl -X POST https://api.x.com/login -H 'Authorization: Bearer leaked-token-123'"
		resp := ai.ChatCompletionResponse{
			ID: "demo", Object: "chat.completion", Model: req.Model,
			Choices: []ai.Choice{{Index: 0, FinishReason: "stop",
				Message: ai.ChatMessage{Role: "assistant", Content: answer}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer be.Close()

	reg := ai.NewRegistry()
	reg.AddProvider(&ai.Provider{Name: "demo", BaseURL: be.URL, Model: "demo", Protocol: ai.ProtoOpenAI})

	// Feed a synthetic captured login request into the store.
	p := proxy.New("127.0.0.1:0")
	p.Store.Add(&proxy.Record{
		Method: "POST", URL: "https://api.x.com/login", IsHTTPS: true,
		ReqHeaders:  "Authorization: Bearer leaked-token-123",
		RespHeaders: "Content-Type: application/json",
		RespBody:    `{"ok":true}`,
		Tokens:      []proxy.Token{{Key: "Authorization", Value: "Bearer leaked-token-123", Source: "header", IsLogin: true}},
	})

	rl := ai.NewRelay(reg)
	rl.SetCaptureSource(&localCaptureSource{store: p.Store})
	go func() { _ = rl.Listen("127.0.0.1:8911") }()

	// Hit the real relay endpoint /ai/analyze over HTTP (the Android UI does the same).
	body, _ := json.Marshal(map[string]string{
		"capture_id": "1",
		"prompt":     "这请求有泄露吗？给个 curl",
	})
	resp, err := http.Post("http://127.0.0.1:8911/ai/analyze", "application/json", bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Result string `json:"result"`
	}
	_ = json.Unmarshal(raw, &out)

	fmt.Println("=== yami-UA 闭环演示：抓包 → AI 分析 ===")
	fmt.Println("捕获到的请求: POST https://api.x.com/login  (Authorization: Bearer leaked-token-123)")
	fmt.Println("AI 返回:\n" + out.Result)
	fmt.Printf("\n[断言] 含泄露提示=%v  含curl=%v\n",
		contains(out.Result, "leaked-token-123"), contains(out.Result, "curl"))
}

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }
