package tasks

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"panemux/internal/agent"
)

// Codex tasks (issue #264). Codex writes no file that says which process
// runs which session, or in what state: a codex process's session is the
// rollout (~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<session ID>.jsonl) it
// holds open, and its state is the newest turn of that session — from
// ~/.codex/thread_history_1.sqlite's thread_turns (codex-cli 0.157 and
// later), or else from the last turn event in the rollout — taken together
// with the process being alive. Checked on real codex-cli 0.142.2 and 0.157.1
// (macOS) and 0.157.1 (Linux); see docs/behavior/tasks.md.

// codexCollect is Codex's collect adapter. Its live section is the rollouts
// codex processes hold open, and its stopped section the rollouts under
// ~/.codex/sessions (issue #264).
type codexCollect struct{}

// Codex's sections, within the "codex" namespace.
const (
	codexSectionOpen     = "open"
	codexSectionRollouts = "rollouts"
)

// codexProgram is the base name of codex's executable, which the open
// section looks for among the processes.
const codexProgram = string(agent.Codex)

func (codexCollect) sections() []collectSection {
	return []collectSection{
		{name: codexSectionOpen, stage: stageLive, script: codexOpenScript},
		{name: codexSectionRollouts, stage: stageStopped, script: codexRolloutsScript},
	}
}

func (codexCollect) processHints() []string { return []string{codexProgram} }

func (codexCollect) newCollected() agentCollected { return &codexCollected{} }

// codexOpenScript lists the open rollouts. Codex writes no file that ties a process to its session (issue #264): the
// rollout a codex process holds open is its session, found through
// /proc/<pid>/fd, or lsof where there is no /proc. Each codex process gets a
// "<pid>\t<etime>" row, then one row per rollout it holds open:
//
//	<pid>\t<etime>\t<mtime> <size>\t<status> <started_at>\t<turn event line>\t<response item line>\t<"cwd":"...">
//	\t<"originator":"...">\t<path>        (one line)
//
// where etime is how long the process has run (ps's [[dd-]hh:]mm:ss), status
// and started_at are the session's newest thread_turns row in
// ~/.codex/thread_history_1.sqlite when sqlite3 can read one, and the two
// lines are the first 512 bytes of the last turn event (task_started,
// task_complete, turn_aborted) and the last response item in the rollout's
// final MiB. Only a session ID made of hex digits and dashes — the shape of
// the UUID in the file name — is put into the query.
const codexOpenScript = `db="$HOME/.codex/thread_history_1.sqlite"
ps -U "$uid" -o pid=,etime=,command= 2>/dev/null |
awk '{ n = $3; sub(/.*\//, "", n); if (n == "` + codexProgram + `") print $1, $2 }' |
while read -r pid et; do
	printf '%s\t%s\n' "$pid" "$et"
	if [ -d "/proc/$pid/fd" ]; then
		for fd in "/proc/$pid/fd"/*; do readlink "$fd"; done 2>/dev/null
	else
		lsof -p "$pid" -Fn 2>/dev/null | sed -n 's/^n//p'
	fi | while IFS= read -r p; do
		case ${p##*/} in rollout-*.jsonl) ;; *) continue ;; esac
		t=$(mtime "$p" 2>/dev/null) || continue
		id=${p##*/}
		id=${id%.jsonl}
		id=${id#rollout-????-??-??T??-??-??-}
		case $id in *[!0-9a-f-]*|'') id= ;; esac
		st=
		if [ -n "$id" ] && [ -f "$db" ]; then
			q="select status || ' ' || coalesce(started_at, '') from thread_turns"
			q="$q where thread_id = '$id' order by rollout_ordinal desc limit 1"
			st=$(sqlite3 -readonly "$db" "$q" </dev/null 2>/dev/null | head -n 1)
		fi
		marks=$(tail -c 1048576 "$p" 2>/dev/null | awk '
			/"payload":[{]"type":"(task_started|task_complete|turn_aborted)"/ { ev = substr($0, 1, 512) }
			/"type":"response_item"/ { ri = substr($0, 1, 512) }
			END { printf "%s\t%s", ev, ri }')
		l=$(head -n 1 "$p" 2>/dev/null)
		c=$(printf '%s\n' "$l" | grep -o '"cwd":"[^"]*"' | head -n 1)
		o=$(printf '%s\n' "$l" | grep -o '"originator":"[^"]*"' | head -n 1)
		printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$pid" "$et" "$t" "$st" "$marks" "$c" "$o" "$p"
	done
done
`

// codexRolloutsScript prints
// "<mtime>\t<size>\t<file name>\t<"cwd":"...">\t<"originator":"...">" of
// its first line for the rollouts (~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl)
// modified in the last 7 days, newest first, counting only the TUI's
// (originator "codex-tui"): the 100 are taken after that filter, so a host
// where codex exec writes many rollouts does not push the TUI's sessions out.
const codexRolloutsScript = `find "$HOME/.codex/sessions" -mindepth 4 -maxdepth 4 -type f -name 'rollout-*.jsonl' -mtime -7 2>/dev/null |
while IFS= read -r p; do
	t=$(mtime "$p" 2>/dev/null) && echo "$t $p"
done | sort -k1,1nr -k2,2nr -k3r | while read -r t s p; do
	l=$(head -n 1 "$p" 2>/dev/null)
	o=$(printf '%s\n' "$l" | grep -o '"originator":"[^"]*"' | head -n 1)
	[ "$o" = '"originator":"codex-tui"' ] || continue
	c=$(printf '%s\n' "$l" | grep -o '"cwd":"[^"]*"' | head -n 1)
	printf '%s\t%s\t%s\t%s\t%s\n' "$t" "$s" "${p##*/}" "$c" "$o"
done | head -n 100
`

// codexCollected is Codex's part of one host's output.
type codexCollected struct {
	// Ages is how long each codex process has run, in seconds.
	Ages map[int]int64
	// Open is every rollout a codex process holds open, and Rollouts the
	// rollouts under ~/.codex/sessions.
	Open     []codexOpenRollout
	Rollouts []codexRollout
}

func (c *codexCollected) addRow(section, line string) {
	switch section {
	case codexSectionOpen:
		if pid, age, ok := parseCodexAgeRow(line); ok {
			if c.Ages == nil {
				c.Ages = map[int]int64{}
			}
			c.Ages[pid] = age
		} else if o, ok := parseCodexOpenRow(line); ok {
			c.Open = append(c.Open, o)
		}
	case codexSectionRollouts:
		if r, ok := parseCodexRolloutRow(line); ok {
			c.Rollouts = append(c.Rollouts, r)
		}
	}
}

// addFile is never called: no codex section declares files.
func (c *codexCollected) addFile(string, string, []byte) {}

// prepared orders the rollouts newest first, each open one carrying the
// file name it was found under.
func (c *codexCollected) prepared() agentCollected {
	p := &codexCollected{
		Ages:     c.Ages,
		Open:     append([]codexOpenRollout(nil), c.Open...),
		Rollouts: append([]codexRollout(nil), c.Rollouts...),
	}
	sort.SliceStable(p.Rollouts, func(i, j int) bool { return newerCodexRollout(p.Rollouts[i], p.Rollouts[j]) })
	for i := range p.Open {
		p.Open[i].Rollout.File = p.Open[i].File
	}
	sort.SliceStable(p.Open, func(i, j int) bool { return newerCodexRollout(p.Open[i].Rollout, p.Open[j].Rollout) })
	return p
}

// stoppedCandidates are the interactive rollouts no live codex process
// holds open, newest first.
func (c *codexCollected) stoppedCandidates(b *taskBuilder, _ []Task) []stoppedCandidate {
	held := c.heldSessions(b)
	seen := map[string]bool{}
	var candidates []stoppedCandidate
	for _, r := range c.Rollouts {
		if r.Originator != codexOriginatorTUI || held[r.SessionID] || seen[r.SessionID] {
			continue
		}
		seen[r.SessionID] = true
		candidates = append(candidates, stoppedCandidate{
			agent: AgentCodex, sessionID: r.SessionID, cwd: r.CWD, modTime: r.ModTime,
		})
	}
	return candidates
}

// addLogVersion sets a stopped task's rollout version; a live task already
// carries the exact open rollout it was found from.
func (c *codexCollected) addLogVersion(task *Task) {
	if task.Log != nil {
		return
	}
	for _, r := range c.Rollouts {
		if r.SessionID == task.SessionID && r.Originator == codexOriginatorTUI {
			task.Log = codexLogVersion(r)
			return
		}
	}
}

// codexRollout is one rollout the collection found: its session ID (from the
// file name), the cwd and originator of its session_meta line, and the file's
// modification time and size.
type codexRollout struct {
	File       string
	SessionID  string
	CWD        string
	Originator string
	ModTime    int64
	Size       int64
}

// codexOpenRollout is a rollout a codex process holds open.
type codexOpenRollout struct {
	// File is the rollout's base name, which starts with its creation time.
	File    string
	Rollout codexRollout
	Turn    codexTurn
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
	// LastItemAt is the time the last response_item carries itself (Unix
	// milliseconds, 0 unknown).
	LastItemAt int64
}

// codexOriginatorTUI is the session_meta originator of a session the TUI
// started, whether the TUI wrote the rollout itself (source "cli") or through
// codex's shared app-server daemon (source "vscode"). codex exec
// ("codex_exec") and the desktop app write rollouts too.
const codexOriginatorTUI = "codex-tui"

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
	// codexLineTime is the timestamp a rollout line starts with: its own,
	// not one quoted further in.
	codexLineTime = regexp.MustCompile(`^\{"timestamp":"([^"]+)"`)
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

// parseCodexOpenRow reads one codex/open row (see codexOpenScript).
func parseCodexOpenRow(line string) (codexOpenRollout, bool) {
	parts := strings.SplitN(line, "\t", 9)
	if len(parts) < 9 {
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
	path := parts[8]
	name := path[strings.LastIndex(path, "/")+1:]
	sessionID, ok := rolloutSessionID(name)
	if !ok {
		return codexOpenRollout{}, false
	}
	return codexOpenRollout{
		PID:     pid,
		Elapsed: parseEtime(parts[1]),
		File:    name,
		Rollout: codexRollout{
			File: name, SessionID: sessionID, CWD: transcriptCWD(parts[6]),
			Originator: jsonFragmentString(parts[7], `"originator":`),
			ModTime:    modTime, Size: size,
		},
		Turn: parseCodexTurn(parts[3], parts[4], parts[5]),
	}, true
}

// parseCodexRolloutRow reads one codex/rollouts row (see codexRolloutsScript).
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
	r := codexRollout{File: parts[2], SessionID: sessionID, ModTime: modTime, Size: size}
	if len(parts) >= 4 {
		r.CWD = transcriptCWD(parts[3])
	}
	if len(parts) == 5 {
		r.Originator = jsonFragmentString(parts[4], `"originator":`)
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
		if ts := codexLineTime.FindStringSubmatch(item); ts != nil {
			//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — at exactly 0 it stores the 0 the field already holds
			if at, err := time.Parse(time.RFC3339Nano, ts[1]); err == nil && at.UnixMilli() > 0 {
				turn.LastItemAt = at.UnixMilli()
			}
		}
		if strings.HasSuffix(m[1], "_call") {
			if name := codexItemName.FindStringSubmatch(item); name != nil {
				turn.LastItemName = name[1]
			}
		}
	}
	return turn
}

// liveTasks are the interactive codex processes — each in the session whose
// rollout it wrote last, or known only by its pid before it has one — and
// the TUI sessions codex's shared daemon runs.
func (c *codexCollected) liveTasks(b *taskBuilder) []Task {
	tasks := c.tuiTasks(b)
	return append(tasks, c.daemonTasks(b)...)
}

// daemonTasks are the TUI sessions codex's shared app-server daemon
// holds. A TUI started without -c or --no-daemon runs its session there,
// and nothing on the host ties the TUI's process to the session (checked
// with codex-cli 0.157.1 on Linux): the session is a task of its own, which
// no pane can be told to show. The daemon keeps a session's rollout open
// after its TUI has exited, so such a session is not listed as stopped
// while the daemon holds it. A session a TUI holds itself is that TUI's.
func (c *codexCollected) daemonTasks(b *taskBuilder) []Task {
	listed := map[string]bool{}
	for _, o := range c.Open {
		if proc, alive := b.procs[o.PID]; alive && isInteractiveCodex(proc.Command) {
			listed[o.Rollout.SessionID] = true
		}
	}
	var tasks []Task
	for _, o := range c.Open {
		proc, alive := b.procs[o.PID]
		daemonTUISession := alive && isCodexDaemon(proc.Command) && o.Rollout.Originator == codexOriginatorTUI
		if !daemonTUISession || listed[o.Rollout.SessionID] {
			continue
		}
		listed[o.Rollout.SessionID] = true
		task := Task{
			Host:      b.host,
			ID:        b.id(AgentCodex, o.Rollout.SessionID),
			Agent:     AgentCodex,
			SessionID: o.Rollout.SessionID,
			Log:       codexLogVersion(o.Rollout),
			CWD:       o.Rollout.CWD,
			PID:       o.PID,
			Location:  Location{Kind: LocationDaemon},
		}
		if o.Elapsed >= 0 {
			task.StartedAt = b.hostMillis((b.raw.Now - o.Elapsed) * 1000)
		}
		b.codexState(&task, o)
		tasks = append(tasks, task)
	}
	return tasks
}

// tuiTasks are the interactive codex processes.
func (c *codexCollected) tuiTasks(b *taskBuilder) []Task {
	current := map[int]codexOpenRollout{}
	for _, o := range c.Open {
		prev, seen := current[o.PID]
		// An equal name at an equal time is one file held on two descriptors,
		// whose rows are alike in every field.
		newer := newerCodexRollout(o.Rollout, prev.Rollout)
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
		elapsed, known := c.Ages[p.PID]
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
		task.Log = codexLogVersion(o.Rollout)
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
		// The question's own time, which a second question in the same
		// turn does not share; the rollout's mtime moves with every write.
		task.WaitSignature = waitSignature(b.host, AgentCodex, task.SessionID, o.Turn.LastItemAt, codexToolAskUser)
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

// heldSessions is every session whose rollout a live codex process
// holds open, current or not: none of them is stopped.
func (c *codexCollected) heldSessions(b *taskBuilder) map[string]bool {
	held := map[string]bool{}
	for _, o := range c.Open {
		if proc, alive := b.procs[o.PID]; alive && isCodexProgram(proc.Command) {
			held[o.Rollout.SessionID] = true
		}
	}
	return held
}

// isCodexDaemon reports whether a `ps` command line is codex's app-server:
// argv[0]'s base name is codex and its first positional argument is
// app-server, as for the shared daemon (`codex app-server --listen unix://
// --managed-daemon`).
func isCodexDaemon(command string) bool {
	if !isCodexProgram(command) {
		return false
	}
	return agent.CodexSubcommand(command) == "app-server"
}

func codexLogVersion(r codexRollout) *LogVersion {
	return &LogVersion{File: r.File, ModTime: r.ModTime, Size: r.Size}
}

func newerCodexRollout(a, b codexRollout) bool {
	if a.ModTime != b.ModTime {
		//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — the guard excludes equal modification times
		return a.ModTime > b.ModTime
	}
	if a.Size != b.Size {
		//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — the guard excludes equal sizes
		return a.Size > b.Size
	}
	//mutation:exempt[CONDITIONALS_BOUNDARY] unreachable — equal file/version keys are identical collected rows
	return a.File > b.File
}
