package session

import (
	"miri-main/src/internal/storage"
	"sync"

	"github.com/google/uuid"
)

type Message struct {
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
}

type Session struct {
	ID       string    `json:"id"`
	Soul     string    `json:"soul,omitempty"`
	ClientID string    `json:"client_id,omitempty"`
	Messages []Message `json:"messages"`
	mu       sync.RWMutex
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

type SessionManager struct {
	sessions       map[string]*Session
	clientSessions map[string]string
	mu             sync.RWMutex
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions:       make(map[string]*Session),
		clientSessions: make(map[string]string),
	}
}

func (sm *SessionManager) GetOrCreate(id string) *Session {
	if id == "" {
		id = uuid.New().String()
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

func (sm *SessionManager) AddMessage(id string, prompt, response string) {
	sess := sm.GetOrCreate(id)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.Messages = append(sess.Messages, Message{
		Prompt:   prompt,
		Response: response,
	})
}

func (sm *SessionManager) CreateNewSession(clientID string) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	id := uuid.New().String()
	sm.sessions[id] = NewSession(id)
	sm.sessions[id].ClientID = clientID
	sm.clientSessions[clientID] = id

	return id
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
