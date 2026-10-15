package tasks

import (
	"regexp"
	"sort"
	"time"

	"panemux/internal/agent"
)

// State is a task's column on the dashboard.
type State string

// The states the dashboard shows. Only a running process yields busy, wait,
// idle or run; stop means no process is handling the session, which covers a
// host reboot, a crash and a normal exit alike. There is no "done": whether
// a task's work is finished cannot be read off a process (issue #252).
const (
	StateBusy    State = "busy"
	StateWait    State = "wait"
	StateIdle    State = "idle"
	StateRun     State = "run"
	StateUnknown State = "unknown"
	StateStop    State = "stop"
)

// Agent kinds.
const (
	AgentClaude = string(agent.Claude)
	AgentCodex  = string(agent.Codex)
)

// LocationKind says where a task's agent runs.
type LocationKind string

// Where an agent runs. Only a tmux session can be attached to from a pane.
const (
	LocationTmux    LocationKind = "tmux"
	LocationOutside LocationKind = "outside"
	LocationNone    LocationKind = "none"
	// LocationDaemon is a codex session run by codex's shared app-server
	// daemon, which no pane can be told to show (issue #264).
	LocationDaemon LocationKind = "daemon"
)

// Location is where a task's agent process runs on its host.
type Location struct {
	Kind        LocationKind `json:"kind"`
	TmuxSession string       `json:"tmux_session,omitempty"`
	// PaneID is the pane an agent outside tmux was started from, as its
	// PANEMUX_PANE_ID names it. It is only a claim: the browser opens it only
	// when it is a local pane (panemux host) or an ssh pane on the task's
	// connection that the workspaces hold.
	PaneID string `json:"pane_id,omitempty"`
	// Attachable reports whether a tmux / ssh_tmux pane can attach to
	// TmuxSession: pane configs accept only validTmuxSessionName.
	Attachable bool `json:"attachable"`
}

// Task is one agent session on one host.
type Task struct {
	StatusSince *time.Time `json:"status_since,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	// Log is the version of the session's conversation log the collection
	// saw, nil when none was collected. It is what a summary is keyed on,
	// and is not part of the API.
	Log *LogVersion `json:"-"`
	// Host is the ssh_connections key, or "" for the panemux host itself.
	Host       string `json:"host"`
	ID         string `json:"id"`
	Agent      string `json:"agent"`
	SessionID  string `json:"session_id,omitempty"`
	CWD        string `json:"cwd,omitempty"`
	State      State  `json:"state"`
	WaitingFor string `json:"waiting_for,omitempty"`
	// WaitSignature identifies the wait a task in StateWait is in, when the
	// agent recorded when it began; see waitSignature.
	WaitSignature string   `json:"wait_signature,omitempty"`
	Location      Location `json:"location"`
	PID           int      `json:"pid,omitempty"`
}

// LogVersion is one state of a conversation log, as the collection saw it:
// its modification time on the host's clock (Unix seconds) and its size.
// A log that has not changed keeps both, so a summary made from it is still
// current.
type LogVersion struct {
	// File is a validated Codex rollout basename, never a caller-supplied path.
	File    string
	ModTime int64
	Size    int64
}

// maxStoppedTasks caps the stopped sessions listed per host (issue #252:
// the last 7 days, at most 50 per host).
const maxStoppedTasks = 50

// maxParentWalk bounds the walk up the process tree; a real tree is far
// shallower, and the bound also ends a cycle in a malformed `ps` listing.
const maxParentWalk = 64

// validSessionID is the shape a Claude Code session id must have before it
// is used in an id or a file name. It matches internal/session's own
// validClaudeSessionID.
var validSessionID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// validTmuxSessionName is the tmux session name a pane config accepts. It
// matches internal/session's validTmuxSessionName, the guard in front of the
// `tmux new-session -A -s <name>` a pane runs.
var validTmuxSessionName = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// validPaneID is the PANEMUX_PANE_ID value collection keeps. It matches
// internal/session's validPaneEnvID, the only IDs a pane's shell is given.
var validPaneID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// claudeState is the part of ~/.claude/sessions/<pid>.json the dashboard
// reads. Claude Code writes this file itself; it is not a published format
// (see docs/DECISIONLOG.md), so every field is optional here.
type claudeState struct {
	SessionID       string `json:"sessionId"`
	CWD             string `json:"cwd"`
	Status          string `json:"status"`
	WaitingFor      string `json:"waitingFor"`
	PID             int    `json:"pid"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
	UpdatedAt       int64  `json:"updatedAt"`
	StartedAt       int64  `json:"startedAt"`
}

// UnreadableReason is why a Claude Code state file could not be read.
type UnreadableReason string

// Unreadable reasons: the file is not a JSON object, its pid is missing or
// not a positive integer, or its sessionId is missing or not a session ID.
const (
	UnreadableNotJSON          UnreadableReason = "not_json"
	UnreadableInvalidPID       UnreadableReason = "invalid_pid"
	UnreadableInvalidSessionID UnreadableReason = "invalid_session_id"
)

// Bounds, in runes, on the host-supplied text an unreadable state file
// carries to the API and the log.
const (
	maxUnreadableFileName = 128
	maxUnreadableDetail   = 120
)

// UnreadableStateFile is a state file under ~/.claude/sessions that could
// not be read (issue #313). It is not a task; the dashboard shows it as a
// diagnostic so the operator can compare it with what panemux reads.
type UnreadableStateFile struct {
	// Location is where PID runs, present with PID.
	Location *Location        `json:"location,omitempty"`
	File     string           `json:"file"`
	Reason   UnreadableReason `json:"reason"`
	// Detail is what was found: the JSON error, or the offending field's
	// value as written, bounded and without control characters.
	Detail string `json:"detail,omitempty"`
	// name is the file's full name, which tells two files apart when
	// their bounded File is the same.
	name string
	// PID is the live claude process the file name (<pid>.json) names;
	// omitted when the name carries no pid.
	PID int `json:"pid,omitempty"`
}

// buildTasks turns one host's raw collection into tasks. host is "" for the
// panemux host. collectedAt is panemux's own clock when the output arrived:
// every time a host reports is converted by its age against the host's own
// clock (raw.Now), so a host whose clock is off does not shift the dashboard.
func buildTasks(host string, raw rawSnapshot, collectedAt time.Time) []Task {
	tasks, _ := buildTasksWithDiagnostics(host, raw, collectedAt)
	return tasks
}

// buildTasksWithDiagnostics is buildTasks with the host's unreadable state
// files, ordered by file name.
func buildTasksWithDiagnostics(host string, raw rawSnapshot, collectedAt time.Time) ([]Task, []UnreadableStateFile) {
	b := newTaskBuilder(host, raw, collectedAt)
	live := b.liveTasks()
	tasks := b.withLogVersions(append(live, b.stoppedTasks(live)...))
	var unreadable []UnreadableStateFile
	for _, a := range b.agents {
		if d, ok := a.collected.(stateDiagnostics); ok {
			unreadable = append(unreadable, d.unreadableStateFiles(b)...)
		}
	}
	return tasks, unreadable
}

// buildLiveTasks is buildTasks without the stopped sessions and the log
// versions: the running tasks alone, as the attention collection lists them.
func buildLiveTasks(host string, raw rawSnapshot, collectedAt time.Time) []Task {
	return newTaskBuilder(host, raw, collectedAt).liveTasks()
}

// stateDiagnostics is an adapter part that reports the session files it
// could not read.
type stateDiagnostics interface {
	unreadableStateFiles(b *taskBuilder) []UnreadableStateFile
}

type taskBuilder struct {
	collectedAt time.Time
	procs       map[int]process
	panes       map[int]string
	host        string
	// agents are the adapters' prepared parts, in registration order. An
	// adapter the output has no part for gets an empty one.
	agents []builderAgent
	raw    rawSnapshot
}

type builderAgent struct {
	collected agentCollected
	id        agent.ID
}

func newTaskBuilder(host string, raw rawSnapshot, collectedAt time.Time) *taskBuilder {
	b := &taskBuilder{
		host:        host,
		raw:         raw,
		collectedAt: collectedAt,
		procs:       make(map[int]process, len(raw.Processes)),
		panes:       make(map[int]string, len(raw.TmuxPanes)),
	}
	for _, p := range raw.Processes {
		b.procs[p.PID] = p
	}
	for _, p := range raw.TmuxPanes {
		b.panes[p.PanePID] = p.Session
	}
	for _, r := range collectPlan.full.adapters {
		collected, ok := raw.Agents[r.id]
		if !ok {
			collected = r.adapter.newCollected()
		}
		b.agents = append(b.agents, builderAgent{id: r.id, collected: collected.prepared()})
	}
	return b
}

// liveTasks are every adapter's running tasks, ordered by ID.
func (b *taskBuilder) liveTasks() []Task {
	var live []Task
	for _, a := range b.agents {
		live = append(live, a.collected.liveTasks(b)...)
	}
	//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — ids are unique within a host, so no two compare equal
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })
	return live
}

// stoppedTasks are the sessions no live task handles, newest first across
// the agents and at most maxStoppedTasks of them together. Each adapter
// narrows its own logs; among equal times the adapters' registration order,
// and each adapter's own order, are kept.
func (b *taskBuilder) stoppedTasks(live []Task) []Task {
	var candidates []stoppedCandidate
	for _, a := range b.agents {
		var own []Task
		for _, task := range live {
			if task.Agent == string(a.id) {
				own = append(own, task)
			}
		}
		candidates = append(candidates, a.collected.stoppedCandidates(b, own)...)
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].modTime > candidates[j].modTime })
	if len(candidates) > maxStoppedTasks {
		candidates = candidates[:maxStoppedTasks]
	}
	tasks := make([]Task, 0, len(candidates))
	for _, c := range candidates {
		tasks = append(tasks, Task{
			Host:        b.host,
			ID:          b.id(c.agent, c.sessionID),
			Agent:       c.agent,
			SessionID:   c.sessionID,
			CWD:         c.cwd,
			State:       StateStop,
			StatusSince: b.hostMillis(c.modTime * 1000),
			Location:    Location{Kind: LocationNone},
		})
	}
	return tasks
}

// stoppedCandidate is a conversation log that may be listed as a stopped
// task.
type stoppedCandidate struct {
	agent, sessionID, cwd string
	modTime               int64
}

// withLogVersions lets each task's adapter supply the version of its log.
func (b *taskBuilder) withLogVersions(tasks []Task) []Task {
	for i := range tasks {
		for _, a := range b.agents {
			if tasks[i].Agent == string(a.id) {
				a.collected.addLogVersion(&tasks[i])
			}
		}
	}
	return tasks
}

// Key is the task's session key; it is not Valid while the session ID is
// unknown.
func (t Task) Key() agent.SessionKey {
	return agent.SessionKey{Host: t.Host, Agent: agent.ID(t.Agent), SessionID: t.SessionID}
}

func (b *taskBuilder) id(agent, key string) string {
	return taskID(b.host, agent, key)
}

// locate walks from pid up its parents until one is a tmux pane's process.
// An agent outside tmux carries the pane its own environment names; an
// ancestor's is not used, as the agent is what inherited it from the pane.
func (b *taskBuilder) locate(pid int) Location {
	seen := map[int]bool{}
	for current, steps := pid, 0; steps < maxParentWalk; steps++ {
		//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — parseProcessRow and leadingPID drop pid 0, so neither map has it
		if current <= 0 || seen[current] {
			break
		}
		seen[current] = true
		if name, ok := b.panes[current]; ok {
			return Location{
				Kind:        LocationTmux,
				TmuxSession: name,
				Attachable:  validTmuxSessionName.MatchString(name),
			}
		}
		proc, ok := b.procs[current]
		if !ok {
			break
		}
		current = proc.PPID
	}
	return Location{Kind: LocationOutside, PaneID: b.raw.PaneIDs[pid]}
}

// hostMillis converts a host timestamp (Unix milliseconds on the host's own
// clock) to panemux's clock by its age. Zero means "not reported". A
// timestamp ahead of the host's clock is treated as "just now".
func (b *taskBuilder) hostMillis(millis int64) *time.Time {
	if millis <= 0 {
		return nil
	}
	age := time.Duration(b.raw.Now*1000-millis) * time.Millisecond
	//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — at zero the clamp assigns the value it already holds
	if age < 0 {
		age = 0
	}
	t := b.collectedAt.Add(-age)
	return &t
}

func firstNonZero(values ...int64) int64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}
