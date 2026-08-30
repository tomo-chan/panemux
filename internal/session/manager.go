package session

import (
	"fmt"
	"io"
	"log"
	"sync"
)

const sessionReplayLimitBytes = 256 * 1024

// Manager manages the lifecycle of all terminal sessions.
type Manager struct {
	sessions map[string]*managedSession
	mu       sync.RWMutex
}

// managedSession keeps the session handle and replay state together so
// subscription lifecycle changes stay under one owner.
//
//nolint:govet // fieldalignment: clarity is preferred over splitting this tiny state holder.
type managedSession struct {
	session        Session
	history        []byte
	subscribers    map[int]chan []byte
	nextSubscriber int
	closed         bool
	mu             sync.Mutex
}

// NewManager creates a new session manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*managedSession),
	}
}

// Add registers a session with the manager and starts buffering its output.
//
// The replay buffer exists because workspace switches unmount hidden panes in the
// browser, which closes their WebSocket readers while the PTY keeps producing
// bytes. Without a backend-side buffer, a pane that reconnects later only sees
// fresh output and appears blank until the shell redraws.
func (m *Manager) Add(s Session) {
	entry := &managedSession{
		session:     s,
		subscribers: make(map[int]chan []byte),
	}

	m.mu.Lock()
	m.sessions[s.ID()] = entry
	m.mu.Unlock()

	go entry.pump()
}

// Get retrieves a session by ID.
func (m *Manager) Get(id string) (Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.sessions[id]
	if !ok {
		return nil, false
	}
	return entry.session, true
}

// Subscribe returns buffered output plus a live stream for the session.
//
// Callers are expected to send the snapshot before consuming the live stream so a
// reconnected terminal reconstructs its recent screen contents immediately.
func (m *Manager) Subscribe(id string) ([]byte, <-chan []byte, func(), bool) {
	m.mu.RLock()
	entry, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return nil, nil, nil, false
	}

	snapshot, stream, unsubscribe := entry.subscribe()
	return snapshot, stream, unsubscribe, true
}

// List returns all current sessions.
func (m *Manager) List() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]Session, 0, len(m.sessions))
	for _, entry := range m.sessions {
		list = append(list, entry.session)
	}
	return list
}

// Remove closes and removes a session.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	entry, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	return entry.session.Close()
}

// CloseAll closes all sessions.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := make([]Session, 0, len(m.sessions))
	for _, entry := range m.sessions {
		sessions = append(sessions, entry.session)
	}
	m.sessions = make(map[string]*managedSession)
	m.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}
}

func (m *managedSession) pump() {
	buf := make([]byte, 4096)
	for {
		n, err := m.session.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			m.publish(chunk)
		}
		if err != nil {
			if err == io.EOF {
				m.closeSubscribers()
				return
			}
			log.Printf("session %s read error: %v", m.session.ID(), err)
			m.closeSubscribers()
			return
		}
	}
}

func (m *managedSession) publish(chunk []byte) {
	m.mu.Lock()
	m.history = append(m.history, chunk...)
	// The `>` boundary cannot be pinned by a test: at exactly the limit,
	// m.history[0:] copies the identical bytes, so `>=` behaves the same on
	// every input. TestManagedSession_Publish_ReplayBufferBound covers the
	// boundary's behavior; the mutant on it is equivalent, not unkilled.
	// Issue #190.
	//mutation:exempt equivalent at the boundary — history[0:] is the same bytes, so >= cannot behave differently
	if len(m.history) > sessionReplayLimitBytes {
		m.history = append([]byte(nil), m.history[len(m.history)-sessionReplayLimitBytes:]...)
	}

	for _, subscriber := range m.subscribers {
		select {
		case subscriber <- append([]byte(nil), chunk...):
		default:
			// A slow client must not block session pumping or subscription cleanup.
			// The recent replay buffer remains the source of truth for reconnects.
		}
	}
	m.mu.Unlock()
}

func (m *managedSession) subscribe() ([]byte, <-chan []byte, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot := append([]byte(nil), m.history...)
	ch := make(chan []byte, 64)
	if m.closed {
		close(ch)
		return snapshot, ch, func() {}
	}

	subscriptionID := m.nextSubscriber
	m.nextSubscriber++
	m.subscribers[subscriptionID] = ch

	unsubscribe := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		subscriber, ok := m.subscribers[subscriptionID]
		if !ok {
			return
		}
		delete(m.subscribers, subscriptionID)
		close(subscriber)
	}

	return snapshot, ch, unsubscribe
}

func (m *managedSession) closeSubscribers() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	for id, subscriber := range m.subscribers {
		delete(m.subscribers, id)
		close(subscriber)
	}
}
