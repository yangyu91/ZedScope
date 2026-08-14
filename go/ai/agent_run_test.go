package ai

import (
	"strings"
	"sync/atomic"
	"testing"
)

// TestRunWithSession verifies the new session-wired entry point drives the
// browser, closes the loop, and persists the conversation into the session
// store (so 省token compaction applies).
func TestRunWithSession(t *testing.T) {
	var calls int32
	be := newMockOpenAI(true, &calls)
	defer be.Close()
	reg := NewRegistry()
	reg.AddProvider(&Provider{Name: "mock", BaseURL: be.URL, Model: "mock", Protocol: ProtoOpenAI})
	rl := NewRelay(reg)

	browser := &mockBrowser{}
	caps := &mockCaps{}
	rl.SetBrowser(browser)
	rl.SetCaptureSource(caps)

	agent := NewAgent(rl, browser, caps)
	out, err := agent.RunWithSession("sess-1", "帮我打开登录页并分析")
	if err != nil {
		t.Fatalf("RunWithSession error: %v", err)
	}
	if !strings.Contains(out, "闭环验证通过") {
		t.Fatalf("agent did not finish loop, got: %s", out)
	}
	if browser.last != "https://example.com/login" {
		t.Fatalf("browser not driven, last=%s", browser.last)
	}
	// the session must now exist in the store (persisted + compacted).
	found := false
	for _, id := range rl.Sessions().List() {
		if id == "sess-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("RunWithSession did not persist session 'sess-1'")
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", calls)
	}
}
