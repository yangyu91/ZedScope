package ai

import "sync"

// DeepseekWebSession holds the server-side conversation identity for the free
// DeepSeek web bridge, enabling multi-turn chat without resending tokens:
// DeepSeek keeps context keyed by chat_session_id + parent_message_id (mirrors
// deepseek-pp's reuse of the logged-in web session). Key is the yami session
// id (ChatCompletionRequest.SessionID); an empty Key means a one-shot,
// anonymous turn.
type DeepseekWebSession struct {
	Key              string // yami session id
	ChatSessionID    string // server chat_session_id (from /chat_session/create)
	ParentMessageID  string // last assistant/request message id (numeric string)
	RequestMessageID string
	Model            string // "deepseek-chat" | "deepseek-reasoner"
	Reasoning        string // accumulated thinking chain of the last turn
	PowHeader        string // cached x-ds-pow-response value
	PowExpireAt      int64  // unix seconds when PowHeader expires
}

// DeepseekWebSessionStore maps yami session keys to DeepSeek web sessions.
type DeepseekWebSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*DeepseekWebSession
}

// NewDeepseekWebSessionStore returns an empty store.
func NewDeepseekWebSessionStore() *DeepseekWebSessionStore {
	return &DeepseekWebSessionStore{sessions: map[string]*DeepseekWebSession{}}
}

// DefaultDeepseekWebSessions is the process-wide store used by the bridge.
var DefaultDeepseekWebSessions = NewDeepseekWebSessionStore()

// GetDeepseekWebSessionStore returns the default store so 总办 can inspect or
// clear DeepSeek web sessions (e.g. to surface the last reasoning chain).
func GetDeepseekWebSessionStore() *DeepseekWebSessionStore { return DefaultDeepseekWebSessions }

// GetOrCreate returns the existing session for key, or a fresh one. An empty
// key yields an ephemeral session that is never persisted.
func (s *DeepseekWebSessionStore) GetOrCreate(key string) *DeepseekWebSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == "" {
		return &DeepseekWebSession{Key: ""}
	}
	if ses, ok := s.sessions[key]; ok {
		return ses
	}
	ses := &DeepseekWebSession{Key: key}
	s.sessions[key] = ses
	return ses
}

// Get returns the session for key.
func (s *DeepseekWebSessionStore) Get(key string) (*DeepseekWebSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ses, ok := s.sessions[key]
	return ses, ok
}

// Put persists a session (no-op for ephemeral sessions).
func (s *DeepseekWebSessionStore) Put(ses *DeepseekWebSession) {
	if ses == nil || ses.Key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[ses.Key] = ses
}

// Clear drops a session by key.
func (s *DeepseekWebSessionStore) Clear(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key)
}

// List returns all persisted session keys.
func (s *DeepseekWebSessionStore) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.sessions))
	for k := range s.sessions {
		out = append(out, k)
	}
	return out
}
