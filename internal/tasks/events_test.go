package tasks

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEventSource answers each host's collection only when the test hands
// it a result, so a test decides exactly when every observation finishes.
type fakeEventSource struct {
	answers map[string]chan hostAnswer
	calls   map[string]int
	// started receives a host name each time an observation of it begins.
	started chan string
	hosts   []string
	// ctxs records the context each observation ran under.
	ctxs []context.Context
	mu   sync.Mutex
}

type hostAnswer struct {
	result HostResult
	tasks  []Task
}

func newFakeEventSource(hosts ...string) *fakeEventSource {
	return &fakeEventSource{
		hosts:   hosts,
		answers: map[string]chan hostAnswer{},
		calls:   map[string]int{},
		started: make(chan string, 64),
	}
}

func (f *fakeEventSource) Hosts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hosts...)
}

func (f *fakeEventSource) setHosts(hosts ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hosts = hosts
}

func (f *fakeEventSource) answerChan(name string) chan hostAnswer {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := f.answers[name]
	if ch == nil {
		ch = make(chan hostAnswer)
		f.answers[name] = ch
	}
	return ch
}

func (f *fakeEventSource) CollectHostLive(ctx context.Context, name string) (HostResult, []Task) {
	ch := f.answerChan(name)
	f.mu.Lock()
	f.calls[name]++
	f.ctxs = append(f.ctxs, ctx)
	f.mu.Unlock()
	f.started <- name
	a := <-ch
	return a.result, a.tasks
}

func (f *fakeEventSource) callCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

// answer waits for an observation of name to begin and finishes it.
func (f *fakeEventSource) answer(t *testing.T, name string, result HostResult, tasks ...Task) {
	t.Helper()
	f.awaitStart(t, name)
	f.answerChan(name) <- hostAnswer{result: result, tasks: tasks}
}

func (f *fakeEventSource) awaitStart(t *testing.T, name string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-f.started:
			if got == name {
				return
			}
			// Another host began; let the test answer it later.
			go func() { f.started <- got }()
			time.Sleep(time.Millisecond)
		case <-deadline:
			t.Fatalf("no observation of %q began", name)
		}
	}
}

func newTestPublisher(t *testing.T, src EventSource) *Publisher {
	t.Helper()
	p := NewPublisher(src, PublisherOptions{Interval: time.Millisecond, Buffer: 16})
	t.Cleanup(p.Close)
	return p
}

func nextFrame(t *testing.T, frames <-chan Frame) Frame {
	t.Helper()
	select {
	case f, ok := <-frames:
		require.True(t, ok, "the subscription was closed")
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame arrived")
		return Frame{}
	}
}

func assertNoFrame(t *testing.T, frames <-chan Frame) {
	t.Helper()
	select {
	case f := <-frames:
		t.Fatalf("unexpected frame %+v", f)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestPublisher_FirstSubscriberStartsObservationWithPendingHosts(t *testing.T) {
	src := newFakeEventSource("gpu-box")
	p := newTestPublisher(t, src)

	snap, frames, cancel := p.Subscribe()
	defer cancel()

	assert.Equal(t, FrameSnapshot, snap.Type)
	assert.Len(t, snap.Epoch, 16)
	assert.Equal(t, []HostView{{Name: "", Status: HostPending}, {Name: "gpu-box", Status: HostPending}}, snap.Hosts)
	assert.Empty(t, snap.Tasks)

	src.answer(t, "gpu-box", okHost("gpu-box"), liveTask("gpu-box", "ssh:gpu-box:claude:a", StateBusy))
	f := nextFrame(t, frames)
	assert.Equal(t, snap.Seq+1, f.Seq, "the first frame after a snapshot is seq + 1")
	assert.Equal(t, &HostView{Name: "gpu-box", Status: HostOK}, f.Host)
	f = nextFrame(t, frames)
	assert.Equal(t, OpAdded, f.Op)
	assert.Equal(t, snap.Epoch, f.Epoch)
}

func TestPublisher_OneObservationServesEverySubscriber(t *testing.T) {
	src := newFakeEventSource()
	p := newTestPublisher(t, src)
	_, a, cancelA := p.Subscribe()
	defer cancelA()
	snapB, b, cancelB := p.Subscribe()
	defer cancelB()
	assert.Equal(t, []HostView{{Name: "", Status: HostPending}}, snapB.Hosts)

	src.answer(t, "", okHost(""), waitTask("", "local:claude:a", "input needed", "w1-x"))

	for _, frames := range []<-chan Frame{a, b} {
		assert.Equal(t, FrameHost, nextFrame(t, frames).Type)
		f := nextFrame(t, frames)
		assert.Equal(t, "w1-x", f.Task.WaitID)
	}
	src.awaitStart(t, "")
	assert.Equal(t, 2, src.callCount(""), "two subscribers do not double the observation")
}

func TestPublisher_SlowHostDoesNotHoldUpOthers(t *testing.T) {
	src := newFakeEventSource("slow")
	p := newTestPublisher(t, src)
	_, frames, cancel := p.Subscribe()
	defer cancel()

	src.awaitStart(t, "slow")
	src.answer(t, "", okHost(""))
	assert.Equal(t, "", nextFrame(t, frames).Host.Name)
	src.answer(t, "", okHost(""), liveTask("", "local:claude:a", StateBusy))
	assert.Equal(t, "local:claude:a", nextFrame(t, frames).Task.ID)

	// The slow host finally answers, with a failure.
	src.answerChan("slow") <- hostAnswer{result: HostResult{Name: "slow", Status: HostError, Error: "timeout"}}
	f := nextFrame(t, frames)
	assert.Equal(t, &HostView{Name: "slow", Status: HostError, Error: "timeout"}, f.Host)
}

func TestPublisher_LastSubscriberLeavingStopsWithoutInterruptingAndKeepsModel(t *testing.T) {
	src := newFakeEventSource()
	p := newTestPublisher(t, src)
	_, frames, cancel := p.Subscribe()
	src.answer(t, "", okHost(""), waitTask("", "local:claude:a", "input needed", ""))
	nextFrame(t, frames)
	added := nextFrame(t, frames)
	waitID := added.Task.WaitID
	require.NotEmpty(t, waitID)

	// The next observation is in flight when the last subscriber leaves.
	src.awaitStart(t, "")
	cancel()
	_, open := <-frames
	assert.False(t, open, "cancel closes the subscription")
	src.mu.Lock()
	inFlight := src.ctxs[len(src.ctxs)-1]
	src.mu.Unlock()
	assert.NoError(t, inFlight.Err(), "stopping does not interrupt an observation in flight")
	src.answerChan("") <- hostAnswer{result: okHost(""), tasks: []Task{liveTask("", "local:claude:a", StateBusy)}}
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 2, src.callCount(""), "no observation starts without a subscriber")

	// A reload: observation starts again from the kept model.
	snap, frames, cancel := p.Subscribe()
	defer cancel()
	assert.Equal(t, added.Seq+1, snap.Seq,
		"the result that arrived after stopping was discarded; only pending was published")
	assert.Equal(t, []HostView{{Name: "", Status: HostPending}}, snap.Hosts)
	require.Len(t, snap.Tasks, 1)
	assert.Equal(t, waitID, snap.Tasks[0].WaitID)

	src.answer(t, "", okHost(""), waitTask("", "local:claude:a", "input needed", ""))
	assert.Equal(t, HostOK, nextFrame(t, frames).Host.Status)
	assertNoFrame(t, frames)
}

func TestPublisher_HostsAddedAndRemovedFromConfigArePickedUp(t *testing.T) {
	src := newFakeEventSource("old")
	p := newTestPublisher(t, src)
	_, frames, cancel := p.Subscribe()
	defer cancel()
	src.answer(t, "old", okHost("old"), liveTask("old", "ssh:old:claude:a", StateBusy))
	nextFrame(t, frames)
	nextFrame(t, frames)

	src.setHosts("new")

	var got []Frame
	for len(got) < 3 {
		got = append(got, nextFrame(t, frames))
	}
	assert.Equal(t, OpRemoved, got[0].Op)
	assert.Equal(t, "ssh:old:claude:a", got[0].Task.ID)
	assert.Equal(t, Frame{Type: FrameHost, Epoch: got[0].Epoch, Seq: got[0].Seq + 1, Op: OpRemoved,
		Host: &HostView{Name: "old", Status: HostOK}}, got[1])
	assert.Equal(t, &HostView{Name: "new", Status: HostPending}, got[2].Host)
	assert.Equal(t, OpAdded, got[2].Op)

	src.answer(t, "new", okHost("new"))
	assert.Equal(t, &HostView{Name: "new", Status: HostOK}, nextFrame(t, frames).Host)
}

func TestPublisher_SubscriberThatCannotKeepUpIsClosedNotStalled(t *testing.T) {
	src := newFakeEventSource()
	p := NewPublisher(src, PublisherOptions{Interval: time.Millisecond, Buffer: 3})
	t.Cleanup(p.Close)
	_, slow, cancelSlow := p.Subscribe()
	defer cancelSlow()
	_, fast, cancelFast := p.Subscribe()
	defer cancelFast()

	src.answer(t, "", okHost(""), liveTask("", "local:claude:a", StateBusy), liveTask("", "local:claude:b", StateBusy))
	for i := 0; i < 3; i++ {
		nextFrame(t, fast)
	}
	src.answer(t, "", okHost(""), liveTask("", "local:claude:a", StateIdle), liveTask("", "local:claude:b", StateIdle))
	for i := 0; i < 2; i++ {
		assert.Equal(t, StateIdle, nextFrame(t, fast).Task.State, "the subscriber keeping up is not held back")
	}

	for i := 0; i < 3; i++ {
		<-slow
	}
	_, open := <-slow
	assert.False(t, open, "a subscriber whose buffer overflowed is closed rather than given a gap")
}

func TestPublisher_CloseEndsSubscriptionsAndCancelsObservation(t *testing.T) {
	src := newFakeEventSource()
	p := NewPublisher(src, PublisherOptions{Interval: time.Millisecond})
	_, frames, cancel := p.Subscribe()
	src.awaitStart(t, "")

	p.Close()
	cancel()

	_, open := <-frames
	assert.False(t, open)
	src.mu.Lock()
	ctx := src.ctxs[0]
	src.mu.Unlock()
	assert.Error(t, ctx.Err(), "shutting down cancels the observation in flight")
	_, closed, cancel2 := p.Subscribe()
	defer cancel2()
	_, open = <-closed
	assert.False(t, open, "a closed publisher hands out closed subscriptions")
}

func TestPublisher_DroppingTheLastSubscriberStopsObservation(t *testing.T) {
	src := newFakeEventSource()
	p := NewPublisher(src, PublisherOptions{Interval: time.Millisecond, Buffer: 1})
	t.Cleanup(p.Close)
	_, frames, cancel := p.Subscribe()
	defer cancel()

	src.answer(t, "", okHost(""), liveTask("", "local:claude:a", StateBusy))

	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 1, src.callCount(""), "nobody is left to observe for")
	<-frames
	_, open := <-frames
	assert.False(t, open)
}

func TestFrame_MarshalJSONWritesOnlyItsTypesFields(t *testing.T) {
	since := time.Date(2026, 10, 2, 9, 59, 58, 0, time.UTC)
	snap, err := json.Marshal(Frame{Type: FrameSnapshot, Epoch: testEpoch, Seq: 0})
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"snapshot","epoch":"`+testEpoch+`","seq":0,"hosts":[],"tasks":[]}`, string(snap))

	task, err := json.Marshal(Frame{Type: FrameTask, Epoch: testEpoch, Seq: 5, Op: OpChanged, PrevState: StateBusy,
		Task: &TaskView{ID: "local:claude:a", Agent: "claude", State: StateWait, WaitingFor: "input needed",
			WaitID: "w1-x", StatusSince: &since, Location: Location{Kind: LocationOutside, PaneID: "pane-1"}}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"task","epoch":"`+testEpoch+`","seq":5,"op":"changed","prev_state":"busy",
		"task":{"id":"local:claude:a","host":"","agent":"claude","state":"wait","waiting_for":"input needed",
		"wait_id":"w1-x","status_since":"2026-10-02T09:59:58Z",
		"location":{"kind":"outside","pane_id":"pane-1","attachable":false}}}`, string(task))

	host, err := json.Marshal(Frame{Type: FrameHost, Epoch: testEpoch, Seq: 6, Op: OpAdded,
		Host: &HostView{Name: "gpu-box", Status: HostPending}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"host","epoch":"`+testEpoch+`","seq":6,"op":"added",
		"host":{"name":"gpu-box","status":"pending"}}`, string(host))
}
