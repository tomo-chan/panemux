package tasks

import (
	"errors"
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
	p := NewPendingLabels(c.Now)
	p.Add("build-box", "task-0a1b2c3d", []string{"payment", "api"})
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

// A claude task in the tmux session is not the codex session the labels
// were meant for.
func TestPendingLabels_OnlyACodexSessionClaimsThem(t *testing.T) {
	c := &clock{now: time.Unix(1000, 0)}
	p := NewPendingLabels(c.Now)
	p.Add("", "task-0a1b2c3d", []string{"x"})
	log := &putLog{}
	claude := codexInTmux("", "task-0a1b2c3d", testSessionID)
	claude.Agent = AgentClaude
	p.Apply(pendingSnapshot(okHosts, claude), log.put)
	assert.Empty(t, log.puts)
}

func TestPendingLabels_AFailedWriteIsRetried(t *testing.T) {
	p := NewPendingLabels((&clock{now: time.Unix(1000, 0)}).Now)
	p.Add("", "task-0a1b2c3d", []string{"x"})
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
	p := NewPendingLabels(c.Now)
	p.Add("", "task-0a1b2c3d", []string{"x"})
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
	p := NewPendingLabels((&clock{now: time.Unix(1000, 0)}).Now)
	p.Add("build-box", "task-0a1b2c3d", []string{"x"})
	p.Apply(pendingSnapshot([]HostResult{{Name: "", Status: HostOK}}), (&putLog{}).put)
	assert.Equal(t, 0, p.Len(), "a host removed from ssh_connections")
}

func TestPendingLabels_AddCopiesAndIgnoresNoLabels(t *testing.T) {
	p := NewPendingLabels((&clock{now: time.Unix(1000, 0)}).Now)
	p.Add("", "task-a", nil)
	assert.Equal(t, 0, p.Len())
	labels := []string{"x"}
	p.Add("", "task-b", labels)
	labels[0] = "changed"
	log := &putLog{}
	p.Apply(pendingSnapshot(okHosts, codexInTmux("", "task-b", codexSessionA)), log.put)
	require.Len(t, log.puts, 1)
	assert.Equal(t, []string{"x"}, log.puts[0].Labels)
}
