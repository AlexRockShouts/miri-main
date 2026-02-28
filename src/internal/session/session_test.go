package session

import (
	"sync"
	"testing"
)

func TestSessionManager_GetOrCreate(t *testing.T) {
	sm := NewSessionManager()
	id := "test-session"
	s1 := sm.GetOrCreate(id)
	if s1.ID != id {
		t.Errorf("expected ID %s, got %s", id, s1.ID)
	}
	s2 := sm.GetOrCreate(id)
	if s1 != s2 {
		t.Error("expected same session")
	}
	emptyID := ""
	s3 := sm.GetOrCreate(emptyID)
	if s3.ID != DefaultSessionID {
		t.Errorf("expected %s ID, got %s", DefaultSessionID, s3.ID)
	}
}

func TestSessionManager_AddTokens(t *testing.T) {
	sm := NewSessionManager()
	id := "test-session"
	sm.AddTokens(id, 60, 40, 0.001)
	sess := sm.GetOrCreate(id)
	if sess.TotalTokens != 100 {
		t.Errorf("expected 100 tokens, got %d", sess.TotalTokens)
	}
	if sess.TotalCost != 0.001 {
		t.Errorf("expected 0.001 cost, got %f", sess.TotalCost)
	}
}

func TestConcurrentAddTokens(t *testing.T) {
	sm := NewSessionManager()
	id := "test-session"
	var wg sync.WaitGroup
	const N = 100
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			sm.AddTokens(id, 1, 0, 0)
		}()
	}
	wg.Wait()
	sess := sm.GetOrCreate(id)
	if sess.TotalTokens != N {
		t.Errorf("expected %d tokens, got %d", N, sess.TotalTokens)
	}
}
