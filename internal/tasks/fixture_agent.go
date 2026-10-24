//go:build panemux_fixture_agent

package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"panemux/internal/agent"
)

// The fixture agent's adapters exist only in a build with the
// panemux_fixture_agent tag, as its registration does (issue #377). They are
// the smallest adapters the task board takes: collect, launch and summary,
// written against the same interfaces as Claude Code's and codex's, with no
// change to the common code. Its CLI is a fake the E2E run puts on PATH; it
// writes one state file per running session under
// ~/.panemux-fixture-agent/sessions and one log per session under
// ~/.panemux-fixture-agent/logs.

const fixtureAgentName = string(agent.FixtureAgent)

func fixtureCollectAdapters() []registeredCollect {
	return []registeredCollect{{id: agent.FixtureAgent, adapter: fixtureCollect{}}}
}

func fixtureLaunchAdapters() []registeredLaunch {
	return []registeredLaunch{{id: agent.FixtureAgent, adapter: fixtureLaunch{}}}
}

func fixtureSummaryAdapters() []registeredSummary {
	return []registeredSummary{{
		id: agent.FixtureAgent, source: fixtureSummarySource{}, runner: fixtureSummaryRunner{bin: fixtureAgentName},
	}}
}

// The fixture agent's sections, within its namespace.
const (
	fixtureSectionState = "state"
	fixtureSectionLogs  = "logs"
)

// fixtureStateScript prints each state file after "::file <name>".
const fixtureStateScript = `for f in "$HOME"/.panemux-fixture-agent/sessions/*.json; do
	[ -f "$f" ] || continue
	echo "::file ${f##*/}"
	cat "$f" 2>/dev/null
	echo
done
`

// fixtureLogsScript prints "<mtime>\t<size>\t<file name>" for each log.
const fixtureLogsScript = `for f in "$HOME"/.panemux-fixture-agent/logs/*.log; do
	[ -f "$f" ] || continue
	mtime "$f" 2>/dev/null | while read -r t s; do
		printf '%s\t%s\t%s\n' "$t" "$s" "${f##*/}"
	done
done
`

// fixtureCollect lists the fixture agent's running sessions from their state
// files, and gives each the version of its log for its summary.
type fixtureCollect struct{}

func (fixtureCollect) sections() []collectSection {
	return []collectSection{
		{name: fixtureSectionState, stage: stageLive, files: true, script: fixtureStateScript},
		{name: fixtureSectionLogs, stage: stageStopped, script: fixtureLogsScript},
	}
}

func (fixtureCollect) processHints() []string { return []string{fixtureAgentName} }

func (fixtureCollect) newCollected() agentCollected { return &fixtureCollected{} }

func (fixtureCollect) stateNote(task Task) string {
	if task.State == StateUnknown {
		return "The fixture agent reported a status the dashboard does not recognize."
	}
	return ""
}

// fixtureState is one state file of the fake CLI.
type fixtureState struct {
	SessionID       string `json:"sessionId"`
	CWD             string `json:"cwd"`
	Status          string `json:"status"`
	WaitingFor      string `json:"waitingFor"`
	PID             int    `json:"pid"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
}

type fixtureLog struct {
	sessionID string
	modTime   int64
	size      int64
}

// fixtureCollected is the fixture agent's part of one host's output.
type fixtureCollected struct {
	files  []stateFile
	logs   []fixtureLog
	states []fixtureState
}

func (c *fixtureCollected) addRow(_, line string) {
	parts := strings.Split(line, "\t")
	if len(parts) != 3 {
		return
	}
	modTime, errTime := strconv.ParseInt(parts[0], 10, 64)
	size, errSize := strconv.ParseInt(parts[1], 10, 64)
	sessionID, ok := strings.CutSuffix(parts[2], ".log")
	if errTime != nil || errSize != nil || !ok || !validUUID.MatchString(sessionID) {
		return
	}
	c.logs = append(c.logs, fixtureLog{sessionID: sessionID, modTime: modTime, size: size})
}

func (c *fixtureCollected) addFile(_, name string, data []byte) {
	c.files = append(c.files, stateFile{Name: name, Data: data})
}

// prepared reads the state files, leaving out any that is not JSON or names
// no session or process: the fake CLI writes nothing else.
func (c *fixtureCollected) prepared() agentCollected {
	p := &fixtureCollected{files: c.files, logs: c.logs}
	for _, file := range c.files {
		var st fixtureState
		if json.Unmarshal(file.Data, &st) != nil || st.PID <= 0 || !validUUID.MatchString(st.SessionID) {
			continue
		}
		p.states = append(p.states, st)
	}
	return p
}

// liveTasks are the sessions whose state file names a running interactive
// fixture process. A status outside the fake CLI's set is unknown, not
// guessed.
func (c *fixtureCollected) liveTasks(b *taskBuilder) []Task {
	rule := taskRule(agent.FixtureAgent)
	var tasks []Task
	seen := map[string]bool{}
	for _, st := range c.states {
		proc, alive := b.procs[st.PID]
		if !alive || !rule.IsInteractive(proc.Command) || seen[st.SessionID] {
			continue
		}
		seen[st.SessionID] = true
		task := Task{
			Host:        b.host,
			ID:          b.id(fixtureAgentName, st.SessionID),
			Agent:       fixtureAgentName,
			SessionID:   st.SessionID,
			CWD:         st.CWD,
			State:       fixtureStatusState(st.Status),
			PID:         st.PID,
			StatusSince: b.hostMillis(st.StatusUpdatedAt),
			Location:    b.locate(st.PID),
		}
		if task.State == StateWait {
			task.WaitingFor = st.WaitingFor
			task.WaitSignature = waitSignature(b.host, fixtureAgentName, st.SessionID, st.StatusUpdatedAt, st.WaitingFor)
		}
		tasks = append(tasks, task)
	}
	return tasks
}

// The statuses the fake CLI writes.
const (
	fixtureStatusBusy    = "busy"
	fixtureStatusWaiting = "waiting"
	fixtureStatusIdle    = "idle"
)

func fixtureStatusState(status string) State {
	switch status {
	case fixtureStatusBusy:
		return StateBusy
	case fixtureStatusWaiting:
		return StateWait
	case fixtureStatusIdle:
		return StateIdle
	default:
		return StateUnknown
	}
}

// stoppedCandidates is empty: the fixture agent cannot be resumed, so a
// stopped session is not listed.
func (c *fixtureCollected) stoppedCandidates(*taskBuilder, []Task) []stoppedCandidate { return nil }

func (c *fixtureCollected) addLogVersion(task *Task) {
	for _, l := range c.logs {
		if l.sessionID == task.SessionID && (task.Log == nil || l.modTime > task.Log.ModTime) {
			task.Log = &LogVersion{ModTime: l.modTime, Size: l.size}
		}
	}
}

// fixtureLaunch starts the fixture agent with a session ID panemux mints,
// as Claude Code is started. It has no resume adapter.
type fixtureLaunch struct{}

func (fixtureLaunch) newSession(r io.Reader) (string, string, error) {
	id, err := newSessionID(r)
	if err != nil {
		return "", "", err
	}
	return id, tmuxSessionForTask(id), nil
}

func (fixtureLaunch) validNewSessionID(id string) bool { return validUUID.MatchString(id) }
func (fixtureLaunch) note() string                     { return "" }
func (fixtureLaunch) refusals() map[string]string      { return nil }

func (fixtureLaunch) newCommand() string {
	return `  run='p=$(cat -- "$1"); rm -f -- "$1"; cd -- "$4" || exit 1; exec "$2" "--session-id=$3" -- "$p"'`
}

// fixtureSummarySource prints a session's log on its host.
type fixtureSummarySource struct{}

func (fixtureSummarySource) validSessionID(id string) bool { return validUUID.MatchString(id) }

// logScript prints the log of a session ID the source accepted; the ID is a
// UUID, so it holds nothing the shell would read.
func (s fixtureSummarySource) logScript(sessionID string, _ LogVersion) (string, error) {
	if !s.validSessionID(sessionID) {
		return "", fmt.Errorf("%w: session ID %q", ErrInvalidSummary, sessionID)
	}
	return `cat -- "$HOME/.panemux-fixture-agent/logs/` + sessionID + `.log" 2>/dev/null; exit 0` + "\n", nil
}

// excerpt is the text of the log's lines, each a JSON object with a "text"
// field, cut to the size a Claude Code excerpt may be. A line that is not
// one makes the log unreadable rather than half read.
func (fixtureSummarySource) excerpt(out []byte, _ string) (string, bool, error) {
	var texts []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var entry struct {
			Text *string `json:"text"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Text == nil {
			return "", false, errors.New("the fixture agent's log could not be read")
		}
		texts = append(texts, *entry.Text)
	}
	if len(texts) == 0 {
		return "", false, nil
	}
	return truncateUTF8(strings.Join(texts, "\n"), transcriptHeadBytes+transcriptTailBytes), true, nil
}

func (fixtureSummarySource) version() string { return "" }

// fixtureSummaryRunner summarizes with the fake CLI's one-shot mode. bin is
// the literal fixture CLI name in the registration; tests pass a stub.
type fixtureSummaryRunner struct{ bin string }

func (fixtureSummaryRunner) version() string                      { return "fixture-summary-1" }
func (fixtureSummaryRunner) unexpectedModelWarning(string) string { return "" }

func (r fixtureSummaryRunner) newSummarizer() SummarizeFunc {
	return func(ctx context.Context, excerpt string) (Summary, error) {
		// G204: bin is the literal fixture CLI name (or a test's stub), and
		// the argument is a constant.
		cmd := exec.CommandContext(ctx, r.bin, "--print") //nolint:gosec // see above
		cmd.Stdin = strings.NewReader(excerpt)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
		cmd.WaitDelay = summaryWaitDelay
		out, err := cmd.Output()
		switch {
		case err == nil:
			return parseFixtureSummary(out)
		case ctx.Err() != nil:
			return Summary{}, errors.New("the fixture agent did not finish summarizing in time")
		case errors.Is(err, exec.ErrNotFound):
			return Summary{}, errSummaryCLIMissing(agent.FixtureAgent)
		default:
			return Summary{}, errors.New("the fixture agent failed to summarize")
		}
	}
}

// parseFixtureSummary reads {"summary": ..., "remaining": [...]}, bounded as
// every summary is. Its errors are fixed messages.
func parseFixtureSummary(out []byte) (Summary, error) {
	var answer struct {
		Summary   string   `json:"summary"`
		Remaining []string `json:"remaining"`
	}
	if err := json.Unmarshal(out, &answer); err != nil {
		return Summary{}, errors.New("the fixture agent's answer was not JSON")
	}
	summary := Summary{Text: truncateUTF8(strings.TrimSpace(answer.Summary), maxSummaryTextBytes), Remaining: []string{}}
	if summary.Text == "" {
		return Summary{}, errors.New("the fixture agent's answer had no summary")
	}
	for _, item := range answer.Remaining {
		if len(summary.Remaining) == maxSummaryRemaining {
			break
		}
		if item = strings.TrimSpace(item); item != "" {
			summary.Remaining = append(summary.Remaining, truncateUTF8(item, maxSummaryItemBytes))
		}
	}
	return summary, nil
}
