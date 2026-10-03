// Package taskevents is the task event publisher (issue #293): it observes
// the running tasks on every host through internal/tasks while the task
// event stream has a subscriber, and publishes every change in their state
// as an ordered frame. It only observes and publishes; what a change means
// is each receiver's decision. docs/behavior/task-events.md is the
// specification.
package taskevents

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"panemux/internal/tasks"
)

// This file is what the stream publishes, and the model it is derived from:
// what was last observed on each host, and the stream position.

// HostPending is a host whose observation has started and that has not
// answered yet. Only the task event stream uses it.
const HostPending tasks.HostStatus = "pending"

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
	StatusSince *time.Time     `json:"status_since,omitempty"`
	ID          string         `json:"id"`
	Host        string         `json:"host"`
	Agent       string         `json:"agent"`
	SessionID   string         `json:"session_id,omitempty"`
	CWD         string         `json:"cwd,omitempty"`
	State       tasks.State    `json:"state"`
	WaitingFor  string         `json:"waiting_for,omitempty"`
	WaitID      string         `json:"wait_id,omitempty"`
	Location    tasks.Location `json:"location"`
}

// HostView is what the stream publishes about one host.
type HostView struct {
	Name   string           `json:"name"`
	Status tasks.HostStatus `json:"status"`
	Error  string           `json:"error,omitempty"`
}

// Frame is one frame of the task event stream.
type Frame struct {
	// Task, Host, Op and PrevState are a task or host frame's.
	Task      *TaskView
	Host      *HostView
	Type      FrameType
	Epoch     string
	Op        FrameOp
	PrevState tasks.State
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
	Task      *TaskView   `json:"task,omitempty"`
	Host      *HostView   `json:"host,omitempty"`
	Type      FrameType   `json:"type"`
	Epoch     string      `json:"epoch"`
	Op        FrameOp     `json:"op"`
	PrevState tasks.State `json:"prev_state,omitempty"`
	Seq       uint64      `json:"seq"`
}

// MarshalJSON writes the fields of the frame's type only.
func (f Frame) MarshalJSON() ([]byte, error) {
	var v any
	if f.Type == FrameSnapshot {
		hosts, views := f.Hosts, f.Tasks
		if hosts == nil {
			hosts = []HostView{}
		}
		if views == nil {
			views = []TaskView{}
		}
		v = snapshotFrameJSON{Type: f.Type, Epoch: f.Epoch, Seq: f.Seq, Hosts: hosts, Tasks: views}
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

func (m *eventModel) taskFrame(op FrameOp, prev tasks.State, view TaskView) Frame {
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
func (m *eventModel) applyHost(name string, result tasks.HostResult, observed []tasks.Task) []Frame {
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
	if result.Status != tasks.HostOK {
		return frames
	}

	current := make(map[string]tasks.Task, len(observed))
	for _, task := range observed {
		if task.State == tasks.StateStop {
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
func (m *eventModel) view(task tasks.Task, prev TaskView, known bool) TaskView {
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
	if task.State != tasks.StateWait {
		return v
	}
	v.WaitingFor = task.WaitingFor
	switch {
	case task.WaitSignature != "":
		v.WaitID = task.WaitSignature
	case known && prev.State == tasks.StateWait && prev.WaitingFor == task.WaitingFor && isUnsignedWaitID(prev.WaitID):
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

func sortedTaskIDs(views map[string]TaskView) []string {
	ids := make([]string, 0, len(views))
	for id := range views {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
