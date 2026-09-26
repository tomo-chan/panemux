package tasks

import (
	"encoding/json"
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
)

// Location is where a task's agent process runs on its host.
type Location struct {
	Kind        LocationKind `json:"kind"`
	TmuxSession string       `json:"tmux_session,omitempty"`
	// Attachable reports whether a tmux / ssh_tmux pane can attach to
	// TmuxSession: pane configs accept only validTmuxSessionName.
	Attachable bool `json:"attachable"`
}

// Task is one agent session on one host.
type Task struct {
	StatusSince *time.Time `json:"status_since,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	// Host is the ssh_connections key, or "" for the panemux host itself.
	Host       string   `json:"host"`
	ID         string   `json:"id"`
	Agent      string   `json:"agent"`
	SessionID  string   `json:"session_id,omitempty"`
	CWD        string   `json:"cwd,omitempty"`
	State      State    `json:"state"`
	WaitingFor string   `json:"waiting_for,omitempty"`
	Location   Location `json:"location"`
	PID        int      `json:"pid,omitempty"`
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

// buildTasks turns one host's raw collection into tasks. host is "" for the
// panemux host. collectedAt is panemux's own clock when the output arrived:
// every time a host reports is converted by its age against the host's own
// clock (raw.Now), so a host whose clock is off does not shift the dashboard.
func buildTasks(host string, raw rawSnapshot, collectedAt time.Time) []Task {
	b := taskBuilder{
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

	live := b.liveClaudeTasks()
	live = append(live, b.unexplainedClaudeTasks(live)...)
	live = append(live, b.codexTasks()...)
	//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — ids are unique within a host, so no two compare equal
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })

	liveSessions := make(map[string]bool, len(live))
	// A running claude task with no session id (its state file is unreadable
	// or missing) is most likely writing the newest log in its directory;
	// that log is its own, not a second, stopped task.
	claimedLogs := map[string]int{}
	for _, task := range live {
		switch {
		case task.Agent != AgentClaude:
		case task.SessionID != "":
			liveSessions[task.SessionID] = true
		case task.PID > 0 && task.CWD != "":
			claimedLogs[task.CWD]++
		}
	}
	return append(live, b.stoppedTasks(liveSessions, claimedLogs)...)
}

type taskBuilder struct {
	collectedAt time.Time
	procs       map[int]process
	panes       map[int]string
	host        string
	raw         rawSnapshot
}

func (b *taskBuilder) id(agent, key string) string {
	prefix := "local"
	if b.host != "" {
		prefix = "ssh:" + b.host
	}
	return prefix + ":" + agent + ":" + key
}

func (b *taskBuilder) liveClaudeTasks() []Task {
	bySession := map[string]Task{}
	sinceMillis := map[string]int64{}
	var unknown []Task

	for _, file := range b.raw.StateFiles {
		var st claudeState
		if err := json.Unmarshal(file.Data, &st); err != nil || st.PID <= 0 || !validSessionID.MatchString(st.SessionID) {
			if task, ok := b.unreadableStateTask(file, st); ok {
				unknown = append(unknown, task)
			}
			continue
		}

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
		}
		bySession[st.SessionID] = task
	}

	tasks := make([]Task, 0, len(bySession)+len(unknown))
	for _, task := range bySession {
		tasks = append(tasks, task)
	}
	return append(tasks, unknown...)
}

// unreadableStateTask is the task for a state file that is not JSON or lacks
// a pid or a valid session id. It is kept rather than dropped — a file the
// dashboard cannot read is still evidence of an agent, and hiding it would
// make a format change look like every agent had stopped — unless the pid in
// its name (Claude Code names the file <pid>.json) is no longer a claude
// process, which makes it a leftover.
func (b *taskBuilder) unreadableStateTask(file stateFile, st claudeState) (Task, bool) {
	task := Task{
		Host:     b.host,
		ID:       b.id(AgentClaude, "state-file:"+file.Name),
		Agent:    AgentClaude,
		CWD:      st.CWD,
		State:    StateUnknown,
		Location: Location{Kind: LocationNone},
	}
	pid, ok := stateFilePID(file.Name)
	if !ok {
		return task, true
	}
	if !b.isLiveClaude(pid) {
		return Task{}, false
	}
	task.PID = pid
	task.Location = b.locate(pid)
	if cwd := b.raw.ProcessCWDs[pid]; cwd != "" {
		task.CWD = cwd
	}
	return task, true
}

// unexplainedClaudeTasks are running interactive claude processes that no
// state file names, by its content or by its file name. Without them, a
// Claude Code release that moved or stopped writing ~/.claude/sessions would
// show every running agent as stopped.
func (b *taskBuilder) unexplainedClaudeTasks(known []Task) []Task {
	explained := map[int]bool{}
	for _, task := range known {
		explained[task.PID] = true
	}
	for _, file := range b.raw.StateFiles {
		var st claudeState
		//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — no process has pid 0, so marking it explained changes nothing
		if json.Unmarshal(file.Data, &st) == nil && st.PID > 0 {
			explained[st.PID] = true
		}
		if pid, ok := stateFilePID(file.Name); ok {
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

func (b *taskBuilder) codexTasks() []Task {
	var tasks []Task
	for _, p := range b.raw.Processes {
		if !isInteractiveCodex(p.Command) {
			continue
		}
		tasks = append(tasks, Task{
			Host:     b.host,
			ID:       b.id(AgentCodex, "pid-"+strconv.Itoa(p.PID)),
			Agent:    AgentCodex,
			CWD:      b.raw.ProcessCWDs[p.PID],
			State:    StateRun,
			PID:      p.PID,
			Location: b.locate(p.PID),
		})
	}
	return tasks
}

func (b *taskBuilder) stoppedTasks(liveSessions map[string]bool, claimedLogs map[string]int) []Task {
	transcripts := append([]transcript(nil), b.raw.Transcripts...)
	sort.SliceStable(transcripts, func(i, j int) bool { return transcripts[i].ModTime > transcripts[j].ModTime })

	seen := map[string]bool{}
	var tasks []Task
	for _, tr := range transcripts {
		if len(tasks) >= maxStoppedTasks {
			break
		}
		if liveSessions[tr.SessionID] || seen[tr.SessionID] {
			continue
		}
		seen[tr.SessionID] = true
		if claimedLogs[tr.CWD] > 0 {
			claimedLogs[tr.CWD]--
			continue
		}
		tasks = append(tasks, Task{
			Host:        b.host,
			ID:          b.id(AgentClaude, tr.SessionID),
			Agent:       AgentClaude,
			SessionID:   tr.SessionID,
			CWD:         tr.CWD,
			State:       StateStop,
			StatusSince: b.hostMillis(tr.ModTime * 1000),
			Location:    Location{Kind: LocationNone},
		})
	}
	return tasks
}

// locate walks from pid up its parents until one is a tmux pane's process.
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
	return Location{Kind: LocationOutside}
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

func claudeStatusState(status string) State {
	switch status {
	case "busy":
		return StateBusy
	case "waiting":
		return StateWait
	case "idle":
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
	skipValue := false
	for _, arg := range fields[1:] {
		switch {
		case skipValue:
			skipValue = false
		case arg == "--":
			return true
		case strings.HasPrefix(arg, "-"):
			skipValue = codexValueOptions[arg]
		default:
			return !codexNonInteractiveCommands[arg]
		}
	}
	return true
}

func firstNonZero(values ...int64) int64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}
