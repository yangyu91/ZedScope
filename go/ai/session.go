package ai

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Session holds one conversation's full message history.
//
// CompactRatio is an optional per-session override of the store's global
// compact_ratio. A value of 0 means "use the store default"; any value in
// (0,1] overrides it. This is the per-session ratio coverage required by the
// 省 token 部 spec.
type Session struct {
	ID        string        `json:"id"`
	Messages  []ChatMessage `json:"messages"`
	CreatedAt int64         `json:"created_at"`

	CompactRatio float64 `json:"compact_ratio,omitempty"`
}

// SessionStore keeps conversations and applies Reasonix-style context
// compaction: a single compact_ratio drives a content-based maintenance that
// keeps a stable prefix (system + first user task), collapses the middle into
// one rolling summary, and preserves a recent tail. This is the "省 token
// 模式" — when the active provider is a paid key, compaction is on by default
// so long multi-turn chats stay small.
//
// All new tuning knobs (tailKeep, extra triggers, importance weighting, role
// weights, summary prompt) default to values that reproduce the original
// behaviour, so callers that never touch them see no change.
type SessionStore struct {
	mu             sync.Mutex
	sessions       map[string]*Session
	compactEnabled bool
	compactRatio   float64 // 0..1; trigger = budget * ratio
	budgetTokens   int
	summarizer     func(ctx string) (string, error) // nil => extractive fallback

	// --- tunables (default = legacy behaviour) ---

	// summaryPrompt is prepended to the context handed to the summarizer.
	// Empty by default, which keeps the extractive fallback semantics.
	summaryPrompt string
	// tailKeep is the baseline number of recent messages preserved verbatim.
	tailKeep int
	// triggerToolCalls forces compaction once cumulative tool calls reach it
	// (0 = disabled).
	triggerToolCalls int
	// triggerMessages forces compaction once total message count reaches it
	// (0 = disabled).
	triggerMessages int
	// importanceWeighted switches the tail from a fixed count to a
	// role/importance-aware token budget (see roleWeights).
	importanceWeighted bool
	// roleWeights gives each role an importance score used to compute the
	// weighted tail. nil => defaults (defaultRoleWeights).
	roleWeights map[string]float64
}

// defaultTailKeep reproduces the original tail length.
const defaultTailKeep = 6

// defaultRoleWeights are used when roleWeights is nil.
var defaultRoleWeights = map[string]float64{
	"system":    0.2,
	"user":      1.0,
	"assistant": 0.7,
	"tool":      0.4,
}

// NewSessionStore returns an empty store with sensible defaults.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions:       make(map[string]*Session),
		compactRatio:   0.85,
		budgetTokens:   60000,
		compactEnabled: false,
		tailKeep:       defaultTailKeep,
		roleWeights:    map[string]float64{},
	}
}

// ---- configuration ----

func (s *SessionStore) SetCompactEnabled(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compactEnabled = on
}

func (s *SessionStore) IsCompactEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compactEnabled
}

// CompactRatio returns the current global compact_ratio.
func (s *SessionStore) CompactRatio() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compactRatio
}

// SetCompactRatio sets the global compact_ratio in (0,1]. Lower = compact earlier/more.
func (s *SessionStore) SetCompactRatio(r float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r > 0 && r <= 1 {
		s.compactRatio = r
	}
}

// SetSummarizer installs a model-backed summarizer (e.g. the active provider).
// When nil, compaction falls back to an extractive summary.
func (s *SessionStore) SetSummarizer(fn func(ctx string) (string, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summarizer = fn
}

// SetSummaryPrompt installs an external prompt prepended to the summarizer
// context. Empty restores the default (no injected prompt). The extractive
// fallback ignores the prompt and never panics.
func (s *SessionStore) SetSummaryPrompt(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summaryPrompt = p
}

// SummaryPrompt returns the currently configured summary prompt.
func (s *SessionStore) SummaryPrompt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.summaryPrompt
}

// SetTailKeep overrides the baseline verbatim tail length (default 6).
func (s *SessionStore) SetTailKeep(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n >= 0 {
		s.tailKeep = n
	}
}

// TailKeep returns the baseline verbatim tail length.
func (s *SessionStore) TailKeep() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tailKeep
}

// SetCompactTriggerToolCalls forces compaction when cumulative tool calls reach
// n (0 disables this extra trigger). Independent of the token trigger.
func (s *SessionStore) SetCompactTriggerToolCalls(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.triggerToolCalls = n
}

// CompactTriggerToolCalls returns the tool-call trigger threshold.
func (s *SessionStore) CompactTriggerToolCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.triggerToolCalls
}

// SetCompactTriggerMessages forces compaction when total message count reaches
// n (0 disables this extra trigger). Independent of the token trigger.
func (s *SessionStore) SetCompactTriggerMessages(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.triggerMessages = n
}

// CompactTriggerMessages returns the message-count trigger threshold.
func (s *SessionStore) CompactTriggerMessages() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.triggerMessages
}

// SetImportanceWeighted toggles role/importance-aware tail selection.
func (s *SessionStore) SetImportanceWeighted(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.importanceWeighted = on
}

// IsImportanceWeighted reports whether weighted tail selection is on.
func (s *SessionStore) IsImportanceWeighted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.importanceWeighted
}

// SetRoleWeights merges role => importance scores into the active weights.
// Unknown roles are ignored; pass nil to reset to defaults. A higher score
// makes a role's messages cheaper to keep in the weighted tail.
func (s *SessionStore) SetRoleWeights(w map[string]float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w == nil {
		s.roleWeights = map[string]float64{}
		return
	}
	if s.roleWeights == nil {
		s.roleWeights = map[string]float64{}
	}
	for k, v := range w {
		s.roleWeights[k] = v
	}
}

// RoleWeights returns a copy of the active role weights (defaults applied).
func (s *SessionStore) RoleWeights() map[string]float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]float64{}
	for k, v := range defaultRoleWeights {
		out[k] = v
	}
	for k, v := range s.roleWeights {
		out[k] = v
	}
	return out
}

// ---- per-session ratio override ----

// SetCompactRatio sets a per-session compact_ratio override in (0,1].
// 0 restores "use store default".
func (ses *Session) SetCompactRatio(r float64) {
	if r > 0 && r <= 1 {
		ses.CompactRatio = r
	} else if r == 0 {
		ses.CompactRatio = 0
	}
}

// ---- session lifecycle ----

// GetOrCreate returns the session for id, creating it on first use.
func (s *SessionStore) GetOrCreate(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		id = fmt.Sprintf("s%x", time.Now().UnixNano())
	}
	ses, ok := s.sessions[id]
	if !ok {
		ses = &Session{ID: id, CreatedAt: time.Now().Unix()}
		s.sessions[id] = ses
	}
	return ses
}

// Clear drops a session (or all when id == "").
func (s *SessionStore) Clear(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		s.sessions = make(map[string]*Session)
		return
	}
	delete(s.sessions, id)
}

// List returns all session ids.
func (s *SessionStore) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.sessions))
	for k := range s.sessions {
		out = append(out, k)
	}
	return out
}

// ---- compaction ----

// estimateTokens approximates token count as total characters / 4. Good enough
// for budgeting without a tokenizer dependency.
func estimateTokens(msgs []ChatMessage) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Role) + len(m.Content) + len(m.Name)
		for _, t := range m.ToolCalls {
			n += len(t.Function.Name) + len(t.Function.Arguments)
		}
	}
	if n < 0 {
		return 0
	}
	return n / 4
}

// countToolCalls counts cumulative tool calls across the messages.
func countToolCalls(msgs []ChatMessage) int {
	n := 0
	for _, m := range msgs {
		n += len(m.ToolCalls)
	}
	return n
}

// roleImportance returns the importance score for a message role, applying the
// store's weights (with defaults) and a bonus for tool-calling turns.
func (s *SessionStore) roleImportance(role string, hasToolCalls bool) float64 {
	w := s.roleWeights
	if w == nil {
		w = map[string]float64{}
	}
	score, ok := w[role]
	if !ok {
		score, ok = defaultRoleWeights[role]
		if !ok {
			score = 0.5
		}
	}
	if hasToolCalls {
		score += 0.5
	}
	if score < 0.1 {
		score = 0.1
	}
	return score
}

// effectiveCost returns an importance-weighted token cost: high-importance
// messages cost less, so they are preferentially retained in the tail.
func (s *SessionStore) effectiveCost(m ChatMessage) int {
	raw := estimateTokens([]ChatMessage{m})
	if raw <= 0 {
		return 0
	}
	imp := s.roleImportance(m.Role, len(m.ToolCalls) > 0)
	// cost is inverse to importance; floor at 1 to keep progress deterministic
	c := float64(raw) / imp
	if c < 1 {
		c = 1
	}
	return int(c)
}

// Compact shrinks ses.Messages in place: stable prefix + one summary + tail.
// It is a no-op when compaction is disabled or no trigger is met. Triggers are
// the token budget (budget * ratio) plus optional tool-call / message-count
// thresholds. The resolved ratio honours a per-session override.
func (s *SessionStore) Compact(ses *Session) {
	s.mu.Lock()
	enabled := s.compactEnabled
	ratio := s.compactRatio
	budget := s.budgetTokens
	sum := s.summarizer
	prompt := s.summaryPrompt
	tailKeep := s.tailKeep
	trigTC := s.triggerToolCalls
	trigMsg := s.triggerMessages
	weighted := s.importanceWeighted
	s.mu.Unlock()

	if !enabled || len(ses.Messages) < 4 {
		return
	}
	// per-session ratio override
	if ses.CompactRatio > 0 {
		ratio = ses.CompactRatio
	}

	// multi-condition trigger
	tokenTrigger := estimateTokens(ses.Messages) >= int(float64(budget)*ratio)
	toolTrigger := trigTC > 0 && countToolCalls(ses.Messages) >= trigTC
	msgTrigger := trigMsg > 0 && len(ses.Messages) >= trigMsg
	if !tokenTrigger && !toolTrigger && !msgTrigger {
		return
	}

	msgs := ses.Messages
	// stable prefix start: first "system" message
	sysIdx := -1
	for i, m := range msgs {
		if m.Role == "system" {
			sysIdx = i
			break
		}
	}
	if sysIdx < 0 {
		sysIdx = 0
	}
	// first user task after the system prompt
	firstUser := -1
	for i := sysIdx; i < len(msgs); i++ {
		if msgs[i].Role == "user" {
			firstUser = i
			break
		}
	}
	if firstUser < 0 {
		return
	}

	// determine tail start: fixed count by default, role/importance-aware budget
	// when weighting is enabled.
	tailStart := len(msgs) - tailKeep
	if tailStart < firstUser+1 {
		tailStart = firstUser + 1
	}
	if weighted {
		tailStart = s.weightedTailStart(msgs, firstUser, budget, tailKeep)
	}
	if tailStart <= firstUser+1 {
		// not enough middle to bother compacting
		return
	}

	prefix := append([]ChatMessage{}, msgs[:firstUser+1]...) // system + first user task
	middle := msgs[firstUser+1 : tailStart]                 // to be summarized
	tail := append([]ChatMessage{}, msgs[tailStart:]...)    // recent

	summaryText := summarize(middle, sum, prompt)
	summary := ChatMessage{Role: "assistant", Content: "[对话摘要] " + summaryText}

	ses.Messages = append(append(prefix, summary), tail...)
}

// weightedTailStart walks backwards from the end, accumulating an
// importance-weighted cost until a tail token budget is exceeded (but always
// keeping at least tailKeep messages, and never more than tailKeep*4). It then
// snaps to a role boundary so a coherent turn is not split.
func (s *SessionStore) weightedTailStart(msgs []ChatMessage, firstUser, budget, tailKeep int) int {
	tailBudget := int(float64(budget) * 0.15)
	if tailBudget < 1 {
		tailBudget = 1
	}
	minTail := tailKeep
	if minTail < 1 {
		minTail = 1
	}
	maxTail := tailKeep * 4

	idx := len(msgs) - 1
	acc := 0
	for idx > firstUser && (len(msgs)-idx) < maxTail {
		kept := len(msgs) - idx // messages currently in tail
		cost := s.effectiveCost(msgs[idx])
		if kept >= minTail && acc+cost > tailBudget {
			break
		}
		acc += cost
		idx--
	}
	tailStart := idx + 1
	if tailStart < firstUser+1 {
		tailStart = firstUser + 1
	}
	// snap backwards to keep a same-role run whole (semantic boundary)
	for tailStart > firstUser+1 && tailStart < len(msgs) && msgs[tailStart-1].Role == msgs[tailStart].Role {
		tailStart--
	}
	return tailStart
}

// summarize renders the middle into a single compact string. It prefers the
// model-backed summarizer (with the injected prompt); on failure it falls back
// to an extractive snippet. It never panics.
func summarize(middle []ChatMessage, sum func(string) (string, error), prompt string) string {
	if len(middle) == 0 {
		return ""
	}
	var b strings.Builder
	if strings.TrimSpace(prompt) != "" {
		b.WriteString(strings.TrimSpace(prompt))
		b.WriteString("\n\n")
	}
	for _, m := range middle {
		b.WriteString(fmt.Sprintf("[%s] %s\n", m.Role, truncate(m.Content, 600)))
		for _, t := range m.ToolCalls {
			b.WriteString(fmt.Sprintf("  tool:%s(%s)\n", t.Function.Name, truncate(t.Function.Arguments, 300)))
		}
	}
	ctx := b.String()
	if sum != nil {
		if out, err := sum(ctx); err == nil && strings.TrimSpace(out) != "" {
			return out
		}
	}
	// extractive fallback: first 200 chars of each middle message
	var fb strings.Builder
	for _, m := range middle {
		c := m.Content
		if len(c) > 200 {
			c = c[:200]
		}
		if c != "" {
			fb.WriteString(c + " … ")
		}
	}
	return strings.TrimSpace(fb.String())
}
