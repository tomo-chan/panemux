package tasks

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pendingSnapshot(hosts []HostResult, tasks ...Task) Snapshot {
	return Snapshot{Hosts: hosts, Tasks: tasks}
}

func codexInTmux(host, name, sessionID string) Task {
	return Task{
		Host: host, Agent: AgentCodex, SessionID: sessionID, State: StateIdle,
		Location: Location{Kind: LocationTmux, TmuxSession: name, Attachable: true},
	}
}

type putLog struct {
	err  error
	puts []Record
}

func (l *putLog) put(rec Record) error {
	l.puts = append(l.puts, rec)
	return l.err
}

var okHosts = []HostResult{{Name: "", Status: HostOK}, {Name: "build-box", Status: HostOK}}

// The labels a new codex task was started with are recorded under the
// session ID the first collection that finds codex's session in the task's
// tmux session reports, and only once.
func TestPendingLabels_RecordedOnceTheSessionAppears(t *testing.T) {
	c := &clock{now: time.Unix(1000, 0)}
	p := NewPendingLabels(c.Now, nil)
	p.Add("build-box", AgentCodex, "task-0a1b2c3d", []string{"payment", "api"})
	log := &putLog{}

	// Still at a start-up screen: the process is there, the session is not.
	running := codexInTmux("build-box", "task-0a1b2c3d", "")
	running.State = StateRun
	p.Apply(pendingSnapshot(okHosts, running), log.put)
	assert.Empty(t, log.puts)

	c.advance(10 * time.Minute)
	p.Apply(pendingSnapshot(okHosts,
		codexInTmux("", "task-0a1b2c3d", codexSessionB),       // same name, another host
		codexInTmux("build-box", "task-other", codexSessionB), // another tmux session
		codexInTmux("build-box", "task-0a1b2c3d", codexSessionA),
	), log.put)
	require.Equal(t, []Record{
		{Host: "build-box", Agent: AgentCodex, SessionID: codexSessionA, Labels: []string{"payment", "api"}},
	}, log.puts)

	p.Apply(pendingSnapshot(okHosts, codexInTmux("build-box", "task-0a1b2c3d", codexSessionA)), log.put)
	assert.Len(t, log.puts, 1, "recorded once")
}

// A task of another agent in the tmux session is not the session the labels
// were meant for: it neither claims them nor keeps them waiting, so they are
// dropped once the grace has passed.
func TestPendingLabels_AnotherAgentNeitherClaimsNorHoldsThem(t *testing.T) {
	c := &clock{now: time.Unix(1000, 0)}
	p := NewPendingLabels(c.Now, nil)
	p.Add("", AgentCodex, "task-0a1b2c3d", []string{"x"})
	log := &putLog{}
	claude := codexInTmux("", "task-0a1b2c3d", testSessionID)
	claude.Agent = AgentClaude
	p.Apply(pendingSnapshot(okHosts, claude), log.put)
	assert.Empty(t, log.puts)
	assert.Equal(t, 1, p.Len(), "kept while young")

	c.advance(pendingLabelsGrace)
	p.Apply(pendingSnapshot(okHosts, claude), log.put)
	assert.Empty(t, log.puts)
	assert.Equal(t, 0, p.Len(), "another agent does not keep them waiting")
}

// The lifecycle is the agent's, not codex's: labels held for any agent are
// recorded under that agent, by that agent's session only.
func TestPendingLabels_RecordedUnderTheAgentTheyWereHeldFor(t *testing.T) {
	p := NewPendingLabels((&clock{now: time.Unix(1000, 0)}).Now, nil)
	p.Add("build-box", "third", "task-0a1b2c3d", []string{"x"})
	log := &putLog{}
	codex := codexInTmux("build-box", "task-0a1b2c3d", codexSessionA)
	third := codexInTmux("build-box", "task-0a1b2c3d", codexSessionB)
	third.Agent = "third"
	p.Apply(pendingSnapshot(okHosts, codex, third), log.put)
	assert.Equal(t, []Record{
		{Host: "build-box", Agent: "third", SessionID: codexSessionB, Labels: []string{"x"}},
	}, log.puts)
}

// Two sessions of the agent in the tmux session cannot tell which one the
// labels were meant for: none is given them, and they are dropped with a
// line in the log.
func TestPendingLabels_TwoSessionsOfTheAgentAreAmbiguous(t *testing.T) {
	var logged []string
	p := NewPendingLabels((&clock{now: time.Unix(1000, 0)}).Now, func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	})
	p.Add("", AgentCodex, "task-0a1b2c3d", []string{"x"})
	log := &putLog{}
	p.Apply(pendingSnapshot(okHosts,
		codexInTmux("", "task-0a1b2c3d", codexSessionA),
		codexInTmux("", "task-0a1b2c3d", codexSessionB),
		codexInTmux("", "task-0a1b2c3d", codexSessionA), // the same session twice is one
	), log.put)
	assert.Empty(t, log.puts)
	assert.Equal(t, 0, p.Len())
	require.Len(t, logged, 1)
	assert.Contains(t, logged[0], "task-0a1b2c3d")
	assert.Contains(t, logged[0], "2 codex sessions")
}

// The same session ID on another host is another session.
func TestPendingLabels_TheSameSessionIDOnAnotherHostIsAnotherSession(t *testing.T) {
	p := NewPendingLabels((&clock{now: time.Unix(1000, 0)}).Now, nil)
	p.Add("build-box", AgentCodex, "task-0a1b2c3d", []string{"x"})
	log := &putLog{}
	p.Apply(pendingSnapshot(okHosts, codexInTmux("", "task-0a1b2c3d", codexSessionA)), log.put)
	assert.Empty(t, log.puts)
	assert.Equal(t, 1, p.Len())
}

// One tmux session name can be held for two agents on one host, and each
// waits for its own.
func TestPendingLabels_KeyedByAgentToo(t *testing.T) {
	p := NewPendingLabels((&clock{now: time.Unix(1000, 0)}).Now, nil)
	p.Add("", AgentCodex, "task-0a1b2c3d", []string{"x"})
	p.Add("", AgentClaude, "task-0a1b2c3d", []string{"y"})
	assert.Equal(t, 2, p.Len())
}

func TestPendingLabels_AFailedWriteIsRetried(t *testing.T) {
	p := NewPendingLabels((&clock{now: time.Unix(1000, 0)}).Now, nil)
	p.Add("", AgentCodex, "task-0a1b2c3d", []string{"x"})
	log := &putLog{err: errors.New("disk full")}
	snap := pendingSnapshot(okHosts, codexInTmux("", "task-0a1b2c3d", codexSessionA))
	p.Apply(snap, log.put)
	log.err = nil
	p.Apply(snap, log.put)
	p.Apply(snap, log.put)
	assert.Len(t, log.puts, 2, "retried after the failure, then done")
}

// Labels are dropped when their tmux session holds no task any more — codex
// quit at a start-up screen, or the session was killed — once a minute has
// passed since the launch, so a collection that ran before codex appeared
// does not drop them.
func TestPendingLabels_DroppedWhenTheTmuxSessionHoldsNoTask(t *testing.T) {
	c := &clock{now: time.Unix(1000, 0)}
	p := NewPendingLabels(c.Now, nil)
	p.Add("", AgentCodex, "task-0a1b2c3d", []string{"x"})
	log := &putLog{}

	c.advance(pendingLabelsGrace - time.Second)
	p.Apply(pendingSnapshot(okHosts), log.put)
	c.advance(time.Second)
	p.Apply(pendingSnapshot([]HostResult{{Name: "", Status: HostError}}), log.put)
	assert.Equal(t, 1, p.Len(), "kept while young, and while the host could not be collected")

	p.Apply(pendingSnapshot(okHosts), log.put)
	assert.Equal(t, 0, p.Len())
	p.Apply(pendingSnapshot(okHosts, codexInTmux("", "task-0a1b2c3d", codexSessionA)), log.put)
	assert.Empty(t, log.puts)
}

func TestPendingLabels_DroppedWithTheirHost(t *testing.T) {
	p := NewPendingLabels((&clock{now: time.Unix(1000, 0)}).Now, nil)
	p.Add("build-box", AgentCodex, "task-0a1b2c3d", []string{"x"})
	p.Apply(pendingSnapshot([]HostResult{{Name: "", Status: HostOK}}), (&putLog{}).put)
	assert.Equal(t, 0, p.Len(), "a host removed from ssh_connections")
}

func TestNewPendingLabels_DefaultsToTheWallClock(t *testing.T) {
	p := NewPendingLabels(nil, nil)
	p.Add("", AgentCodex, "task-a", []string{"x"})
	p.Apply(pendingSnapshot(okHosts), (&putLog{}).put)
	assert.Equal(t, 1, p.Len(), "a minute has not passed on the wall clock")
}

func TestPendingLabels_AddCopiesAndIgnoresNoLabels(t *testing.T) {
	p := NewPendingLabels((&clock{now: time.Unix(1000, 0)}).Now, nil)
	p.Add("", AgentCodex, "task-a", nil)
	assert.Equal(t, 0, p.Len())
	labels := []string{"x"}
	p.Add("", AgentCodex, "task-b", labels)
	labels[0] = "changed"
	log := &putLog{}
	p.Apply(pendingSnapshot(okHosts, codexInTmux("", "task-b", codexSessionA)), log.put)
	require.Len(t, log.puts, 1)
	assert.Equal(t, []string{"x"}, log.puts[0].Labels)
}
