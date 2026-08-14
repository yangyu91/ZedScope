package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bigMsg(role, char string, n int) ChatMessage {
	return ChatMessage{Role: role, Content: strings.Repeat(char, n)}
}

func TestEstimateTokensApprox(t *testing.T) {
	msgs := []ChatMessage{bigMsg("user", "a", 400)}
	got := estimateTokens(msgs)
	// "user"=4 + 400 content = 404 chars -> /4 = 101
	if got != 101 {
		t.Fatalf("estimateTokens = %d, want 101", got)
	}
	// larger content => larger estimate, monotonic
	if estimateTokens([]ChatMessage{bigMsg("user", "a", 800)}) <= got {
		t.Fatal("estimate should grow with content")
	}
}

func TestCompactReducesCount(t *testing.T) {
	s := NewSessionStore()
	s.SetCompactEnabled(true)
	s.SetCompactRatio(0.5)
	s.budgetTokens = 200 // tiny budget to force compaction

	ses := &Session{ID: "t"}
	ses.Messages = []ChatMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "initial task"},
	}
	for i := 0; i < 20; i++ {
		ses.Messages = append(ses.Messages, bigMsg("user", "x", 500))
		ses.Messages = append(ses.Messages, bigMsg("assistant", "y", 500))
	}
	before := len(ses.Messages)
	lastSix := append([]ChatMessage{}, ses.Messages[before-6:]...)

	s.Compact(ses)
	after := len(ses.Messages)
	if after >= before {
		t.Fatalf("compaction did not reduce count: before=%d after=%d", before, after)
	}

	// prefix preserved: system + first user task
	if ses.Messages[0].Role != "system" {
		t.Fatal("system prefix lost")
	}
	if ses.Messages[1].Role != "user" || ses.Messages[1].Content != "initial task" {
		t.Fatal("first user task lost")
	}

	// exactly one summary + the 6-message tail
	if ses.Messages[2].Role != "assistant" || !strings.HasPrefix(ses.Messages[2].Content, "[对话摘要]") {
		t.Fatal("expected a single summary message at index 2")
	}
	if after != 2+1+6 {
		t.Fatalf("expected prefix(2)+summary(1)+tail(6)=9, got %d", after)
	}

	// tail preserved verbatim
	for i := 0; i < 6; i++ {
		if ses.Messages[3+i].Content != lastSix[i].Content {
			t.Fatalf("tail message %d was altered by compaction", i)
		}
	}
}

func TestCompactKeepsTailVerbatim(t *testing.T) {
	s := NewSessionStore()
	s.SetCompactEnabled(true)
	s.budgetTokens = 100
	s.SetCompactRatio(0.5)

	ses := &Session{ID: "t"}
	ses.Messages = []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	for i := 0; i < 12; i++ {
		ses.Messages = append(ses.Messages, bigMsg("user", "u", 300))
		ses.Messages = append(ses.Messages, bigMsg("assistant", "v", 300))
	}
	tail := append([]ChatMessage{}, ses.Messages[len(ses.Messages)-6:]...)
	s.Compact(ses)
	if len(ses.Messages) < 6 {
		t.Fatalf("tail should survive: got %d messages", len(ses.Messages))
	}
	gotTail := ses.Messages[len(ses.Messages)-6:]
	for i := range tail {
		if gotTail[i].Content != tail[i].Content {
			t.Fatalf("tail[%d] altered", i)
		}
	}
}

func TestCompactFallbackNoPanic(t *testing.T) {
	s := NewSessionStore()
	s.SetCompactEnabled(true)
	s.budgetTokens = 50
	s.SetCompactRatio(0.5)
	// summarizer fails -> extractive fallback must still produce a summary
	s.SetSummarizer(func(ctx string) (string, error) {
		return "", errTestSummarize
	})

	ses := &Session{ID: "t"}
	ses.Messages = []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	for i := 0; i < 10; i++ {
		ses.Messages = append(ses.Messages, bigMsg("user", "hello", 200))
		ses.Messages = append(ses.Messages, bigMsg("assistant", "world", 200))
	}
	s.Compact(ses)
	summary := ses.Messages[2].Content
	if !strings.HasPrefix(summary, "[对话摘要] ") {
		t.Fatal("missing summary marker")
	}
	if strings.TrimSpace(strings.TrimPrefix(summary, "[对话摘要] ")) == "" {
		t.Fatal("fallback summary was empty")
	}
}

func TestCompactSuccessPath(t *testing.T) {
	s := NewSessionStore()
	s.SetCompactEnabled(true)
	s.budgetTokens = 50
	s.SetCompactRatio(0.5)
	s.SetSummarizer(func(ctx string) (string, error) {
		return "SUMMARY-OF-MIDDLE", nil
	})

	ses := &Session{ID: "t"}
	ses.Messages = []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	for i := 0; i < 10; i++ {
		ses.Messages = append(ses.Messages, bigMsg("user", "u", 200))
		ses.Messages = append(ses.Messages, bigMsg("assistant", "v", 200))
	}
	s.Compact(ses)
	if !strings.Contains(ses.Messages[2].Content, "SUMMARY-OF-MIDDLE") {
		t.Fatalf("summarizer output not used: %q", ses.Messages[2].Content)
	}
}

func TestCompactDisabledIsNoop(t *testing.T) {
	s := NewSessionStore()
	s.SetCompactEnabled(false) // default off
	ses := &Session{ID: "t"}
	ses.Messages = []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	for i := 0; i < 20; i++ {
		ses.Messages = append(ses.Messages, bigMsg("user", "u", 500))
	}
	before := len(ses.Messages)
	s.Compact(ses)
	if len(ses.Messages) != before {
		t.Fatalf("disabled compaction changed history: %d -> %d", before, len(ses.Messages))
	}
}

// errTestSummarize is a sentinel error for the fallback test.
var errTestSummarize = &summarizeError{}

type summarizeError struct{}

func (e *summarizeError) Error() string { return "summarize failed" }

// ---- 省 token 部: extended coverage ----

// buildChat builds system + first user task + `pairs` alternating user/assistant
// filler messages of `n` chars each, plus any extra messages appended by the caller.
func buildChat(n, pairs int) []ChatMessage {
	msgs := []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	for i := 0; i < pairs; i++ {
		msgs = append(msgs, bigMsg("user", "u", n))
		msgs = append(msgs, bigMsg("assistant", "v", n))
	}
	return msgs
}

func TestExportImportJSONRoundTrip(t *testing.T) {
	ses := &Session{ID: "rt", CreatedAt: 12345}
	ses.Messages = []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		bigMsg("assistant", "a", 50),
	}
	ses.SetCompactRatio(0.7)

	data, err := ses.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var got Session
	if err := got.ImportJSON(data); err != nil {
		t.Fatalf("ImportJSON: %v", err)
	}
	if got.ID != ses.ID || got.CreatedAt != ses.CreatedAt {
		t.Fatalf("id/created mismatch: %q/%d vs %q/%d", got.ID, got.CreatedAt, ses.ID, ses.CreatedAt)
	}
	if got.CompactRatio != 0.7 {
		t.Fatalf("per-session ratio lost: %v", got.CompactRatio)
	}
	if len(got.Messages) != len(ses.Messages) {
		t.Fatalf("message count mismatch: %d vs %d", len(got.Messages), len(ses.Messages))
	}
	for i := range ses.Messages {
		if got.Messages[i].Role != ses.Messages[i].Role || got.Messages[i].Content != ses.Messages[i].Content {
			t.Fatalf("message %d mismatch after round-trip", i)
		}
	}
}

func TestAppendAndLoadJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.jsonl")

	s1 := NewSessionStore()
	s1.SetCompactEnabled(true)
	a := s1.GetOrCreate("a")
	a.Messages = []ChatMessage{{Role: "system", Content: "sa"}, {Role: "user", Content: "ta"}}
	a.SetCompactRatio(0.6)
	b := s1.GetOrCreate("b")
	b.Messages = []ChatMessage{{Role: "system", Content: "sb"}, {Role: "user", Content: "tb"}, bigMsg("assistant", "x", 40)}

	if err := s1.AppendJSONL(path, a); err != nil {
		t.Fatalf("AppendJSONL a: %v", err)
	}
	if err := s1.AppendJSONL(path, b); err != nil {
		t.Fatalf("AppendJSONL b: %v", err)
	}

	// directory traversal of a multi-line file
	if err := s1.AppendJSONL(path, a); err != nil { // duplicate line, LoadJSONL last-wins
		t.Fatalf("AppendJSONL a again: %v", err)
	}

	s2 := NewSessionStore()
	if err := s2.LoadJSONL(path); err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if len(s2.List()) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(s2.List()))
	}
	ra := s2.GetOrCreate("a")
	if len(ra.Messages) != 2 || ra.CompactRatio != 0.6 {
		t.Fatalf("session a not recovered correctly: msgs=%d ratio=%v", len(ra.Messages), ra.CompactRatio)
	}
	rb := s2.GetOrCreate("b")
	if len(rb.Messages) != 3 {
		t.Fatalf("session b not recovered: %d msgs", len(rb.Messages))
	}
}

func TestPerSessionRatioOverride(t *testing.T) {
	s := NewSessionStore()
	s.SetCompactEnabled(true)
	s.SetCompactRatio(0.1) // global trigger at 10% of budget
	s.budgetTokens = 4000 // token trigger threshold for A = 400

	// sesA has no override -> global 0.1 applies (threshold 400 tokens)
	sesA := &Session{ID: "A"}
	sesA.Messages = buildChat(400, 6) // 14 msgs, ~1200 tokens: over 400, under 3800
	s.Compact(sesA)
	if len(sesA.Messages) >= 14 {
		t.Fatalf("global ratio should have compacted A: %d msgs remain", len(sesA.Messages))
	}

	// sesB overrides ratio to 0.95 -> threshold 3800, not reached
	sesB := &Session{ID: "B"}
	sesB.Messages = buildChat(400, 6)
	sesB.SetCompactRatio(0.95)
	before := len(sesB.Messages)
	s.Compact(sesB)
	if len(sesB.Messages) != before {
		t.Fatalf("per-session override should suppress compaction: %d -> %d", before, len(sesB.Messages))
	}
}

func TestMultiConditionToolTrigger(t *testing.T) {
	s := NewSessionStore()
	s.SetCompactEnabled(true)
	s.budgetTokens = 1_000_000 // token trigger effectively off
	s.SetCompactTriggerToolCalls(3)

	ses := &Session{ID: "t"}
	ses.Messages = []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	// 5 tool-calling assistant turns, each with one tool call (need enough
	// messages so the 6-message tail still leaves a middle to summarize)
	for i := 0; i < 5; i++ {
		ses.Messages = append(ses.Messages, ChatMessage{
			Role: "assistant",
			Content: "calling",
			ToolCalls: []ToolCall{{
				Type:     "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "f", Arguments: "{}"},
			}},
		})
		ses.Messages = append(ses.Messages, bigMsg("tool", "r", 20))
	}
	if len(ses.Messages) < 4 {
		t.Fatal("setup produced too few messages")
	}
	s.Compact(ses)
	// tool trigger fired despite tiny token usage
	if ses.Messages[2].Role != "assistant" || !strings.HasPrefix(ses.Messages[2].Content, "[对话摘要]") {
		t.Fatal("tool-call trigger did not compact")
	}
}

func TestMultiConditionMessageTrigger(t *testing.T) {
	s := NewSessionStore()
	s.SetCompactEnabled(true)
	s.budgetTokens = 1_000_000
	s.SetCompactTriggerMessages(8) // fire once we have >= 8 messages

	ses := &Session{ID: "t"}
	ses.Messages = []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	for i := 0; i < 10; i++ { // +10 => 12 messages total
		ses.Messages = append(ses.Messages, bigMsg("user", "u", 10))
	}
	s.Compact(ses)
	if !strings.HasPrefix(ses.Messages[2].Content, "[对话摘要]") {
		t.Fatal("message-count trigger did not compact")
	}

	// below threshold => no compaction
	ses2 := &Session{ID: "t2"}
	ses2.Messages = []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	for i := 0; i < 3; i++ { // 5 messages < 8
		ses2.Messages = append(ses2.Messages, bigMsg("user", "u", 10))
	}
	before := len(ses2.Messages)
	s.Compact(ses2)
	if len(ses2.Messages) != before {
		t.Fatal("message trigger should not fire below threshold")
	}
}

func TestTailKeepOverride(t *testing.T) {
	s := NewSessionStore()
	s.SetCompactEnabled(true)
	s.SetCompactRatio(0.5)
	s.budgetTokens = 200
	s.SetTailKeep(3) // keep exactly 3 recent messages

	ses := &Session{ID: "t"}
	ses.Messages = []ChatMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "initial task"},
	}
	for i := 0; i < 20; i++ {
		ses.Messages = append(ses.Messages, bigMsg("user", "x", 500))
		ses.Messages = append(ses.Messages, bigMsg("assistant", "y", 500))
	}
	s.Compact(ses)
	if len(ses.Messages) != 2+1+3 {
		t.Fatalf("expected prefix(2)+summary(1)+tail(3)=6, got %d", len(ses.Messages))
	}
}

func TestImportanceWeightedTail(t *testing.T) {
	s := NewSessionStore()
	s.SetCompactEnabled(true)
	s.SetCompactRatio(0.5)
	s.budgetTokens = 1000
	s.SetTailKeep(1) // minTail=1 so low-importance tail can be dropped
	s.SetImportanceWeighted(true)

	ses := &Session{ID: "t"}
	ses.Messages = []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	// 10 expensive low-importance tool messages
	for i := 0; i < 10; i++ {
		ses.Messages = append(ses.Messages, bigMsg("tool", "r", 400))
	}
	// final high-importance user message (must survive)
	ses.Messages = append(ses.Messages, bigMsg("user", "f", 400))

	before := len(ses.Messages)
	s.Compact(ses)
	if len(ses.Messages) >= before {
		t.Fatalf("weighted compaction did not reduce: %d", len(ses.Messages))
	}
	if ses.Messages[0].Role != "system" || ses.Messages[1].Role != "user" {
		t.Fatal("prefix lost under weighted mode")
	}
	if !strings.HasPrefix(ses.Messages[2].Content, "[对话摘要]") {
		t.Fatal("missing summary under weighted mode")
	}
	// the high-importance final user message must be in the tail
	last := ses.Messages[len(ses.Messages)-1]
	if last.Role != "user" || last.Content != strings.Repeat("f", 400) {
		t.Fatalf("important tail message dropped: %q", last.Content[:20])
	}
	// tool messages (low importance) should have been summarized away, not kept verbatim
	for _, m := range ses.Messages {
		if m.Role == "tool" {
			t.Fatal("low-importance tool message leaked into weighted tail")
		}
	}
}

func TestSummaryPromptInjected(t *testing.T) {
	s := NewSessionStore()
	s.SetCompactEnabled(true)
	s.budgetTokens = 50
	s.SetCompactRatio(0.5)
	s.SetSummaryPrompt("CUSTOM-PROMPT-MARKER")
	s.SetSummarizer(func(ctx string) (string, error) {
		return "ECHO:" + ctx, nil
	})

	ses := &Session{ID: "t"}
	ses.Messages = []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
	}
	for i := 0; i < 10; i++ {
		ses.Messages = append(ses.Messages, bigMsg("user", "u", 200))
		ses.Messages = append(ses.Messages, bigMsg("assistant", "v", 200))
	}
	s.Compact(ses)
	if !strings.Contains(ses.Messages[2].Content, "CUSTOM-PROMPT-MARKER") {
		t.Fatalf("injected prompt not forwarded to summarizer: %q", ses.Messages[2].Content)
	}
}

func TestCompactBoundaryShortSession(t *testing.T) {
	s := NewSessionStore()
	s.SetCompactEnabled(true)
	s.SetCompactRatio(0.1)
	s.budgetTokens = 10 // would trigger on any content

	ses := &Session{ID: "t"}
	ses.Messages = []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		bigMsg("assistant", "a", 500), // only 3 messages total
	}
	before := len(ses.Messages)
	s.Compact(ses)
	if len(ses.Messages) != before {
		t.Fatal("session with <4 messages must not be compacted")
	}
}

func TestRoleWeightsMergedWithDefaults(t *testing.T) {
	s := NewSessionStore()
	s.SetRoleWeights(map[string]float64{"user": 2.0})
	w := s.RoleWeights()
	if w["user"] != 2.0 {
		t.Fatalf("user weight override lost: %v", w["user"])
	}
	if w["assistant"] != defaultRoleWeights["assistant"] {
		t.Fatal("default assistant weight not preserved")
	}
	s.SetRoleWeights(nil) // reset
	if s.RoleWeights()["user"] != defaultRoleWeights["user"] {
		t.Fatal("reset to defaults failed")
	}
}

func TestLoadJSONLBadLinesSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("{not valid json}\n")
	f.WriteString("\n") // blank line
	f.WriteString("{\"id\":\"x\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}\n")
	f.Close()

	s := NewSessionStore()
	if err := s.LoadJSONL(path); err != nil {
		t.Fatalf("LoadJSONL errored: %v", err)
	}
	if len(s.List()) != 1 {
		t.Fatalf("expected 1 recovered session, got %d", len(s.List()))
	}
}
