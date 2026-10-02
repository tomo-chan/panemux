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
	session     Session
	history     *replayBuffer
	subscribers map[int]chan []byte
	// watch, when set, is told the subscriber count after each change.
	watch          func(subscribers int)
	nextSubscriber int
	// changes numbers each subscriber-count change under mu; delivered is
	// the latest one handed to watch, under notifyMu.
	changes   uint64
	delivered uint64
	closed    bool
	mu        sync.Mutex
	notifyMu  sync.Mutex
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

// Watch makes fn hear the session's subscriber count each time a subscriber
// is added or removed, so an owner can act when nothing reads the session any
// more. It replaces any earlier watcher and reports false for an unknown ID.
//
// fn runs outside the lock that guards the subscribers, one call at a time,
// and a count older than one fn has already heard is dropped rather than
// delivered late, so the last count fn heard is the current one. fn must not
// subscribe to or unsubscribe from the same session.
func (m *Manager) Watch(id string, fn func(subscribers int)) bool {
	m.mu.RLock()
	entry, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	entry.mu.Lock()
	entry.watch = fn
	entry.mu.Unlock()
	return true
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

// replay returns the session's replay buffer, creating it on first use.
//
// Lazy rather than built in Add so the zero value of managedSession is usable:
// several tests construct one directly to drive publish and subscribe without a
// Session behind them. Callers must already hold m.mu.
func (m *managedSession) replay() *replayBuffer {
	if m.history == nil {
		m.history = newReplayBuffer(sessionReplayLimitBytes)
	}
	return m.history
}

func (m *managedSession) publish(chunk []byte) {
	m.mu.Lock()
	m.replay().append(chunk)

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

	snapshot := m.replay().snapshot()
	ch := make(chan []byte, 64)
	if m.closed {
		m.mu.Unlock()
		close(ch)
		return snapshot, ch, func() {}
	}

	subscriptionID := m.nextSubscriber
	m.nextSubscriber++
	m.subscribers[subscriptionID] = ch
	notify := m.countChangedLocked()
	m.mu.Unlock()
	notify()

	unsubscribe := func() {
		m.mu.Lock()
		subscriber, ok := m.subscribers[subscriptionID]
		if !ok {
			m.mu.Unlock()
			return
		}
		delete(m.subscribers, subscriptionID)
		close(subscriber)
		notify := m.countChangedLocked()
		m.mu.Unlock()
		notify()
	}

	return snapshot, ch, unsubscribe
}

// countChangedLocked numbers a subscriber-count change and returns the func
// that reports it to the watcher; the caller runs it after releasing m.mu.
// m.mu must be held.
func (m *managedSession) countChangedLocked() func() {
	m.changes++
	change, watch, count := m.changes, m.watch, len(m.subscribers)
	return func() {
		m.notifyMu.Lock()
		defer m.notifyMu.Unlock()
		// Two changes read in order can reach here in either order; the
		// older one, arriving second, would leave the watcher a stale count.
		if watch == nil || change <= m.delivered {
			return
		}
		m.delivered = change
		watch(count)
	}
}

// closeSubscribers ends every subscription once the session's output ends.
// The subscribers' own unsubscribes then find nothing to remove and report
// nothing, so the watcher hears the drop to 0 from here.
func (m *managedSession) closeSubscribers() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	had := len(m.subscribers)
	for id, subscriber := range m.subscribers {
		delete(m.subscribers, id)
		close(subscriber)
	}
	notify := func() {}
	if had > 0 {
		notify = m.countChangedLocked()
	}
	m.mu.Unlock()
	notify()
}
