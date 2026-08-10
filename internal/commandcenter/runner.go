package commandcenter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ErrBusy is returned by Query when a previous query against this Runner's
// single command-center session is still in flight. See
// docs/agent-board.md's "Concurrency" subsection: two concurrent
// `claude -p --resume <same-id>` invocations have no ordering guarantee
// from the CLI itself, so a second request is rejected immediately rather
// than queued.
var ErrBusy = errors.New("command center busy")

// EventType discriminates the kind of Event a Query emits.
type EventType string

const (
	// EventLine carries one raw --output-format=stream-json line, exactly
	// as emitted by the subprocess.
	EventLine EventType = "line"
	// EventError reports a subprocess failure — non-zero exit, malformed
	// stream-json output, or a context cancellation/timeout — as an
	// explicit frame rather than a silently empty response. It is always
	// the last event on a channel that received one.
	EventError EventType = "error"
	// EventDone marks a successful query's end. It is never sent after an
	// EventError on the same channel.
	EventDone EventType = "done"
)

// Event is one item streamed back from Query's channel.
//
//nolint:govet // fieldalignment: Type/Raw/Err order kept for readability, padding cost is negligible
type Event struct {
	Type EventType
	Raw  json.RawMessage
	Err  string
}

// cmdRunner abstracts the subset of *exec.Cmd Query needs, so tests can
// substitute a fake subprocess without a real `claude` binary on PATH — see
// DEVELOPMENT.md's testability rule. *exec.Cmd already satisfies this
// interface directly.
type cmdRunner interface {
	StdoutPipe() (io.ReadCloser, error)
	Start() error
	Wait() error
}

// commandFactory constructs the subprocess for one query. The production
// default wraps exec.CommandContext; tests inject a fake.
type commandFactory func(ctx context.Context, name string, args ...string) cmdRunner

const defaultClaudeBin = "claude"

// RunnerConfig configures a Runner. NewCommand and Now default to
// production behavior (a real subprocess, time.Now) when left zero.
type RunnerConfig struct {
	BuildMCPConfig func() (path string, cleanup func(), err error)
	NewCommand     commandFactory
	Now            func() time.Time
	ClaudeBin      string
	SessionPath    string
	HistoryPath    string
	AllowedTools   []string
}

// Runner drives the command center's per-query `claude -p [--resume]`
// subprocess lifecycle: at most one query in flight at a time, streaming
// parsed stream-json lines as Events, persisting captured history, and
// persisting a freshly captured session id only after a successful first
// run. See docs/agent-board.md's Command center section.
type Runner struct {
	buildMCPConfig func() (path string, cleanup func(), err error)
	newCommand     commandFactory
	now            func() time.Time
	claudeBin      string
	sessionPath    string
	historyPath    string
	allowedTools   []string
	mu             sync.Mutex
	busy           bool
}

// NewRunner constructs a Runner from cfg.
func NewRunner(cfg RunnerConfig) *Runner {
	r := &Runner{
		buildMCPConfig: cfg.BuildMCPConfig,
		newCommand:     cfg.NewCommand,
		now:            cfg.Now,
		claudeBin:      cfg.ClaudeBin,
		sessionPath:    cfg.SessionPath,
		historyPath:    cfg.HistoryPath,
		allowedTools:   cfg.AllowedTools,
	}
	if r.claudeBin == "" {
		r.claudeBin = defaultClaudeBin
	}
	if r.newCommand == nil {
		r.newCommand = realCommandFactory
	}
	if r.now == nil {
		r.now = time.Now
	}
	return r
}

// realCommandFactory wraps exec.CommandContext. name is a hardcoded literal
// ("claude") unless operator-overridden, and args are passed as discrete
// argv elements with no intermediate shell, so no argument here can be
// reinterpreted as a command.
func realCommandFactory(ctx context.Context, name string, args ...string) cmdRunner {
	return exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: see doc comment above
}

// Query starts one command-center turn if none is currently in flight,
// returning ErrBusy immediately otherwise. The returned channel is closed
// once the query finishes (successfully or not); the caller must drain it.
func (r *Runner) Query(ctx context.Context, prompt string) (<-chan Event, error) {
	r.mu.Lock()
	if r.busy {
		r.mu.Unlock()
		return nil, ErrBusy
	}
	r.busy = true
	r.mu.Unlock()

	events := make(chan Event, 64)
	go func() {
		defer close(events)
		defer func() {
			r.mu.Lock()
			r.busy = false
			r.mu.Unlock()
		}()
		r.run(ctx, prompt, events)
	}()
	return events, nil
}

func (r *Runner) run(ctx context.Context, prompt string, events chan<- Event) {
	state, err := LoadSessionFile(r.sessionPath)
	if err != nil {
		events <- errorEvent("loading command center session state: %v", err)
		return
	}
	firstRun := state.SessionID == ""

	mcpPath, cleanup, err := r.buildMCPConfig()
	if err != nil {
		events <- errorEvent("building mcp config: %v", err)
		return
	}
	defer cleanup()

	cmd := r.newCommand(ctx, r.claudeBin, r.buildArgs(state.SessionID, firstRun, mcpPath, prompt)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		events <- errorEvent("creating stdout pipe: %v", err)
		return
	}
	if err := cmd.Start(); err != nil {
		events <- errorEvent("starting claude: %v", err)
		return
	}

	historyEntries, capturedSessionID, scanFailed := r.streamOutput(stdout, events)

	waitErr := cmd.Wait()

	if len(historyEntries) > 0 {
		if err := AppendHistory(r.historyPath, historyEntries); err != nil {
			events <- errorEvent("persisting command center history: %v", err)
		}
	}

	if scanFailed {
		return // an EventError for the malformed line was already sent
	}
	if waitErr != nil {
		events <- errorEvent("claude exited with error: %v", waitErr)
		return
	}
	if firstRun && capturedSessionID != "" {
		if err := SaveSessionFile(r.sessionPath, SessionState{SessionID: capturedSessionID}); err != nil {
			events <- errorEvent("persisting command center session id: %v", err)
			return
		}
	}
	events <- Event{Type: EventDone}
}

func (r *Runner) buildArgs(sessionID string, firstRun bool, mcpPath, prompt string) []string {
	args := []string{"-p"}
	if !firstRun {
		args = append(args, "--resume", sessionID)
	}
	args = append(args,
		"--output-format=stream-json",
		"--verbose",
		"--mcp-config", mcpPath,
		"--allowedTools", strings.Join(r.allowedTools, ","),
		prompt,
	)
	return args
}

// streamOutput reads stdout line by line, emitting an EventLine per parsed
// stream-json line and collecting HistoryEntry copies for persistence. It
// stops at the first line that fails to parse as a JSON object, having
// already sent the corresponding EventError, and drains any remaining
// output so the subprocess is never left blocked writing into a pipe no one
// is reading — Wait() must still be safe to call after this returns.
func (r *Runner) streamOutput(
	stdout io.ReadCloser, events chan<- Event,
) (entries []HistoryEntry, sessionID string, failed bool) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		lineCopy := append([]byte(nil), line...)

		var probe struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(lineCopy, &probe); err != nil {
			events <- errorEvent("malformed stream-json output: %v", err)
			_, _ = io.Copy(io.Discard, stdout)
			return entries, sessionID, true
		}
		if probe.SessionID != "" {
			sessionID = probe.SessionID
		}
		entries = append(entries, HistoryEntry{At: r.now(), Raw: json.RawMessage(lineCopy)})
		events <- Event{Type: EventLine, Raw: json.RawMessage(lineCopy)}
	}
	if err := scanner.Err(); err != nil {
		events <- errorEvent("reading claude output: %v", err)
		return entries, sessionID, true
	}
	return entries, sessionID, false
}

func errorEvent(format string, err error) Event {
	return Event{Type: EventError, Err: fmt.Sprintf(format, err)}
}
