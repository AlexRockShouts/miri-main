package session

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestSessionManager_GetOrCreate(t *testing.T) {
	sm := NewSessionManager()
	id := uuid.New().String()
	s1 := sm.GetOrCreate(id)
	if s1.ID != "default" {
		t.Errorf("expected ID default, got %s", s1.ID)
	}
	s2 := sm.GetOrCreate(id)
	if s1 != s2 {
		t.Error("expected same session")
	}
	emptyID := ""
	s3 := sm.GetOrCreate(emptyID)
	if s3.ID != "default" {
		t.Error("expected default ID")
	}
}

func TestSessionManager_AddMessage(t *testing.T) {
	sm := NewSessionManager()
	id := uuid.New().String()
	sm.AddMessage(id, "prompt1", "response1")
	sess := sm.GetOrCreate(id)
	if len(sess.Messages) != 1 || sess.Messages[0].Prompt != "prompt1" || sess.Messages[0].Response != "response1" {
		t.Errorf("expected message, got %v", sess.Messages)
	}
}

func TestConcurrentAdd(t *testing.T) {
	sm := NewSessionManager()
	id := uuid.New().String()
	var wg sync.WaitGroup
	const N = 100
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			defer wg.Done()
			sm.AddMessage(id, fmt.Sprintf("p%d", idx), fmt.Sprintf("r%d", idx))
		}(i)
	}
	wg.Wait()
	sess := sm.GetOrCreate(id)
	if len(sess.Messages) != N {
		t.Errorf("expected %d messages, got %d", N, len(sess.Messages))
	}
}
