package tasks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// This file is the task event publisher (issue #293): it observes the
// running tasks on every host while someone subscribes, and publishes every
// change in their state as a frame. It only observes and publishes; what a
// change means is each receiver's decision. docs/behavior/task-events.md is
// the specification.

// HostPending is a host whose observation has started and that has not
// answered yet. Only the task event stream uses it.
const HostPending HostStatus = "pending"

// FrameType is a task event frame's `type`.
type FrameType string

// Frame types.
const (
	FrameSnapshot FrameType = "snapshot"
	FrameTask     FrameType = "task"
	FrameHost     FrameType = "host"
)

// FrameOp is a task or host frame's `op`.
type FrameOp string

// Frame ops.
const (
	OpAdded   FrameOp = "added"
	OpChanged FrameOp = "changed"
	OpRemoved FrameOp = "removed"
)

// TaskView is what the stream publishes about one running task. A change is
// a view that differs from the previous one in anything but StatusSince.
// It carries no conversation text, prompt, command line or process ID.
type TaskView struct {
	// StatusSince is carried as of the change and never compared: it is
	// converted from the host's clock on every collection.
	StatusSince *time.Time `json:"status_since,omitempty"`
	ID          string     `json:"id"`
	Host        string     `json:"host"`
	Agent       string     `json:"agent"`
	SessionID   string     `json:"session_id,omitempty"`
	CWD         string     `json:"cwd,omitempty"`
	State       State      `json:"state"`
	WaitingFor  string     `json:"waiting_for,omitempty"`
	WaitID      string     `json:"wait_id,omitempty"`
	Location    Location   `json:"location"`
}

// HostView is what the stream publishes about one host.
type HostView struct {
	Name   string     `json:"name"`
	Status HostStatus `json:"status"`
	Error  string     `json:"error,omitempty"`
}

// Frame is one frame of the task event stream.
type Frame struct {
	// Task, Host, Op and PrevState are a task or host frame's.
	Task      *TaskView
	Host      *HostView
	Type      FrameType
	Epoch     string
	Op        FrameOp
	PrevState State
	// Hosts and Tasks are a snapshot's.
	Hosts []HostView
	Tasks []TaskView
	Seq   uint64
}

type snapshotFrameJSON struct {
	Type  FrameType  `json:"type"`
	Epoch string     `json:"epoch"`
	Hosts []HostView `json:"hosts"`
	Tasks []TaskView `json:"tasks"`
	Seq   uint64     `json:"seq"`
}

type changeFrameJSON struct {
	Task      *TaskView `json:"task,omitempty"`
	Host      *HostView `json:"host,omitempty"`
	Type      FrameType `json:"type"`
	Epoch     string    `json:"epoch"`
	Op        FrameOp   `json:"op"`
	PrevState State     `json:"prev_state,omitempty"`
	Seq       uint64    `json:"seq"`
}

// MarshalJSON writes the fields of the frame's type only.
func (f Frame) MarshalJSON() ([]byte, error) {
	var v any
	if f.Type == FrameSnapshot {
		hosts, tasks := f.Hosts, f.Tasks
		if hosts == nil {
			hosts = []HostView{}
		}
		if tasks == nil {
			tasks = []TaskView{}
		}
		v = snapshotFrameJSON{Type: f.Type, Epoch: f.Epoch, Seq: f.Seq, Hosts: hosts, Tasks: tasks}
	} else {
		v = changeFrameJSON{Type: f.Type, Epoch: f.Epoch, Seq: f.Seq, Op: f.Op,
			PrevState: f.PrevState, Task: f.Task, Host: f.Host}
	}
	out, err := json.Marshal(v)
	if err != nil {
		//coverage:exempt every field is a string, number, time or nested struct of them
		return nil, fmt.Errorf("marshal task event frame: %w", err)
	}
	return out, nil
}

// eventHost is one host as the model last observed it.
type eventHost struct {
	tasks map[string]TaskView
	view  HostView
}

// eventModel is what the publisher last observed and the stream position.
// It is not safe for concurrent use; the Publisher serializes it.
type eventModel struct {
	hosts map[string]*eventHost
	epoch string
	seq   uint64
}

func newEventModel(epoch string) *eventModel {
	return &eventModel{epoch: epoch, hosts: map[string]*eventHost{}}
}

func (m *eventModel) next(f Frame) Frame {
	m.seq++
	f.Epoch = m.epoch
	f.Seq = m.seq
	return f
}

func (m *eventModel) hostFrame(op FrameOp, view HostView) Frame {
	return m.next(Frame{Type: FrameHost, Op: op, Host: &view})
}

func (m *eventModel) taskFrame(op FrameOp, prev State, view TaskView) Frame {
	return m.next(Frame{Type: FrameTask, Op: op, PrevState: prev, Task: &view})
}

// syncHosts makes the model's hosts names: a host no longer listed is
// removed, after each of its tasks, and a new one is added as pending.
func (m *eventModel) syncHosts(names []string) []Frame {
	keep := make(map[string]bool, len(names))
	for _, name := range names {
		keep[name] = true
	}
	var frames []Frame
	for _, name := range m.sortedHostNames() {
		if keep[name] {
			continue
		}
		h := m.hosts[name]
		for _, id := range sortedTaskIDs(h.tasks) {
			view := h.tasks[id]
			frames = append(frames, m.taskFrame(OpRemoved, view.State, view))
		}
		delete(m.hosts, name)
		frames = append(frames, m.hostFrame(OpRemoved, h.view))
	}
	added := append([]string(nil), names...)
	sort.Strings(added)
	for _, name := range added {
		if _, ok := m.hosts[name]; ok {
			continue
		}
		h := &eventHost{view: HostView{Name: name, Status: HostPending}, tasks: map[string]TaskView{}}
		m.hosts[name] = h
		frames = append(frames, m.hostFrame(OpAdded, h.view))
	}
	return frames
}

// markPending makes every host pending, as observation starts again.
func (m *eventModel) markPending() []Frame {
	var frames []Frame
	for _, name := range m.sortedHostNames() {
		h := m.hosts[name]
		if h.view.Status == HostPending {
			continue
		}
		h.view = HostView{Name: name, Status: HostPending}
		frames = append(frames, m.hostFrame(OpChanged, h.view))
	}
	return frames
}

// applyHost records one observation of a host. A host that did not answer
// publishes its status only and keeps its tasks as last observed; one that
// did publishes the difference from them.
func (m *eventModel) applyHost(name string, result HostResult, observed []Task) []Frame {
	h, ok := m.hosts[name]
	if !ok {
		return nil
	}
	var frames []Frame
	view := HostView{Name: name, Status: result.Status, Error: result.Error}
	if view != h.view {
		h.view = view
		frames = append(frames, m.hostFrame(OpChanged, view))
	}
	if result.Status != HostOK {
		return frames
	}

	current := make(map[string]Task, len(observed))
	for _, task := range observed {
		if task.State == StateStop {
			continue
		}
		if _, dup := current[task.ID]; !dup {
			current[task.ID] = task
		}
	}
	for _, id := range sortedTaskIDs(h.tasks) {
		if _, still := current[id]; still {
			continue
		}
		prev := h.tasks[id]
		delete(h.tasks, id)
		frames = append(frames, m.taskFrame(OpRemoved, prev.State, prev))
	}
	ids := make([]string, 0, len(current))
	for id := range current {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		prev, known := h.tasks[id]
		next := m.view(current[id], prev, known)
		h.tasks[id] = next
		switch {
		case !known:
			frames = append(frames, m.taskFrame(OpAdded, "", next))
		case !sameView(prev, next):
			frames = append(frames, m.taskFrame(OpChanged, prev.State, next))
		}
		// The wait ID of a new unsigned wait is the position of the frame
		// that publishes it, known only once that frame has its seq.
		if next.WaitID == pendingWaitID {
			next.WaitID = unsignedWaitID(m.epoch, m.seq)
			h.tasks[id] = next
			frames[len(frames)-1].Task.WaitID = next.WaitID
		}
	}
	return frames
}

// pendingWaitID stands for an unsigned wait ID not yet assigned. A wait
// always publishes a frame when it starts, which assigns it.
const pendingWaitID = "\x00pending"

func unsignedWaitID(epoch string, seq uint64) string {
	return fmt.Sprintf("e1-%s-%d", epoch, seq)
}

// view reduces an observed task to its view. prev is the task's previous
// view when known.
func (m *eventModel) view(task Task, prev TaskView, known bool) TaskView {
	v := TaskView{
		StatusSince: task.StatusSince,
		ID:          task.ID,
		Host:        task.Host,
		Agent:       task.Agent,
		SessionID:   task.SessionID,
		CWD:         task.CWD,
		State:       task.State,
		Location:    task.Location,
	}
	if task.State != StateWait {
		return v
	}
	v.WaitingFor = task.WaitingFor
	switch {
	case task.WaitSignature != "":
		v.WaitID = task.WaitSignature
	case known && prev.State == StateWait && prev.WaitingFor == task.WaitingFor && isUnsignedWaitID(prev.WaitID):
		v.WaitID = prev.WaitID
	default:
		v.WaitID = pendingWaitID
	}
	return v
}

func isUnsignedWaitID(id string) bool {
	return len(id) > 3 && id[:3] == "e1-"
}

// sameView compares two views without StatusSince.
func sameView(a, b TaskView) bool {
	a.StatusSince, b.StatusSince = nil, nil
	return a == b
}

func (m *eventModel) snapshot() Frame {
	f := Frame{Type: FrameSnapshot, Epoch: m.epoch, Seq: m.seq, Hosts: []HostView{}, Tasks: []TaskView{}}
	for _, name := range m.sortedHostNames() {
		h := m.hosts[name]
		f.Hosts = append(f.Hosts, h.view)
		for _, id := range sortedTaskIDs(h.tasks) {
			f.Tasks = append(f.Tasks, h.tasks[id])
		}
	}
	sort.Slice(f.Tasks, func(i, j int) bool { return f.Tasks[i].ID < f.Tasks[j].ID })
	return f
}

func (m *eventModel) sortedHostNames() []string {
	names := make([]string, 0, len(m.hosts))
	for name := range m.hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedTaskIDs(tasks map[string]TaskView) []string {
	ids := make([]string, 0, len(tasks))
	for id := range tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// EventSource is what the publisher observes. *Service is the production
// implementation.
type EventSource interface {
	// Hosts is the configured hosts besides the panemux host, read on every
	// cycle.
	Hosts() []string
	// CollectHostLive observes one host's running tasks, within its own
	// timeout.
	CollectHostLive(ctx context.Context, name string) (HostResult, []Task)
}

// PublisherOptions configures a Publisher. Every field has a default.
type PublisherOptions struct {
	// Interval is how long after one observation of a host finished the
	// next begins, and how often the hosts are read.
	Interval time.Duration
	// Buffer is how many frames a subscriber may fall behind before it is
	// closed.
	Buffer int
}

const (
	defaultEventInterval = 5 * time.Second
	defaultEventBuffer   = 256
)

// Publisher observes the hosts while the stream has a subscriber and fans
// every change out to the subscribers.
type Publisher struct {
	src EventSource
	// ctx ends only when the publisher closes: an observation in flight
	// when the last subscriber leaves runs to its own timeout.
	ctx    context.Context
	cancel context.CancelFunc

	// mu guards everything below it.
	model *eventModel
	subs  map[*eventSubscriber]struct{}
	// stop is closed when observation stops.
	stop chan struct{}
	// looping is the hosts whose observation loop is running.
	looping map[string]bool
	opts    PublisherOptions
	mu      sync.Mutex
	// observing is whether observation runs.
	observing  bool
	supervised bool
	closed     bool
}

type eventSubscriber struct {
	frames chan Frame
}

// NewPublisher makes a publisher. Nothing is observed until Subscribe.
func NewPublisher(src EventSource, opts PublisherOptions) *Publisher {
	if opts.Interval <= 0 {
		opts.Interval = defaultEventInterval
	}
	if opts.Buffer <= 0 {
		opts.Buffer = defaultEventBuffer
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Publisher{
		src:     src,
		opts:    opts,
		ctx:     ctx,
		cancel:  cancel,
		model:   newEventModel(newEpoch()),
		subs:    map[*eventSubscriber]struct{}{},
		looping: map[string]bool{},
	}
}

// newEpoch is 16 hex digits, chosen once per server start.
func newEpoch() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Subscribe returns the snapshot of everything currently known and the
// frames that follow it. The channel is closed by cancel, by the publisher
// closing, or when the subscriber falls Buffer frames behind. The first
// subscriber starts observation; cancel by the last one stops it.
func (p *Publisher) Subscribe() (Frame, <-chan Frame, func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sub := &eventSubscriber{frames: make(chan Frame, p.opts.Buffer)}
	if p.closed {
		close(sub.frames)
		return p.model.snapshot(), sub.frames, func() {}
	}
	if !p.observing {
		p.startLocked()
	}
	p.subs[sub] = struct{}{}
	var once sync.Once
	return p.model.snapshot(), sub.frames, func() { once.Do(func() { p.unsubscribe(sub) }) }
}

func (p *Publisher) unsubscribe(sub *eventSubscriber) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.subs[sub]; !ok {
		return
	}
	delete(p.subs, sub)
	close(sub.frames)
	if len(p.subs) == 0 && p.observing {
		p.observing = false
		close(p.stop)
	}
}

// Close stops observation, cancels any observation in flight and closes
// every subscription.
func (p *Publisher) Close() {
	p.cancel()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if p.observing {
		p.observing = false
		close(p.stop)
	}
	for sub := range p.subs {
		delete(p.subs, sub)
		close(sub.frames)
	}
}

// startLocked starts observation: every host becomes pending and is
// observed at once.
func (p *Publisher) startLocked() {
	p.observing = true
	p.stop = make(chan struct{})
	p.publishLocked(p.model.markPending())
	p.syncHostsLocked()
	if !p.supervised {
		p.supervised = true
		go p.superviseHosts()
	}
}

// syncHostsLocked reads the hosts, publishes the hosts added and removed
// and starts an observation loop for each host that has none.
func (p *Publisher) syncHostsLocked() {
	names := append([]string{""}, p.src.Hosts()...)
	p.publishLocked(p.model.syncHosts(names))
	for _, name := range names {
		if !p.looping[name] {
			p.looping[name] = true
			go p.observeHost(name)
		}
	}
}

// superviseHosts reads the hosts every Interval while observation runs.
func (p *Publisher) superviseHosts() {
	for {
		p.mu.Lock()
		if !p.observing {
			p.supervised = false
			p.mu.Unlock()
			return
		}
		stop := p.stop
		p.mu.Unlock()
		select {
		case <-time.After(p.opts.Interval):
		case <-stop:
		}
		p.mu.Lock()
		if p.observing {
			p.syncHostsLocked()
		}
		p.mu.Unlock()
	}
}

// observeHost is one host's cycle: observe, publish the difference, wait
// Interval, again. It ends when observation stops or the host is removed;
// a result that arrives after either is discarded.
func (p *Publisher) observeHost(name string) {
	for {
		result, observed := p.src.CollectHostLive(p.ctx, name)
		p.mu.Lock()
		if !p.continueLocked(name) {
			p.mu.Unlock()
			return
		}
		p.publishLocked(p.model.applyHost(name, result, observed))
		stop := p.stop
		p.mu.Unlock()

		select {
		case <-time.After(p.opts.Interval):
		case <-stop:
		}
		p.mu.Lock()
		if !p.continueLocked(name) {
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
	}
}

// continueLocked reports whether name's loop goes on, and marks it ended
// when it does not.
func (p *Publisher) continueLocked(name string) bool {
	_, known := p.model.hosts[name]
	if p.observing && known {
		return true
	}
	delete(p.looping, name)
	return false
}

// publishLocked hands frames to every subscriber without waiting for any:
// one whose buffer is full is closed, so it never sees a gap.
// Closing the last subscriber stops observation, as its leaving would.
func (p *Publisher) publishLocked(frames []Frame) {
	dropped := false
	for _, f := range frames {
		for sub := range p.subs {
			select {
			case sub.frames <- f:
			default:
				delete(p.subs, sub)
				close(sub.frames)
				dropped = true
			}
		}
	}
	if dropped && len(p.subs) == 0 && p.observing {
		p.observing = false
		close(p.stop)
	}
}
