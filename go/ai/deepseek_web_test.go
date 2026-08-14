package ai

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestParseDsWebEventAnswer(t *testing.T) {
	a, r, _, _, fin, ok := parseDsWebEvent([]byte(`{"id":"x","choices":[{"delta":{"content":"你好"}}]}`))
	if !ok || a != "你好" || r != "" || fin {
		t.Fatalf("got a=%q r=%q fin=%v ok=%v", a, r, fin, ok)
	}
}

func TestParseDsWebEventReasoning(t *testing.T) {
	a, r, _, _, _, ok := parseDsWebEvent([]byte(`{"id":"x","choices":[{"delta":{"content":"A","reasoning_content":"让我想想"}}]}`))
	if !ok || a != "A" || r != "让我想想" {
		t.Fatalf("got a=%q r=%q ok=%v", a, r, ok)
	}
}

func TestParseDsWebEventVReasoningWithEmptyContent(t *testing.T) {
	// Web-UI flavor: reasoning only, empty content, top-level v carries reasoning.
	a, r, _, _, _, ok := parseDsWebEvent([]byte(`{"id":"x","v":"思考中","choices":[{"delta":{"content":""}}]}`))
	if !ok || a != "" || r != "思考中" {
		t.Fatalf("got a=%q r=%q ok=%v", a, r, ok)
	}
}

func TestParseDsWebEventPatchReasoning(t *testing.T) {
	a, r, _, _, _, ok := parseDsWebEvent([]byte(`{"p":"response/fragments/-1/reasoning_content","o":"APPEND","v":"链"}`))
	if !ok || a != "" || r != "链" {
		t.Fatalf("got a=%q r=%q ok=%v", a, r, ok)
	}
}

func TestParseDsWebEventPatchContent(t *testing.T) {
	a, r, _, _, _, ok := parseDsWebEvent([]byte(`{"p":"response/fragments/-1/content","o":"APPEND","v":"答"}`))
	if !ok || a != "答" || r != "" {
		t.Fatalf("got a=%q r=%q ok=%v", a, r, ok)
	}
}

func TestParseDsWebEventBareVShorthand(t *testing.T) {
	// deepseek-pp shorthand: bare v routes to the answer channel by default.
	a, r, _, _, _, ok := parseDsWebEvent([]byte(`{"v":"hello"}`))
	if !ok || a != "hello" || r != "" {
		t.Fatalf("got a=%q r=%q ok=%v", a, r, ok)
	}
}

func TestParseDsWebEventMessageIDs(t *testing.T) {
	_, _, reqID, respID, _, ok := parseDsWebEvent([]byte(`{"response_message_id":123,"request_message_id":122}`))
	if !ok || respID != "123" || reqID != "122" {
		t.Fatalf("got req=%q resp=%q ok=%v", reqID, respID, ok)
	}
}

func TestParseDsWebEventFinish(t *testing.T) {
	_, _, _, _, fin, ok := parseDsWebEvent([]byte(`{"id":"x","choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`))
	if !ok || !fin {
		t.Fatalf("expected finished, got ok=%v fin=%v", ok, fin)
	}
}

func TestParseDsWebEventInvalid(t *testing.T) {
	_, _, _, _, _, ok := parseDsWebEvent([]byte(`not json`))
	if ok {
		t.Fatal("expected ok=false for invalid json")
	}
}

func TestParseDsWebEventMultiConcat(t *testing.T) {
	events := []string{
		`{"id":"x","choices":[{"delta":{"content":"你"}}]}`,
		`{"id":"x","choices":[{"delta":{"content":"好"}}]}`,
		`{"id":"x","choices":[{"delta":{"reasoning_content":"想"}}]}`,
		`{"id":"x","choices":[{"delta":{"content":"啊"}}]}`,
	}
	var ans, rea strings.Builder
	for _, e := range events {
		a, r, _, _, _, ok := parseDsWebEvent([]byte(e))
		if !ok {
			t.Fatalf("parse failed for %s", e)
		}
		ans.WriteString(a)
		rea.WriteString(r)
	}
	if ans.String() != "你好啊" || rea.String() != "想" {
		t.Fatalf("concat ans=%q rea=%q", ans.String(), rea.String())
	}
}

func TestDsWebEventIsAuth(t *testing.T) {
	if !dsWebEventIsAuth([]byte(`{"code":40002,"message":"请登录"}`)) {
		t.Fatal("expected auth detection for code 40002")
	}
	if !dsWebEventIsAuth([]byte(`{"biz_code":40003,"msg":"unauthorized"}`)) {
		t.Fatal("expected auth detection for biz_code 40003")
	}
	if dsWebEventIsAuth([]byte(`{"id":"x","choices":[{"delta":{"content":"hi"}}]}`)) {
		t.Fatal("normal event misclassified as auth")
	}
}

func TestIsDeepseekLoginRequired(t *testing.T) {
	if !IsDeepseekLoginRequired(http.StatusUnauthorized, "") {
		t.Fatal("401 should be login required")
	}
	if !IsDeepseekLoginRequired(http.StatusOK, `{"message":"请登录后再试"}`) {
		t.Fatal("login text should be detected")
	}
	if IsDeepseekLoginRequired(http.StatusOK, `{"choices":[]}`) {
		t.Fatal("normal body should not be flagged")
	}
}

func TestFormatDeepseekPrompt(t *testing.T) {
	p := formatDeepseekPrompt([]ChatMessage{
		{Role: "system", Content: "你是助手"},
		{Role: "user", Content: "你好"},
		{Role: "assistant", Content: "在的"},
		{Role: "user", Content: "继续"},
	})
	want := "System: 你是助手\n\nUser: 你好\n\nAssistant: 在的\n\nUser: 继续"
	if p != want {
		t.Fatalf("prompt=\n%q\nwant=\n%q", p, want)
	}
}

func TestDeepseekWebSessionStore(t *testing.T) {
	s := NewDeepseekWebSessionStore()
	ses := s.GetOrCreate("k1")
	ses.ParentMessageID = "42"
	s.Put(ses)
	got, ok := s.Get("k1")
	if !ok || got.ParentMessageID != "42" {
		t.Fatalf("store get failed: ok=%v id=%q", ok, got.ParentMessageID)
	}
	if len(s.List()) != 1 {
		t.Fatalf("list len=%d", len(s.List()))
	}
	s.Clear("k1")
	if _, ok := s.Get("k1"); ok {
		t.Fatal("clear failed")
	}
	// empty key is ephemeral
	if s.GetOrCreate("").Key != "" {
		t.Fatal("empty key should be ephemeral")
	}
}

func TestCallDeepseekWebViaMock(t *testing.T) {
	be := newMockDeepseekWeb()
	defer be.Close()
	old := dsWebBaseURL
	dsWebBaseURL = be.URL
	defer func() { dsWebBaseURL = old }()

	p := &Provider{Name: "ds", Protocol: ProtoDeepseekWeb, Cookies: "session=abc"}
	reg := NewRegistry()
	reg.AddProvider(p)

	resp, err := callDeepseekWeb(reg, p, &ChatCompletionRequest{
		Model:    "deepseek-chat",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !strings.Contains(resp.Choices[0].Message.Content, "白嫖") {
		t.Fatalf("content=%q", resp.Choices[0].Message.Content)
	}

	// missing cookie => login required
	p2 := &Provider{Name: "ds2", Protocol: ProtoDeepseekWeb, Cookies: ""}
	_, err = callDeepseekWeb(reg, p2, &ChatCompletionRequest{
		Model:    "deepseek-chat",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, ErrDeepseekLoginRequired) {
		t.Fatalf("expected login required, got %v", err)
	}
}

func TestStreamDeepseekWebViaMock(t *testing.T) {
	be := newMockDeepseekWeb()
	defer be.Close()
	old := dsWebBaseURL
	dsWebBaseURL = be.URL
	defer func() { dsWebBaseURL = old }()

	p := &Provider{Name: "ds", Protocol: ProtoDeepseekWeb, Cookies: "session=abc"}
	reg := NewRegistry()
	reg.AddProvider(p)

	var ans, rea strings.Builder
	a, _, err := StreamDeepseekWeb(reg, p, &ChatCompletionRequest{
		Model:      "deepseek-chat",
		SessionID:  "sess-1",
		Messages:   []ChatMessage{{Role: "user", Content: "hi"}},
		Stream:     true,
	}, func(answer, reasoning string) {
		ans.WriteString(answer)
		rea.WriteString(reasoning)
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if a != "你好，我是白嫖的 DeepSeek" {
		t.Fatalf("answer=%q", a)
	}
	if rea.String() != "" {
		t.Fatalf("reasoning should be empty for mock, got %q", rea.String())
	}
	// session persisted with reasoning
	ses, ok := GetDeepseekWebSessionStore().Get("sess-1")
	if !ok || ses.Reasoning != "" {
		t.Fatalf("session not persisted correctly: ok=%v ses=%+v", ok, ses)
	}
}
