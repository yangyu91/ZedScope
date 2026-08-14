package ai

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// ExportJSON serialises the session (id, messages, created-at and any
// per-session compact_ratio) to JSON. Pure standard library.
func (ses *Session) ExportJSON() ([]byte, error) {
	return json.Marshal(ses)
}

// ImportJSON populates the receiver from previously exported JSON. It overwrites
// id, messages, created-at and the per-session compact_ratio.
func (ses *Session) ImportJSON(data []byte) error {
	var tmp Session
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	ses.ID = tmp.ID
	ses.Messages = tmp.Messages
	ses.CreatedAt = tmp.CreatedAt
	ses.CompactRatio = tmp.CompactRatio
	return nil
}

// AppendJSONL appends one JSON line for ses to the append-only log at path.
// Best-effort: it only returns hard IO errors (open/write). Each line is a
// complete session snapshot, so the file can later be reloaded to recover
// state and re-project cache-friendly prefixes.
func (s *SessionStore) AppendJSONL(path string, ses *Session) error {
	data, err := json.Marshal(ses)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// LoadJSONL reads an append-only JSONL log and re-inserts every parsed session
// into the store (keyed by session id, last write wins). Malformed lines are
// skipped best-effort so a single bad record cannot abort recovery.
func (s *SessionStore) LoadJSONL(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ses Session
		if err := json.Unmarshal([]byte(line), &ses); err != nil {
			continue // skip corrupt line
		}
		if ses.ID == "" {
			continue
		}
		s.mu.Lock()
		s.sessions[ses.ID] = &ses
		s.mu.Unlock()
	}
	return sc.Err()
}
