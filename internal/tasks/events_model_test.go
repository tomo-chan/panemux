package tasks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testEpoch = "0123456789abcdef"

func okHost(name string) HostResult {
	at := time.Unix(100, 0)
	return HostResult{Name: name, Status: HostOK, CollectedAt: &at}
}

func liveTask(host, id string, state State) Task {
	since := time.Unix(50, 0).UTC()
	return Task{
		ID: id, Host: host, Agent: "claude", SessionID: "s-" + id, CWD: "/workspace/user/project",
		State: state, StatusSince: &since, PID: 4242,
		Location: Location{Kind: LocationTmux, TmuxSession: "work", Attachable: true},
	}
}

func waitTask(host, id, waitingFor, signature string) Task {
	t := liveTask(host, id, StateWait)
	t.WaitingFor = waitingFor
	t.WaitSignature = signature
	return t
}

// startedModel is a model observing the panemux host only, whose first
// answer has been applied with tasks.
func startedModel(t *testing.T, tasks ...Task) *eventModel {
	t.Helper()
	m := newEventModel(testEpoch)
	m.syncHosts([]string{""})
	m.applyHost("", okHost(""), tasks)
	return m
}

func TestEventModel_NewHostIsAddedPendingThenOK(t *testing.T) {
	m := newEventModel(testEpoch)

	frames := m.syncHosts([]string{"gpu-box", ""})
	require.Len(t, frames, 2)
	assert.Equal(t, Frame{Type: FrameHost, Epoch: testEpoch, Seq: 1, Op: OpAdded,
		Host: &HostView{Name: "", Status: HostPending}}, frames[0])
	assert.Equal(t, Frame{Type: FrameHost, Epoch: testEpoch, Seq: 2, Op: OpAdded,
		Host: &HostView{Name: "gpu-box", Status: HostPending}}, frames[1])

	frames = m.applyHost("gpu-box", okHost("gpu-box"), nil)
	require.Len(t, frames, 1)
	assert.Equal(t, Frame{Type: FrameHost, Epoch: testEpoch, Seq: 3, Op: OpChanged,
		Host: &HostView{Name: "gpu-box", Status: HostOK}}, frames[0])

	assert.Empty(t, m.syncHosts([]string{"", "gpu-box"}), "a known host is not added again")
	assert.Empty(t, m.applyHost("gpu-box", okHost("gpu-box"), nil), "an unchanged host publishes nothing")
}

func TestEventModel_TaskAddedCarriesItsViewOnly(t *testing.T) {
	m := newEventModel(testEpoch)
	m.syncHosts([]string{""})
	task := liveTask("", "local:claude:a", StateBusy)
	task.StartedAt = &time.Time{}
	task.Log = &LogVersion{ModTime: 1, Size: 2}
	task.WaitingFor = "stale text from a previous wait"

	frames := m.applyHost("", okHost(""), []Task{task})

	require.Len(t, frames, 2)
	assert.Equal(t, OpChanged, frames[0].Op)
	assert.Equal(t, Frame{Type: FrameTask, Epoch: testEpoch, Seq: 3, Op: OpAdded, Task: &TaskView{
		ID: "local:claude:a", Host: "", Agent: "claude", SessionID: "s-local:claude:a",
		CWD: "/workspace/user/project", State: StateBusy, StatusSince: task.StatusSince,
		Location: Location{Kind: LocationTmux, TmuxSession: "work", Attachable: true},
	}}, frames[1], "waiting_for is dropped outside wait; pid, start and log are not part of the view")
}

func TestEventModel_RecollectionWithoutChangePublishesNothing(t *testing.T) {
	m := startedModel(t, liveTask("", "local:claude:a", StateBusy))
	again := liveTask("", "local:claude:a", StateBusy)
	later := time.Unix(51, 0).UTC()
	again.StatusSince = &later
	again.PID = 9999
	again.Log = &LogVersion{ModTime: 5, Size: 6}

	assert.Empty(t, m.applyHost("", okHost(""), []Task{again}))

	snap := m.snapshot()
	require.Len(t, snap.Tasks, 1)
	assert.Equal(t, &later, snap.Tasks[0].StatusSince, "the snapshot carries the latest status_since")
	assert.Equal(t, uint64(3), snap.Seq)
}

func TestEventModel_StateTransitionIsOneChangedFrame(t *testing.T) {
	m := startedModel(t, liveTask("", "local:claude:a", StateBusy))

	frames := m.applyHost("", okHost(""), []Task{waitTask("", "local:claude:a", "input needed", "w1-abc")})

	require.Len(t, frames, 1)
	f := frames[0]
	assert.Equal(t, FrameTask, f.Type)
	assert.Equal(t, OpChanged, f.Op)
	assert.Equal(t, uint64(4), f.Seq)
	assert.Equal(t, StateBusy, f.PrevState)
	assert.Equal(t, StateWait, f.Task.State)
	assert.Equal(t, "input needed", f.Task.WaitingFor)
	assert.Equal(t, "w1-abc", f.Task.WaitID)

	assert.Empty(t, m.applyHost("", okHost(""), []Task{waitTask("", "local:claude:a", "input needed", "w1-abc")}),
		"the same signed wait observed again is not a change")

	frames = m.applyHost("", okHost(""), []Task{liveTask("", "local:claude:a", StateBusy)})
	require.Len(t, frames, 1)
	assert.Equal(t, StateWait, frames[0].PrevState)
	assert.Empty(t, frames[0].Task.WaitID)
	assert.Empty(t, frames[0].Task.WaitingFor)
}

func TestEventModel_UnsignedWaitID(t *testing.T) {
	m := startedModel(t, liveTask("", "local:claude:a", StateBusy))

	frames := m.applyHost("", okHost(""), []Task{waitTask("", "local:claude:a", "input needed", "")})
	require.Len(t, frames, 1)
	assert.Equal(t, "e1-"+testEpoch+"-4", frames[0].Task.WaitID, "the stream position the wait was first observed at")

	assert.Empty(t, m.applyHost("", okHost(""), []Task{waitTask("", "local:claude:a", "input needed", "")}),
		"staying in the same wait keeps the id")

	frames = m.applyHost("", okHost(""), []Task{waitTask("", "local:claude:a", "permission", "")})
	require.Len(t, frames, 1, "a different waiting_for is another wait")
	assert.Equal(t, StateWait, frames[0].PrevState)
	assert.Equal(t, "e1-"+testEpoch+"-5", frames[0].Task.WaitID)

	m.applyHost("", okHost(""), []Task{liveTask("", "local:claude:a", StateBusy)})
	frames = m.applyHost("", okHost(""), []Task{waitTask("", "local:claude:a", "permission", "")})
	require.Len(t, frames, 1)
	assert.Equal(t, "e1-"+testEpoch+"-7", frames[0].Task.WaitID, "a wait left and entered again gets a new id")
}

func TestEventModel_SignedAndUnsignedWaitIDsDoNotMix(t *testing.T) {
	m := startedModel(t, waitTask("", "local:claude:a", "input needed", ""))
	unsigned := m.snapshot().Tasks[0].WaitID
	require.Equal(t, "e1-"+testEpoch+"-3", unsigned)

	frames := m.applyHost("", okHost(""), []Task{waitTask("", "local:claude:a", "input needed", "w1-x")})
	require.Len(t, frames, 1)
	assert.Equal(t, "w1-x", frames[0].Task.WaitID)

	frames = m.applyHost("", okHost(""), []Task{waitTask("", "local:claude:a", "input needed", "")})
	require.Len(t, frames, 1, "losing the signature never brings a signature back as an unsigned id")
	assert.Equal(t, "e1-"+testEpoch+"-5", frames[0].Task.WaitID)
}

func TestEventModel_ChangeOtherThanStateIsChangedWithSameState(t *testing.T) {
	m := startedModel(t, liveTask("", "local:claude:a", StateIdle))
	moved := liveTask("", "local:claude:a", StateIdle)
	moved.CWD = "/workspace/user/other"

	frames := m.applyHost("", okHost(""), []Task{moved})

	require.Len(t, frames, 1)
	assert.Equal(t, OpChanged, frames[0].Op)
	assert.Equal(t, StateIdle, frames[0].PrevState)
	assert.Equal(t, "/workspace/user/other", frames[0].Task.CWD)
}

func TestEventModel_RemovedAndAddedFramesAreOrdered(t *testing.T) {
	m := startedModel(t, liveTask("", "local:claude:b", StateBusy), liveTask("", "local:claude:d", StateIdle))

	frames := m.applyHost("", okHost(""), []Task{
		liveTask("", "local:claude:c", StateBusy),
		liveTask("", "local:claude:a", StateRun),
		liveTask("", "local:claude:d", StateBusy),
	})

	require.Len(t, frames, 4)
	assert.Equal(t, OpRemoved, frames[0].Op)
	assert.Equal(t, "local:claude:b", frames[0].Task.ID)
	assert.Equal(t, StateBusy, frames[0].PrevState, "removed carries the state last observed")
	assert.Equal(t, OpAdded, frames[1].Op)
	assert.Equal(t, "local:claude:a", frames[1].Task.ID)
	assert.Equal(t, OpAdded, frames[2].Op)
	assert.Equal(t, "local:claude:c", frames[2].Task.ID)
	assert.Equal(t, OpChanged, frames[3].Op)
	assert.Equal(t, "local:claude:d", frames[3].Task.ID)
	for i, f := range frames {
		assert.Equal(t, uint64(5+i), f.Seq)
	}
}

func TestEventModel_StoppedTaskIsNeverPublished(t *testing.T) {
	m := startedModel(t)

	assert.Empty(t, m.applyHost("", okHost(""), []Task{liveTask("", "local:claude:a", StateStop)}))
	assert.Empty(t, m.snapshot().Tasks)
}

func TestEventModel_FailingHostKeepsItsTasksAndPublishesOnlyItsStatus(t *testing.T) {
	m := newEventModel(testEpoch)
	m.syncHosts([]string{"", "gpu-box"})
	m.applyHost("gpu-box", okHost("gpu-box"), []Task{waitTask("gpu-box", "ssh:gpu-box:claude:a", "input needed", "")})
	m.applyHost("", okHost(""), []Task{liveTask("", "local:claude:z", StateBusy)})
	before := m.snapshot()

	frames := m.applyHost("gpu-box", HostResult{Name: "gpu-box", Status: HostError, Error: "dial: timeout"}, nil)
	require.Len(t, frames, 1)
	assert.Equal(t, Frame{Type: FrameHost, Epoch: testEpoch, Seq: before.Seq + 1, Op: OpChanged,
		Host: &HostView{Name: "gpu-box", Status: HostError, Error: "dial: timeout"}}, frames[0])
	assert.Empty(t, m.applyHost("gpu-box", HostResult{Name: "gpu-box", Status: HostError, Error: "dial: timeout"}, nil),
		"the same failure again is not a change")

	frames = m.applyHost("gpu-box", HostResult{Name: "gpu-box", Status: HostConnecting}, nil)
	require.Len(t, frames, 1)
	assert.Equal(t, &HostView{Name: "gpu-box", Status: HostConnecting}, frames[0].Host)

	snap := m.snapshot()
	assert.Equal(t, before.Tasks, snap.Tasks, "tasks stay as last observed while their host fails")

	// The host answers again: the difference from what was kept is published,
	// and the still-unsigned wait keeps its id.
	frames = m.applyHost("gpu-box", okHost("gpu-box"), []Task{
		waitTask("gpu-box", "ssh:gpu-box:claude:a", "input needed", ""),
		liveTask("gpu-box", "ssh:gpu-box:claude:b", StateIdle),
	})
	require.Len(t, frames, 2)
	assert.Equal(t, FrameHost, frames[0].Type)
	assert.Equal(t, HostOK, frames[0].Host.Status)
	assert.Equal(t, OpAdded, frames[1].Op)
	assert.Equal(t, "ssh:gpu-box:claude:b", frames[1].Task.ID)
	assert.Equal(t, before.Tasks[0].WaitID, m.snapshot().Tasks[0].WaitID)
}

func TestEventModel_RemovedHostRemovesItsTasksThenItself(t *testing.T) {
	m := newEventModel(testEpoch)
	m.syncHosts([]string{"", "gpu-box"})
	m.applyHost("gpu-box", okHost("gpu-box"), []Task{
		liveTask("gpu-box", "ssh:gpu-box:claude:a", StateBusy),
		liveTask("gpu-box", "ssh:gpu-box:claude:b", StateIdle),
	})
	m.applyHost("", okHost(""), []Task{liveTask("", "local:claude:z", StateBusy)})

	frames := m.syncHosts([]string{""})

	require.Len(t, frames, 3)
	assert.Equal(t, OpRemoved, frames[0].Op)
	assert.Equal(t, "ssh:gpu-box:claude:a", frames[0].Task.ID)
	assert.Equal(t, "ssh:gpu-box:claude:b", frames[1].Task.ID)
	assert.Equal(t, Frame{Type: FrameHost, Epoch: testEpoch, Seq: frames[1].Seq + 1, Op: OpRemoved,
		Host: &HostView{Name: "gpu-box", Status: HostOK}}, frames[2])
	assert.Empty(t, m.applyHost("gpu-box", okHost("gpu-box"), nil), "a result for a removed host is dropped")

	snap := m.snapshot()
	assert.Equal(t, []HostView{{Name: "", Status: HostOK}}, snap.Hosts)
	require.Len(t, snap.Tasks, 1)
	assert.Equal(t, "local:claude:z", snap.Tasks[0].ID)
}

func TestEventModel_MarkPendingOnlyChangesHostsNotAlreadyPending(t *testing.T) {
	m := newEventModel(testEpoch)
	m.syncHosts([]string{"", "gpu-box"})
	m.applyHost("", okHost(""), []Task{waitTask("", "local:claude:a", "input needed", "")})

	frames := m.markPending()

	require.Len(t, frames, 1)
	assert.Equal(t, &HostView{Name: "", Status: HostPending}, frames[0].Host)
	assert.Len(t, m.snapshot().Tasks, 1, "pending keeps the tasks")
}

func TestEventModel_SnapshotOrdersHostsAndTasks(t *testing.T) {
	m := newEventModel(testEpoch)
	empty := m.snapshot()
	assert.Equal(t, FrameSnapshot, empty.Type)
	assert.Equal(t, uint64(0), empty.Seq)
	assert.NotNil(t, empty.Hosts)
	assert.NotNil(t, empty.Tasks)

	m.syncHosts([]string{"b-host", "", "a-host"})
	m.applyHost("b-host", okHost("b-host"), []Task{liveTask("b-host", "ssh:b-host:claude:a", StateBusy)})
	m.applyHost("", okHost(""), []Task{liveTask("", "local:claude:z", StateBusy)})

	snap := m.snapshot()
	assert.Equal(t, []HostView{
		{Name: "", Status: HostOK}, {Name: "a-host", Status: HostPending}, {Name: "b-host", Status: HostOK},
	}, snap.Hosts)
	require.Len(t, snap.Tasks, 2)
	assert.Equal(t, "local:claude:z", snap.Tasks[0].ID)
	assert.Equal(t, "ssh:b-host:claude:a", snap.Tasks[1].ID)
	assert.Equal(t, uint64(7), snap.Seq)
}
