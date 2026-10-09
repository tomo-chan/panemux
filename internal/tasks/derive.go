package tasks

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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
	AgentClaude = "claude"
	AgentCodex  = "codex"
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

	liveSessions := map[string]map[string]bool{
		AgentClaude: {},
		AgentCodex:  b.heldCodexSessions(),
	}
	// A running claude task with no session id (its state file is unreadable
	// or missing) is most likely writing the newest log in its directory;
	// that log is its own, not a second, stopped task.
	claimedLogs := map[string]int{}
	for _, task := range live {
		switch {
		case task.Agent != AgentClaude:
		case task.SessionID != "":
			liveSessions[AgentClaude][task.SessionID] = true
		case task.PID > 0 && task.CWD != "":
			claimedLogs[task.CWD]++
		}
	}
	return b.withLogVersions(append(live, b.stoppedTasks(liveSessions, claimedLogs)...)), b.unreadableStateFiles()
}

// buildLiveTasks is buildTasks without the stopped sessions and the log
// versions: the running tasks alone, as the attention collection lists them.
func buildLiveTasks(host string, raw rawSnapshot, collectedAt time.Time) []Task {
	b := newTaskBuilder(host, raw, collectedAt)
	return b.liveTasks()
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
	for _, file := range raw.StateFiles {
		b.states = append(b.states, readStateFile(file))
	}
	for _, p := range raw.TmuxPanes {
		b.panes[p.PanePID] = p.Session
	}
	return b
}

// liveTasks are the running claude and codex tasks, ordered by ID.
func (b *taskBuilder) liveTasks() []Task {
	live := b.liveClaudeTasks()
	live = append(live, b.unexplainedClaudeTasks(live)...)
	live = append(live, b.codexTasks()...)
	//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — ids are unique within a host, so no two compare equal
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })
	return live
}

// withLogVersions gives every claude task with a session ID the version of
// its newest collected conversation log.
func (b *taskBuilder) withLogVersions(tasks []Task) []Task {
	newest := make(map[string]transcript, len(b.raw.Transcripts))
	for _, tr := range b.raw.Transcripts {
		if prev, ok := newest[tr.SessionID]; !ok || tr.ModTime > prev.ModTime {
			newest[tr.SessionID] = tr
		}
	}
	for i := range tasks {
		if tasks[i].Agent != AgentClaude || tasks[i].SessionID == "" {
			continue
		}
		if tr, ok := newest[tasks[i].SessionID]; ok {
			tasks[i].Log = &LogVersion{ModTime: tr.ModTime, Size: tr.Size}
		}
	}
	return tasks
}

type taskBuilder struct {
	collectedAt time.Time
	states      []readState
	procs       map[int]process
	panes       map[int]string
	host        string
	raw         rawSnapshot
}

func (b *taskBuilder) id(agent, key string) string {
	return taskID(b.host, agent, key)
}

func (b *taskBuilder) liveClaudeTasks() []Task {
	bySession := map[string]Task{}
	sinceMillis := map[string]int64{}

	for _, state := range b.states {
		if state.reason != "" {
			continue
		}
		st := state.st

		if !b.isLiveClaude(st.PID) {
			// A leftover file: its process exited, or its pid now belongs
			// to something else. The session shows up as stopped through
			// its transcript instead.
			continue
		}

		since := firstNonZero(st.StatusUpdatedAt, st.UpdatedAt, st.StartedAt)
		if prev, ok := sinceMillis[st.SessionID]; ok && prev >= since {
			continue
		}
		sinceMillis[st.SessionID] = since

		state := claudeStatusState(st.Status)
		task := Task{
			Host:        b.host,
			ID:          b.id(AgentClaude, st.SessionID),
			Agent:       AgentClaude,
			SessionID:   st.SessionID,
			CWD:         st.CWD,
			State:       state,
			PID:         st.PID,
			StatusSince: b.hostMillis(since),
			StartedAt:   b.hostMillis(st.StartedAt),
			Location:    b.locate(st.PID),
		}
		if state == StateWait {
			task.WaitingFor = st.WaitingFor
			// statusUpdatedAt is when the status became waiting; updatedAt
			// and startedAt are not, so they do not stand in for it.
			task.WaitSignature = waitSignature(b.host, AgentClaude, st.SessionID, st.StatusUpdatedAt, st.WaitingFor)
		}
		bySession[st.SessionID] = task
	}

	tasks := make([]Task, 0, len(bySession))
	for _, task := range bySession {
		tasks = append(tasks, task)
	}
	return tasks
}

// unreadableStateFiles are the state files that could not be read, unless
// the pid in the file's name (Claude Code names it <pid>.json) is no longer
// a claude process, which makes the file a leftover. A file whose name
// carries no pid cannot be checked and is kept.
func (b *taskBuilder) unreadableStateFiles() []UnreadableStateFile {
	var files []UnreadableStateFile
	for _, state := range b.states {
		if state.reason == "" {
			continue
		}
		file := UnreadableStateFile{
			File:   boundedText(state.name, maxUnreadableFileName),
			Reason: state.reason,
			Detail: boundedText(state.detail, maxUnreadableDetail),
		}
		if pid, ok := stateFilePID(state.name); ok {
			if !b.isLiveClaude(pid) {
				continue
			}
			loc := b.locate(pid)
			file.PID, file.Location = pid, &loc
		}
		files = append(files, file)
	}
	//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — names in one directory are unique, so no two compare equal
	sort.Slice(files, func(i, j int) bool { return files[i].File < files[j].File })
	return files
}

// unexplainedClaudeTasks are running interactive claude processes that no
// readable state file names, by its content or by its file name. Without
// them, a Claude Code release that moved, stopped writing or changed
// ~/.claude/sessions would show every running agent as stopped.
func (b *taskBuilder) unexplainedClaudeTasks(known []Task) []Task {
	explained := map[int]bool{}
	for _, task := range known {
		explained[task.PID] = true
	}
	for _, state := range b.states {
		if state.reason != "" {
			continue
		}
		explained[state.st.PID] = true
		if pid, ok := stateFilePID(state.name); ok {
			explained[pid] = true
		}
	}

	var tasks []Task
	for _, p := range b.raw.Processes {
		if explained[p.PID] || !isInteractiveClaude(p.Command) {
			continue
		}
		tasks = append(tasks, Task{
			Host:     b.host,
			ID:       b.id(AgentClaude, "pid-"+strconv.Itoa(p.PID)),
			Agent:    AgentClaude,
			CWD:      b.raw.ProcessCWDs[p.PID],
			State:    StateUnknown,
			PID:      p.PID,
			Location: b.locate(p.PID),
		})
	}
	return tasks
}

func (b *taskBuilder) isLiveClaude(pid int) bool {
	proc, alive := b.procs[pid]
	return alive && isClaudeProcess(proc.Command)
}

// stateFilePID reads the pid out of a state file named <pid>.json.
func stateFilePID(name string) (int, bool) {
	digits, ok := strings.CutSuffix(name, ".json")
	if !ok {
		return 0, false
	}
	pid, err := strconv.Atoi(digits)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// stoppedTasks are the claude and codex sessions no live process handles,
// newest first and at most maxStoppedTasks of them for the two agents
// together. liveSessions holds, per agent, the sessions a live process
// handles; for codex that is every session a codex process holds open.
func (b *taskBuilder) stoppedTasks(liveSessions map[string]map[string]bool, claimedLogs map[string]int) []Task {
	seen := map[string]map[string]bool{AgentClaude: {}, AgentCodex: {}}
	var tasks []Task
	for _, c := range b.stoppedCandidates() {
		if len(tasks) >= maxStoppedTasks {
			break
		}
		if liveSessions[c.agent][c.sessionID] || seen[c.agent][c.sessionID] {
			continue
		}
		seen[c.agent][c.sessionID] = true
		if c.agent == AgentClaude && claimedLogs[c.cwd] > 0 {
			claimedLogs[c.cwd]--
			continue
		}
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

// The statuses a Claude Code state file reports.
const (
	claudeStatusBusy    = "busy"
	claudeStatusWaiting = "waiting"
	claudeStatusIdle    = "idle"
)

func claudeStatusState(status string) State {
	switch status {
	case claudeStatusBusy:
		return StateBusy
	case claudeStatusWaiting:
		return StateWait
	case claudeStatusIdle:
		return StateIdle
	default:
		return StateUnknown
	}
}

// isClaudeProcess reports whether a `ps` command line is a Claude Code
// process: the program itself — argv[0], or the script a node, bun or deno
// interpreter runs — has a path component named claude or claude-code. How
// Claude Code shows in `ps` depends on how it was installed: a native binary
// under .../claude/versions/<version> or named claude, or node running
// .../@anthropic-ai/claude-code/cli.js. Only the program is looked at, so a
// process that merely has a file under ~/.claude as an argument (an editor,
// `tail`, a hook) does not pass for one.
func isClaudeProcess(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	if namesClaude(fields[0]) {
		return true
	}
	return len(fields) > 1 && scriptInterpreters[strings.ToLower(filepath.Base(fields[0]))] && namesClaude(fields[1])
}

var scriptInterpreters = map[string]bool{"node": true, "bun": true, "deno": true}

func namesClaude(program string) bool {
	for _, part := range strings.Split(strings.ToLower(program), "/") {
		if part == AgentClaude || part == "claude-code" {
			return true
		}
	}
	return false
}

// isInteractiveClaude is a claude process that is not `claude -p` /
// `claude --print`, which answer one prompt and exit.
func isInteractiveClaude(command string) bool {
	if !isClaudeProcess(command) {
		return false
	}
	for _, field := range strings.Fields(command)[1:] {
		if field == "-p" || field == "--print" {
			return false
		}
	}
	return true
}

// codexNonInteractiveCommands are codex-cli's subcommands that do not open
// an interactive session, from `codex --help` of codex-cli 0.157.0, plus
// mcp-server from earlier releases. With no subcommand codex opens the TUI
// (the first positional argument is then a prompt), and `resume` and `fork`
// reopen an interactive session, so neither appears here.
var codexNonInteractiveCommands = map[string]bool{
	"agents": true, "exec": true, "e": true, "review": true, "login": true, "logout": true,
	"mcp": true, "mcp-server": true, "plugin": true, "app-server": true, "remote-control": true,
	"completion": true, "update": true, "doctor": true, "sandbox": true, "debug": true,
	"apply": true, "a": true, "queue": true, "archive": true, "delete": true,
	"migrate-rollouts": true, "unarchive": true, "cloud": true, "exec-server": true,
	"features": true, "help": true,
}

// codexValueOptions are the top-level options of codex-cli 0.157.0 that take
// a value in the next argument, so the value is not mistaken for a subcommand.
var codexValueOptions = map[string]bool{
	"-c": true, "--config": true, "--enable": true, "--disable": true, "--remote": true,
	"--remote-auth-token-env": true, "-i": true, "--image": true, "-m": true, "--model": true,
	"--local-provider": true, "-p": true, "--profile": true, "-s": true, "--sandbox": true,
	"-C": true, "--cd": true, "--add-dir": true, "-a": true, "--ask-for-approval": true,
}

// isInteractiveCodex reports whether a `ps` command line is an interactive
// codex session: argv[0]'s base name is codex, and the first positional
// argument is not a non-interactive subcommand. Only that argument is looked
// at, so a prompt that happens to contain "exec" is still a session.
func isInteractiveCodex(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 || strings.ToLower(filepath.Base(fields[0])) != AgentCodex {
		return false
	}
	return !codexNonInteractiveCommands[firstCodexPositional(fields[1:])]
}

// firstCodexPositional is codex's first positional argument — its
// subcommand, or a prompt — with the values of options skipped; "" when
// there is none or it follows "--".
func firstCodexPositional(args []string) string {
	skipValue := false
	for _, arg := range args {
		switch {
		case skipValue:
			skipValue = false
		case arg == "--":
			return ""
		case strings.HasPrefix(arg, "-"):
			skipValue = codexValueOptions[arg]
		default:
			return arg
		}
	}
	return ""
}

func firstNonZero(values ...int64) int64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}
