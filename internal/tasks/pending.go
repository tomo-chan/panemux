package tasks

import (
	"slices"
	"sync"
	"time"
)

// pendingLabelsGrace is how long a new task's labels wait for its tmux
// session to show a task of its agent before a collection that finds none
// drops them: the first collection after a launch can run before the agent
// has replaced the shell that starts it.
const pendingLabelsGrace = time.Minute

// PendingLabels holds the labels a new task was started with until its
// session ID is known (issue #264): the lifecycle of every agent whose launch
// adapter leaves the ID to the agent (launchAdapter.newSession) — codex picks
// its session ID only once it has been given its first instruction, after
// any start-up screen it stops at. The labels are kept by (host, agent, tmux
// session) and recorded by the first collection that finds exactly one
// session of that agent with an ID in that tmux session:
//
//   - a task of the agent without an ID yet keeps them waiting;
//   - a task of another agent is not the session they were meant for: it
//     neither claims them nor keeps them waiting;
//   - two sessions of the agent cannot tell which one they were meant for,
//     so neither gets them and they are dropped;
//   - nothing of the agent there once pendingLabelsGrace has passed, or
//     their host gone from the snapshot, drops them.
//
// A session ID is matched within its host only, as a record's key is. They
// are kept in memory only: a panemux restart drops them.
type PendingLabels struct {
	now     func() time.Time
	logf    func(format string, args ...any)
	entries map[pendingLabelsKey]pendingLabelsEntry
	mu      sync.Mutex
}

type pendingLabelsKey struct {
	host, agent, tmuxSession string
}

type pendingLabelsEntry struct {
	since  time.Time
	labels []string
}

// NewPendingLabels returns an empty set. now defaults to time.Now, and logf,
// where ambiguous sessions are reported, to logging nothing.
func NewPendingLabels(now func() time.Time, logf func(format string, args ...any)) *PendingLabels {
	if now == nil {
		now = time.Now
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &PendingLabels{now: now, logf: logf, entries: map[pendingLabelsKey]pendingLabelsEntry{}}
}

// Add keeps labels for the task of agent started in tmuxSession on host.
func (p *PendingLabels) Add(host, agent, tmuxSession string, labels []string) {
	if len(labels) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[pendingLabelsKey{host, agent, tmuxSession}] = pendingLabelsEntry{
		since: p.now(), labels: append([]string(nil), labels...),
	}
}

// Len is how many tasks still wait for their labels.
func (p *PendingLabels) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

// Apply records, through put, the labels of every task snap shows the
// session of in its tmux session, and drops the labels that can no longer be
// recorded (see PendingLabels). A host that was not collected keeps its
// labels as they are. A failed put keeps the labels for the next collection.
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
		ids, occupied := sessionsIn(snap.Tasks, key)
		switch {
		case len(ids) == 1:
			if put(Record{Host: key.host, Agent: key.agent, SessionID: ids[0], Labels: entry.labels}) == nil {
				delete(p.entries, key)
			}
		case len(ids) > 1:
			p.logf("task labels for tmux session %s on %s dropped: %d %s sessions run there",
				key.tmuxSession, hostName(key.host), len(ids), key.agent)
			delete(p.entries, key)
		case !occupied && now.Sub(entry.since) >= pendingLabelsGrace:
			delete(p.entries, key)
		}
	}
}

// sessionsIn is the distinct session IDs of key's agent running in key's
// tmux session, and whether any task of that agent runs there at all.
func sessionsIn(tasks []Task, key pendingLabelsKey) ([]string, bool) {
	var ids []string
	occupied := false
	for _, task := range tasks {
		if task.Host != key.host || task.Agent != key.agent ||
			task.Location.Kind != LocationTmux || task.Location.TmuxSession != key.tmuxSession {
			continue
		}
		occupied = true
		if task.SessionID != "" && !slices.Contains(ids, task.SessionID) {
			ids = append(ids, task.SessionID)
		}
	}
	return ids, occupied
}
