package session

import (
	"fmt"
	"sync"
)

// Manager manages the lifecycle of all terminal sessions.
type Manager struct {
	sessions map[string]Session
	mu       sync.RWMutex
}

// NewManager creates a new session manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]Session),
	}
}

// Add registers a session with the manager.
func (m *Manager) Add(s Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID()] = s
}

// Get retrieves a session by ID.
func (m *Manager) Get(id string) (Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// List returns all current sessions.
func (m *Manager) List() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	return list
}

// Remove closes and removes a session.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	return s.Close()
}

// CloseAll closes all sessions.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := make([]Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]Session)
	m.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}
}
