package proxy

import (
	"net/http"
	"sync"
	"time"
)

// Token is a single extracted credential/secret found in a request or response.
type Token struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Source      string `json:"source"` // header | cookie | body | url
	URL         string `json:"url"`
	IsLogin     bool   `json:"is_login"`
	CapturedAt  int64  `json:"captured_at"`
}

// Record is one captured HTTP transaction.
type Record struct {
	ID          int64     `json:"id"`
	Method      string    `json:"method"`
	URL         string    `json:"url"`
	Host        string    `json:"host"`
	ReqHeaders  string    `json:"req_headers"`
	ReqBody     string    `json:"req_body"`
	StatusCode  int       `json:"status_code"`
	RespHeaders string    `json:"resp_headers"`
	RespBody    string    `json:"resp_body"`     // in-memory preview (<= 8 MiB); "" if spilled to disk
	RespBodyFile string   `json:"resp_body_file,omitempty"` // path when the body was spilled to disk
	RespBodySize int64    `json:"resp_body_size"`           // total bytes that streamed through
	ReqBodyFile  string   `json:"req_body_file,omitempty"`
	IsHTTPS     bool      `json:"is_https"`
	IsLogin     bool      `json:"is_login"`
	Time        time.Time `json:"time"`
	Tokens      []Token   `json:"tokens"`
}

// Store is a bounded, in-memory ring buffer of captured Records.
type Store struct {
	mu    sync.Mutex
	items []*Record
	seq   int64
	max   int
}

// NewStore creates a store keeping at most max records.
func NewStore(max int) *Store {
	if max <= 0 {
		max = 2000
	}
	return &Store{max: max}
}

// Add appends a record and returns its assigned id.
func (s *Store) Add(r *Record) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	r.ID = s.seq
	s.items = append(s.items, r)
	if len(s.items) > s.max {
		s.items = s.items[len(s.items)-s.max:]
	}
	return r.ID
}

// List returns a snapshot (oldest -> newest).
func (s *Store) List() []*Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Record, len(s.items))
	copy(out, s.items)
	return out
}

// Clear empties the store.
func (s *Store) Clear() {
	s.mu.Lock()
	s.items = nil
	s.seq = 0
	s.mu.Unlock()
}

// headerDump renders an http.Header in raw "Key: Value\r\n" form.
func headerDump(h http.Header) string {
	out := ""
	for k, vs := range h {
		for _, v := range vs {
			out += k + ": " + v + "\r\n"
		}
	}
	return out
}
