package agent

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type SubSession struct {
	ID           string
	Name         string
	CreatedAt    time.Time
	LastMessage  string
	MessageCount int
}

type SubSessionStore struct {
	mu       sync.RWMutex
	seq      uint64
	sessions map[string]*SubSession
}

func NewSubSessionStore() *SubSessionStore {
	return &SubSessionStore{
		sessions: make(map[string]*SubSession),
	}
}

func (s *SubSessionStore) Spawn(name string) SubSession {
	if s == nil {
		return SubSession{}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "worker"
	}

	id := fmt.Sprintf("sub-%06d", atomic.AddUint64(&s.seq, 1))
	item := &SubSession{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}

	s.mu.Lock()
	s.sessions[id] = item
	s.mu.Unlock()
	return *item
}

func (s *SubSessionStore) Send(sessionID, message string) (SubSession, error) {
	if s == nil {
		return SubSession{}, fmt.Errorf("sub session store is not initialized")
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SubSession{}, fmt.Errorf("session_id is required")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return SubSession{}, fmt.Errorf("message is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.sessions[sessionID]
	if !ok {
		return SubSession{}, fmt.Errorf("session not found: %s", sessionID)
	}
	item.MessageCount++
	item.LastMessage = message
	return *item, nil
}
