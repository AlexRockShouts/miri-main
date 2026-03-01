package session

import (
	"sync"
)

const DefaultSessionID = "miri:main:agent"

type Message struct {
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
}

type Session struct {
	ID           string  `json:"id"`
	Soul         string  `json:"soul,omitempty"`
	TotalTokens  uint64  `json:"total_tokens"`
	PromptTokens uint64  `json:"prompt_tokens"`
	OutputTokens uint64  `json:"output_tokens"`
	TotalCost    float64 `json:"total_cost"`
	mu           sync.RWMutex
}

func NewSession(id string) *Session {
	return &Session{
		ID: id,
	}
}

func (s *Session) GetSoul() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Soul
}

func (s *Session) AddTokens(prompt, output uint64, cost float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PromptTokens += prompt
	s.OutputTokens += output
	s.TotalTokens += prompt + output
	s.TotalCost += cost
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

func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalTokens = 0
	s.PromptTokens = 0
	s.OutputTokens = 0
	s.TotalCost = 0
}

func (sm *SessionManager) GetOrCreate(id string) *Session {
	if id == "" {
		id = DefaultSessionID
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sess, ok := sm.sessions[id]; ok {
		return sess
	}
	sess := NewSession(id)
	sm.sessions[id] = sess
	return sess
}

func (sm *SessionManager) AddTokens(id string, prompt, output uint64, cost float64) {
	sess := sm.GetOrCreate(id)
	sess.AddTokens(prompt, output, cost)
}

func (sm *SessionManager) CreateNewSession() string {
	return DefaultSessionID
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
	if id == "" {
		id = DefaultSessionID
	}
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if sess, ok := sm.sessions[id]; ok {
		return sess
	}
	return nil
}
