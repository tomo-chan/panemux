package tasks

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Codex tasks (issue #264). Codex writes no file that says which process
// runs which session, or in what state: a codex process's session is the
// rollout (~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<session ID>.jsonl) it
// holds open, and its state is the newest turn of that session — from
// ~/.codex/thread_history_1.sqlite's thread_turns (codex-cli 0.157 and
// later), or else from the last turn event in the rollout — taken together
// with the process being alive. Checked on real codex-cli 0.142.2 and 0.157.1
// (macOS) and 0.157.1 (Linux); see docs/behavior/tasks.md.

// codexRollout is one rollout the collection found: its session ID (from the
// file name), the cwd and source of its session_meta line, and the file's
// modification time and size.
type codexRollout struct {
	SessionID string
	CWD       string
	Source    string
	ModTime   int64
	Size      int64
}

// codexOpenRollout is a rollout a codex process holds open.
type codexOpenRollout struct {
	// File is the rollout's base name, which starts with its creation time.
	File    string
	Turn    codexTurn
	Rollout codexRollout
	PID     int
	// Elapsed is how long the process has run, in seconds; -1 when ps did
	// not say.
	Elapsed int64
}

// codexTurn is what the collection read about a session's newest turn.
type codexTurn struct {
	// DBStatus and DBStartedAt are the newest thread_turns row (Unix
	// seconds), empty and 0 without one.
	DBStatus string
	// Event is the last task_started, task_complete or turn_aborted in the
	// rollout's tail, and EventAt its time (Unix milliseconds, 0 unknown).
	Event string
	// LastItem is the payload type of the last response_item in the tail,
	// and LastItemName the name it calls, for a call.
	LastItem     string
	LastItemName string
	DBStartedAt  int64
	EventAt      int64
}

// codexSourceInteractive is the session_meta source of a session the TUI
// started. codex exec, the desktop app and subagents write rollouts too.
const codexSourceInteractive = "cli"

// What codex records about a turn: thread_turns statuses, rollout events,
// and the response item and tool that ask the person a question.
const (
	codexTurnInProgress   = "inProgress"
	codexTurnCompleted    = "completed"
	codexTurnInterrupted  = "interrupted"
	codexTurnFailed       = "failed"
	codexEventStarted     = "task_started"
	codexEventComplete    = "task_complete"
	codexEventAborted     = "turn_aborted"
	codexItemFunctionCall = "function_call"
	codexToolAskUser      = "request_user_input"
)

// codexWaitingForQuestion is the reason shown for a turn waiting on
// request_user_input, codex's tool for asking the person a question.
const codexWaitingForQuestion = "a question from codex"

// codexLeftoverMargin is how much earlier than its process's start (which
// ps gives to the second) a turn must have started to be taken as a leftover
// of a process that no longer runs.
const codexLeftoverMargin = 2

var (
	rolloutFileName = regexp.MustCompile(`^rollout-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}-(.+)\.jsonl$`)
	codexTurnEvent  = regexp.MustCompile(`"payload":\{"type":"(task_started|task_complete|turn_aborted)"`)
	codexItemType   = regexp.MustCompile(`"payload":\{"type":"([a-z_]+)"`)
	codexItemName   = regexp.MustCompile(`"name":"([A-Za-z0-9_.-]+)"`)
	codexTimestamp  = regexp.MustCompile(`"timestamp":"([^"]+)"`)
)

// rolloutSessionID reads the session ID out of a rollout's file name. Only a
// UUID is accepted: the ID is what `codex resume` is given.
func rolloutSessionID(name string) (string, bool) {
	m := rolloutFileName.FindStringSubmatch(name)
	if m == nil || !validUUID.MatchString(m[1]) {
		return "", false
	}
	return m[1], true
}

// parseEtime reads ps's elapsed time, [[dd-]hh:]mm:ss, as seconds; -1 when it
// is not in that form.
func parseEtime(s string) int64 {
	s = strings.TrimSpace(s)
	var days int64
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, ok := nonNegative(d)
		if !ok {
			return -1
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return -1
	}
	var total int64
	for _, part := range parts {
		n, ok := nonNegative(part)
		if !ok {
			return -1
		}
		total = total*60 + n
	}
	return days*86400 + total
}

func nonNegative(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n >= 0 && !strings.HasPrefix(s, "+")
}

// parseCodexAgeRow reads a codex-open row that gives only a process's age.
func parseCodexAgeRow(line string) (int, int64, bool) {
	parts := strings.Split(line, "\t")
	if len(parts) != 2 {
		return 0, 0, false
	}
	pid, err := strconv.Atoi(parts[0])
	if err != nil || pid <= 0 {
		return 0, 0, false
	}
	age := parseEtime(parts[1])
	return pid, age, age >= 0
}

// parseCodexOpenRow reads one codex-open row (see collectScript).
func parseCodexOpenRow(line string) (codexOpenRollout, bool) {
	parts := strings.SplitN(line, "\t", 8)
	if len(parts) < 8 {
		return codexOpenRollout{}, false
	}
	pid, err := strconv.Atoi(parts[0])
	if err != nil || pid <= 0 {
		return codexOpenRollout{}, false
	}
	stamp := strings.Fields(parts[2])
	if len(stamp) != 2 {
		return codexOpenRollout{}, false
	}
	modTime, err1 := strconv.ParseInt(stamp[0], 10, 64)
	size, err2 := strconv.ParseInt(stamp[1], 10, 64)
	if err1 != nil || err2 != nil {
		return codexOpenRollout{}, false
	}
	path := parts[7]
	name := path[strings.LastIndex(path, "/")+1:]
	sessionID, ok := rolloutSessionID(name)
	if !ok {
		return codexOpenRollout{}, false
	}
	return codexOpenRollout{
		PID:     pid,
		Elapsed: parseEtime(parts[1]),
		File:    name,
		Rollout: codexRollout{SessionID: sessionID, CWD: transcriptCWD(parts[6]), ModTime: modTime, Size: size},
		Turn:    parseCodexTurn(parts[3], parts[4], parts[5]),
	}, true
}

// parseCodexRolloutRow reads one codex-rollouts row (see collectScript).
func parseCodexRolloutRow(line string) (codexRollout, bool) {
	parts := strings.SplitN(line, "\t", 5)
	if len(parts) < 3 {
		return codexRollout{}, false
	}
	modTime, err1 := strconv.ParseInt(parts[0], 10, 64)
	size, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return codexRollout{}, false
	}
	sessionID, ok := rolloutSessionID(parts[2])
	if !ok {
		return codexRollout{}, false
	}
	r := codexRollout{SessionID: sessionID, ModTime: modTime, Size: size}
	if len(parts) >= 4 {
		r.CWD = transcriptCWD(parts[3])
	}
	if len(parts) == 5 {
		r.Source = jsonFragmentString(parts[4], `"source":`)
	}
	return r, true
}

// jsonFragmentString decodes the string of a `"key":"..."` fragment grep
// found, or "".
func jsonFragmentString(fragment, prefix string) string {
	value, ok := strings.CutPrefix(fragment, prefix)
	if !ok {
		return ""
	}
	s, err := strconv.Unquote(value)
	if err != nil {
		return ""
	}
	return s
}

// parseCodexTurn reads the thread_turns row ("<status> <started_at>") and the
// two rollout lines of a codex-open row. The lines are cut at 512 bytes, so
// they are searched, not decoded.
func parseCodexTurn(db, event, item string) codexTurn {
	var turn codexTurn
	// sqlite3 prints "<status> <started_at>"; anything else is not its answer.
	if status, started, ok := strings.Cut(db, " "); ok && status != "" {
		turn.DBStatus = status
		if n, err := strconv.ParseInt(started, 10, 64); err == nil {
			turn.DBStartedAt = n
		}
	}
	if m := codexTurnEvent.FindStringSubmatch(event); m != nil {
		turn.Event = m[1]
		if ts := codexTimestamp.FindStringSubmatch(event); ts != nil {
			if at, err := time.Parse(time.RFC3339Nano, ts[1]); err == nil {
				turn.EventAt = at.UnixMilli()
			}
		}
	}
	if m := codexItemType.FindStringSubmatch(item); m != nil {
		turn.LastItem = m[1]
		if strings.HasSuffix(m[1], "_call") {
			if name := codexItemName.FindStringSubmatch(item); name != nil {
				turn.LastItemName = name[1]
			}
		}
	}
	return turn
}

// codexTasks are the interactive codex processes: each in the session whose
// rollout it wrote last, or known only by its pid before it has one.
func (b *taskBuilder) codexTasks() []Task {
	current := map[int]codexOpenRollout{}
	for _, o := range b.raw.CodexOpen {
		prev, seen := current[o.PID]
		// An equal name at an equal time is one file held on two descriptors,
		// whose rows are alike in every field.
		newer := o.Rollout.ModTime > prev.Rollout.ModTime ||
			//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — replaces a row with an identical one
			(o.Rollout.ModTime == prev.Rollout.ModTime && o.File > prev.File)
		if !seen || newer {
			current[o.PID] = o
		}
	}

	var tasks []Task
	sessions := map[string]bool{}
	for _, p := range b.raw.Processes {
		if !isInteractiveCodex(p.Command) {
			continue
		}
		o, ok := current[p.PID]
		if ok && sessions[o.Rollout.SessionID] {
			continue
		}
		task := Task{
			Host:     b.host,
			Agent:    AgentCodex,
			CWD:      b.raw.ProcessCWDs[p.PID],
			PID:      p.PID,
			Location: b.locate(p.PID),
		}
		elapsed, known := b.raw.CodexAges[p.PID]
		if ok {
			elapsed, known = o.Elapsed, o.Elapsed >= 0
		}
		if known {
			task.StartedAt = b.hostMillis((b.raw.Now - elapsed) * 1000)
		}
		if !ok {
			task.ID = b.id(AgentCodex, "pid-"+strconv.Itoa(p.PID))
			task.State = StateRun
			tasks = append(tasks, task)
			continue
		}
		sessions[o.Rollout.SessionID] = true
		task.ID = b.id(AgentCodex, o.Rollout.SessionID)
		task.SessionID = o.Rollout.SessionID
		if o.Rollout.CWD != "" {
			task.CWD = o.Rollout.CWD
		}
		b.codexState(&task, o)
		tasks = append(tasks, task)
	}
	return tasks
}

// codexState sets a running codex task's state from its session's newest
// turn. thread_turns is read when it has a status this build knows; else the
// rollout's last turn event. A turn that started before the process did is
// left over from a codex that was killed mid-turn — such a turn stays
// inProgress for good, and resuming the session writes no new turn until it
// is given one — so the session is idle.
func (b *taskBuilder) codexState(task *Task, o codexOpenRollout) {
	inProgress, finished, startedMillis := codexTurnStatus(o.Turn)
	modTime := b.hostMillis(o.Rollout.ModTime * 1000)
	switch {
	case inProgress && o.Elapsed >= 0 && startedMillis > 0 &&
		startedMillis < (b.raw.Now-o.Elapsed-codexLeftoverMargin)*1000:
		task.State, task.StatusSince = StateIdle, modTime
	case inProgress && o.Turn.LastItem == codexItemFunctionCall && o.Turn.LastItemName == codexToolAskUser:
		task.State, task.StatusSince = StateWait, modTime
		task.WaitingFor = codexWaitingForQuestion
	case inProgress:
		task.State, task.StatusSince = StateBusy, b.hostMillis(startedMillis)
	case finished:
		task.State, task.StatusSince = StateIdle, modTime
	default:
		task.State = StateUnknown
	}
}

// codexTurnStatus reads whether the newest turn is in progress or finished,
// and when it started (Unix milliseconds, 0 unknown). Neither is set when
// nothing says.
func codexTurnStatus(turn codexTurn) (inProgress, finished bool, startedMillis int64) {
	switch turn.DBStatus {
	case codexTurnInProgress:
		return true, false, turn.DBStartedAt * 1000
	case codexTurnCompleted, codexTurnInterrupted, codexTurnFailed:
		return false, true, 0
	}
	switch turn.Event {
	case codexEventStarted:
		return true, false, turn.EventAt
	case codexEventComplete, codexEventAborted:
		return false, true, 0
	}
	return false, false, 0
}

// heldCodexSessions is every session whose rollout a live codex process
// holds open, current or not: none of them is stopped.
func (b *taskBuilder) heldCodexSessions() map[string]bool {
	held := map[string]bool{}
	for _, o := range b.raw.CodexOpen {
		if proc, alive := b.procs[o.PID]; alive && isCodexProgram(proc.Command) {
			held[o.Rollout.SessionID] = true
		}
	}
	return held
}

// stoppedCandidate is a conversation log that may be listed as a stopped
// task: a claude transcript or an interactive codex rollout.
type stoppedCandidate struct {
	agent, sessionID, cwd string
	modTime               int64
}

// stoppedCandidates merges claude's transcripts and codex's interactive
// rollouts, newest first, keeping the host's order among equal times.
func (b *taskBuilder) stoppedCandidates() []stoppedCandidate {
	candidates := make([]stoppedCandidate, 0, len(b.raw.Transcripts)+len(b.raw.CodexRollouts))
	for _, tr := range b.raw.Transcripts {
		candidates = append(candidates, stoppedCandidate{
			agent: AgentClaude, sessionID: tr.SessionID, cwd: tr.CWD, modTime: tr.ModTime,
		})
	}
	for _, r := range b.raw.CodexRollouts {
		if r.Source != codexSourceInteractive {
			continue
		}
		candidates = append(candidates, stoppedCandidate{
			agent: AgentCodex, sessionID: r.SessionID, cwd: r.CWD, modTime: r.ModTime,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].modTime > candidates[j].modTime })
	return candidates
}

// isCodexProgram is any codex process, interactive or not.
func isCodexProgram(command string) bool {
	fields := strings.Fields(command)
	//mutation:exempt[CONDITIONALS_BOUNDARY] unreachable — parseProcessRow keeps only rows that have a command
	return len(fields) > 0 && strings.ToLower(fieldBase(fields[0])) == AgentCodex
}

func fieldBase(program string) string {
	return program[strings.LastIndex(program, "/")+1:]
}
