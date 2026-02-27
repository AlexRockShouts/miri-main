package session

import (
	"encoding/json"
	"fmt"
	"miri-main/src/internal/storage"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Message struct {
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
}

type Session struct {
	ID          string    `json:"id"`
	Soul        string    `json:"soul,omitempty"`
	Messages    []Message `json:"messages"`
	TotalTokens uint64    `json:"total_tokens"`
	mu          sync.RWMutex
}

type ArchivedSession struct {
	ID          string    `json:"id"`
	Soul        string    `json:"soul,omitempty"`
	Messages    []Message `json:"messages"`
	TotalTokens uint64    `json:"total_tokens"`
}

func (s *Session) toArchive() ArchivedSession {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return ArchivedSession{
		ID:          s.ID,
		Soul:        s.Soul,
		Messages:    append([]Message(nil), s.Messages...),
		TotalTokens: s.TotalTokens,
	}
}

func NewSession(id string) *Session {
	return &Session{
		ID: id,
	}
}

func (s *Session) SetSoulIfEmpty(st *storage.Storage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Soul != "" {
		return nil
	}
	soul, err := st.GetSoul()
	if err != nil {
		return err
	}
	s.Soul = soul
	return nil
}

func (s *Session) GetSoul() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Soul
}

func (s *Session) AddTokens(tokens uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalTokens += tokens
}

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

func (s *Session) ClearMessages() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = nil
	s.TotalTokens = 0
}

func (sm *SessionManager) GetOrCreate(id string) *Session {
	id = "default"
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sess, ok := sm.sessions[id]; ok {
		return sess
	}
	sess := NewSession(id)
	sm.sessions[id] = sess
	return sess
}

func (sm *SessionManager) AddMessage(id string, prompt, response string) {
	id = "default"
	sess := sm.GetOrCreate(id)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.Messages = append(sess.Messages, Message{
		Prompt:   prompt,
		Response: response,
	})
}

func (sm *SessionManager) CreateNewSession() string {
	return "default"
}

func (sm *SessionManager) ListIDs() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	ids := make([]string, 0, len(sm.sessions))
	for id := range sm.sessions {
		ids = append(ids, id)
	}
	return ids
}

func (sm *SessionManager) GetSession(id string) *Session {
	id = "default"
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if sess, ok := sm.sessions[id]; ok {
		return sess
	}
	return nil
}

func (sm *SessionManager) ArchiveSession(sess *Session) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("user home dir: %w", err)
	}

	memDir := filepath.Join(home, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		return fmt.Errorf("mkdir ~/memory: %w", err)
	}

	now := time.Now().UTC()
	ts := now.Format("2006-01-02-15-04-05Z")
	fn := fmt.Sprintf("memory.session-%s.%s.json", sess.ID, ts)
	path := filepath.Join(memDir, fn)

	arch := sess.toArchive()
	data, err := json.MarshalIndent(arch, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write file %s: %w", path, err)
	}

	return nil
}
