// Package ai — SSH session type.
//
// Adds "ssh" as a first-class session type alongside the existing AI chat
// session: the user (or the agent) can open an SSH connection to a remote
// host and run shell commands, with combined output streamed back.
//
// This is the only non-stdlib feature in the core: it is backed by
// golang.org/x/crypto/ssh. Everything else in yami-UA remains standard
// library only; x/crypto is pulled in solely for this session type.
package ai

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// SshAuth describes how to dial + authenticate an SSH session.
type SshAuth struct {
	Host     string `json:"host"`      // host:port, e.g. "10.0.0.5:22"
	User     string `json:"user"`      // login user
	AuthType string `json:"auth_type"` // "password" | "key"
	Secret   string `json:"secret"`    // password, or PEM private key text
}

// SshSession is one live SSH connection.
type SshSession struct {
	ID     string
	Host   string
	User   string
	client *ssh.Client
	mu     sync.Mutex
	last   string
}

// SshManager pools SSH sessions by id.
type SshManager struct {
	mu       sync.Mutex
	sessions map[string]*SshSession
}

// NewSshManager creates an empty pool.
func NewSshManager() *SshManager {
	return &SshManager{sessions: map[string]*SshSession{}}
}

// Connect opens a new SSH session. Returns the session id, or an error whose
// message is prefixed with "err:".
func (m *SshManager) Connect(a SshAuth) (*SshSession, error) {
	var methods []ssh.AuthMethod
	switch a.AuthType {
	case "key":
		signer, err := ssh.ParsePrivateKey([]byte(a.Secret))
		if err != nil {
			return nil, fmt.Errorf("err: bad private key: %v", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	default: // "password" and anything else
		if a.Secret == "" {
			return nil, fmt.Errorf("err: empty password")
		}
		methods = append(methods, ssh.Password(a.Secret))
	}
	cfg := &ssh.ClientConfig{
		User:            a.User,
		Auth:            methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: pin known_hosts
		Timeout:         10 * time.Second,
	}
	client, err := ssh.Dial("tcp", a.Host, cfg)
	if err != nil {
		return nil, fmt.Errorf("err: dial %s: %v", a.Host, err)
	}
	id := fmt.Sprintf("ssh-%d", time.Now().UnixNano())
	s := &SshSession{ID: id, Host: a.Host, User: a.User, client: client}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	return s, nil
}

// Exec runs a command on the session and returns combined stdout+stderr.
func (s *SshSession) Exec(cmd string) (string, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("err: new session: %v", err)
	}
	defer sess.Close()
	var out bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &out
	runErr := sess.Run(cmd)
	s.mu.Lock()
	s.last = out.String()
	s.mu.Unlock()
	if runErr != nil {
		// Surface the captured output even on a non-zero exit.
		return out.String(), fmt.Errorf("err: %v", runErr)
	}
	return out.String(), nil
}

// Get returns a session by id.
func (m *SshManager) Get(id string) (*SshSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// List returns all active session ids.
func (m *SshManager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

// Close terminates a session and its underlying TCP connection.
func (m *SshManager) Close(id string) bool {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok {
		_ = s.client.Close()
	}
	return ok
}
