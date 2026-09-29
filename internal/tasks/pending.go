package tasks

import (
	"sync"
	"time"
)

// pendingLabelsGrace is how long a new codex task's labels wait for its
// tmux session to show a task before a collection that finds none drops
// them: the first collection after a launch can run before codex has
// replaced the shell that starts it.
const pendingLabelsGrace = time.Minute

// PendingLabels holds the labels a new codex task was started with until its
// session ID is known (issue #264). Codex picks its session ID only once it
// has been given its first instruction — after any start-up screen it stops
// at — so the labels are kept by (host, tmux session) and recorded by the
// first collection that finds a codex session in that tmux session. They are
// kept in memory only: a panemux restart drops them.
type PendingLabels struct {
	now     func() time.Time
	entries map[pendingLabelsKey]pendingLabelsEntry
	mu      sync.Mutex
}

type pendingLabelsKey struct {
	host, tmuxSession string
}

type pendingLabelsEntry struct {
	since  time.Time
	labels []string
}

// NewPendingLabels returns an empty set. now defaults to time.Now.
func NewPendingLabels(now func() time.Time) *PendingLabels {
	if now == nil {
		now = time.Now
	}
	return &PendingLabels{now: now, entries: map[pendingLabelsKey]pendingLabelsEntry{}}
}

// Add keeps labels for the codex task started in tmuxSession on host.
func (p *PendingLabels) Add(host, tmuxSession string, labels []string) {
	if len(labels) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[pendingLabelsKey{host, tmuxSession}] = pendingLabelsEntry{
		since: p.now(), labels: append([]string(nil), labels...),
	}
}

// Len is how many tasks still wait for their labels.
func (p *PendingLabels) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

// Apply records, through put, the labels of every task snap shows a codex
// session for in its tmux session, and drops the labels that can no longer
// be recorded: their host is gone from the snapshot, or their host was
// collected and nothing runs in their tmux session any more (codex quit at a
// start-up screen, or the session was killed) once pendingLabelsGrace has
// passed. A failed put keeps the labels for the next collection.
func (p *PendingLabels) Apply(snap Snapshot, put func(Record) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.entries) == 0 {
		return
	}
	hosts := make(map[string]HostStatus, len(snap.Hosts))
	for _, h := range snap.Hosts {
		hosts[h.Name] = h.Status
	}
	now := p.now()
	for key, entry := range p.entries {
		status, known := hosts[key.host]
		if !known {
			delete(p.entries, key)
			continue
		}
		if status != HostOK {
			continue
		}
		sessionID, occupied := codexSessionIn(snap.Tasks, key)
		switch {
		case sessionID != "":
			if put(Record{Host: key.host, Agent: AgentCodex, SessionID: sessionID, Labels: entry.labels}) == nil {
				delete(p.entries, key)
			}
		case !occupied && now.Sub(entry.since) >= pendingLabelsGrace:
			delete(p.entries, key)
		}
	}
}

// codexSessionIn finds the codex session running in key's tmux session, and
// reports whether any task runs there at all.
func codexSessionIn(tasks []Task, key pendingLabelsKey) (string, bool) {
	occupied := false
	for _, task := range tasks {
		if task.Host != key.host || task.Location.Kind != LocationTmux || task.Location.TmuxSession != key.tmuxSession {
			continue
		}
		occupied = true
		if task.Agent == AgentCodex && task.SessionID != "" {
			return task.SessionID, true
		}
	}
	return "", occupied
}
