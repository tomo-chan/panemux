package tasks

import (
	"sort"
	"strconv"
	"strings"

	"panemux/internal/agent"
)

// claudeCollect is Claude Code's collect adapter. Its live section is
// ~/.claude/sessions, one state file per running session, and its stopped
// section the conversation logs directly under ~/.claude/projects/*/.
type claudeCollect struct{}

// Claude Code's sections, within the "claude" namespace.
const (
	claudeSectionState       = "state"
	claudeSectionTranscripts = "transcripts"
)

func (claudeCollect) sections() []collectSection {
	return []collectSection{
		{name: claudeSectionState, stage: stageLive, files: true, script: claudeStateScript},
		{name: claudeSectionTranscripts, stage: stageStopped, script: claudeTranscriptsScript},
	}
}

func (claudeCollect) processHints() []string { return []string{string(agent.Claude)} }

func (claudeCollect) newCollected() agentCollected { return &claudeCollected{} }

// claudeStateScript prints each ~/.claude/sessions/*.json after
// "::file <name>".
const claudeStateScript = `for f in "$HOME"/.claude/sessions/*.json; do
	[ -f "$f" ] || continue
	echo "::file ${f##*/}"
	cat "$f" 2>/dev/null
	echo
done
`

// claudeTranscriptsScript prints
// "<mtime>\t<file name>\t<first "cwd":"..." in it>\t<size in bytes>" for the
// conversation logs directly under ~/.claude/projects/*/ modified in the last
// 7 days, newest first, at most 100 of them. The window and the count are the
// range decided for stopped sessions (issue #252); the Go side keeps the
// stopped ones and caps them at maxStoppedTasks.
const claudeTranscriptsScript = `find "$HOME/.claude/projects" -mindepth 2 -maxdepth 2 \
	-type f -name '*.jsonl' -mtime -7 2>/dev/null |
while IFS= read -r p; do
	t=$(mtime "$p" 2>/dev/null) && echo "$t $p"
done | sort -rn | head -n 100 | while read -r t s p; do
	c=$(grep -m 1 -o '"cwd":"[^"]*"' "$p" 2>/dev/null | head -n 1)
	printf '%s\t%s\t%s\t%s\n' "$t" "${p##*/}" "$c" "$s"
done
`

// claudeCollected is Claude Code's part of one host's output.
type claudeCollected struct {
	StateFiles  []stateFile
	Transcripts []transcript
	// states are StateFiles read, set by prepared.
	states []readState
}

// addRow reads a transcripts row: the state section declares files, so its
// lines reach addFile instead.
func (c *claudeCollected) addRow(_, line string) {
	if tr, ok := parseTranscriptRow(line); ok {
		c.Transcripts = append(c.Transcripts, tr)
	}
}

func (c *claudeCollected) addFile(_, name string, data []byte) {
	c.StateFiles = append(c.StateFiles, stateFile{Name: name, Data: data})
}

func (c *claudeCollected) prepared() agentCollected {
	p := &claudeCollected{StateFiles: c.StateFiles, Transcripts: c.Transcripts}
	for _, file := range c.StateFiles {
		p.states = append(p.states, readStateFile(file))
	}
	return p
}

// liveTasks are the sessions the state files name, and the interactive
// claude processes none of them explains.
func (c *claudeCollected) liveTasks(b *taskBuilder) []Task {
	live := c.liveSessionTasks(b)
	return append(live, c.unexplainedTasks(b, live)...)
}

// stoppedCandidates are the transcripts no live task handles. A running
// task with no session id (its state file is unreadable or missing) is most
// likely writing the newest log in its directory; that log is its own, not
// a second, stopped task.
func (c *claudeCollected) stoppedCandidates(_ *taskBuilder, live []Task) []stoppedCandidate {
	liveSessions := map[string]bool{}
	claimedLogs := map[string]int{}
	for _, task := range live {
		switch {
		case task.SessionID != "":
			liveSessions[task.SessionID] = true
		case task.PID > 0 && task.CWD != "":
			claimedLogs[task.CWD]++
		}
	}
	transcripts := append([]transcript(nil), c.Transcripts...)
	sort.SliceStable(transcripts, func(i, j int) bool { return transcripts[i].ModTime > transcripts[j].ModTime })

	seen := map[string]bool{}
	var candidates []stoppedCandidate
	for _, tr := range transcripts {
		if liveSessions[tr.SessionID] || seen[tr.SessionID] {
			continue
		}
		seen[tr.SessionID] = true
		if claimedLogs[tr.CWD] > 0 {
			claimedLogs[tr.CWD]--
			continue
		}
		candidates = append(candidates, stoppedCandidate{
			agent: AgentClaude, sessionID: tr.SessionID, cwd: tr.CWD, modTime: tr.ModTime,
		})
	}
	return candidates
}

// addLogVersion sets a task's transcript version from the newest transcript
// of its session.
func (c *claudeCollected) addLogVersion(task *Task) {
	if task.SessionID == "" {
		return
	}
	var newest *transcript
	for i, tr := range c.Transcripts {
		if tr.SessionID == task.SessionID && (newest == nil || tr.ModTime > newest.ModTime) {
			newest = &c.Transcripts[i]
		}
	}
	if newest != nil {
		task.Log = &LogVersion{ModTime: newest.ModTime, Size: newest.Size}
	}
}

// liveSessionTasks are the running sessions the state files name.
func (c *claudeCollected) liveSessionTasks(b *taskBuilder) []Task {
	bySession := map[string]Task{}
	sinceMillis := map[string]int64{}

	for _, state := range c.states {
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
func (c *claudeCollected) unreadableStateFiles(b *taskBuilder) []UnreadableStateFile {
	var files []UnreadableStateFile
	for _, state := range c.states {
		if state.reason == "" {
			continue
		}
		file := UnreadableStateFile{
			name:   state.name,
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
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files
}

// unexplainedTasks are running interactive claude processes that no
// readable state file names, by its content or by its file name. Without
// them, a Claude Code release that moved, stopped writing or changed
// ~/.claude/sessions would show every running agent as stopped.
func (c *claudeCollected) unexplainedTasks(b *taskBuilder, known []Task) []Task {
	explained := map[int]bool{}
	for _, task := range known {
		explained[task.PID] = true
	}
	for _, state := range c.states {
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

type stateFile struct {
	Name string
	Data []byte
}

type transcript struct {
	SessionID string
	CWD       string
	ModTime   int64
	// Size is the log's size in bytes, 0 when the host did not report one.
	// With ModTime it tells whether the log changed since it was summarized.
	Size int64
}

func parseTranscriptRow(line string) (transcript, bool) {
	parts := strings.SplitN(line, "\t", 4)
	if len(parts) < 2 {
		return transcript{}, false
	}
	modTime, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return transcript{}, false
	}
	sessionID, ok := strings.CutSuffix(parts[1], ".jsonl")
	if !ok || !validSessionID.MatchString(sessionID) {
		return transcript{}, false
	}
	tr := transcript{ModTime: modTime, SessionID: sessionID}
	if len(parts) >= 3 {
		tr.CWD = transcriptCWD(parts[2])
	}
	// The cwd fragment is JSON, whose strings never hold a literal tab, so
	// a fourth field is always the size.
	if len(parts) == 4 {
		//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — a size of 0 is stored as 0 either way
		if size, err := strconv.ParseInt(parts[3], 10, 64); err == nil && size > 0 {
			tr.Size = size
		}
	}
	return tr, true
}

// formats are the live sessions' state files in a format this build does
// not know: one that cannot be read (counted by its reason, unless it is a
// leftover of a process that is gone), and one whose status is not one
// claudeStatusState knows. A missing status is read as an older Claude
// Code's, not a new format. The versions are the live sessions'.
func (c *claudeCollected) formats(b *taskBuilder) formatReport {
	var r formatReport
	for _, state := range c.states {
		if state.reason != "" {
			if pid, ok := stateFilePID(state.name); ok && !b.isLiveClaude(pid) {
				continue
			}
			r.add(string(state.reason), state.name+": "+state.detail)
			continue
		}
		st := state.st
		if !b.isLiveClaude(st.PID) {
			continue
		}
		if st.Status != "" && claudeStatusState(st.Status) == StateUnknown {
			r.add(formatUnknownStatus, "status "+strconv.Quote(st.Status))
		}
		r.observeVersion(st.Version, firstNonZero(st.StatusUpdatedAt, st.UpdatedAt, st.StartedAt))
	}
	return r
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
