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
	"regexp"
	"strings"
	"sync"
	"time"
)

// validSessionID matches the shape of a session id claude itself would ever
// emit — an alphanumeric token, optionally with internal "." "_" "-"
// separators (a UUID fits this), and critically never starting with "-".
// --resume's value is optional in the claude CLI's own parser, so a
// persisted session id beginning with "-" would be parsed as a new CLI flag
// rather than a --resume value — the same class of argument-injection
// buildArgs's own "--" end-of-options marker defends the prompt against,
// except --resume's value sits before that marker and needs its own guard.
// A regex-allowlist branch gating a value before it reaches exec.Command
// argv is this repository's established pattern for exactly this problem
// (see docs/security.md's validateShell/validTmuxSessionName/validRemotePath).
var validSessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

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
type commandFactory func(ctx context.Context, dir, name string, args ...string) cmdRunner

const defaultClaudeBin = "claude"

// promptHistoryType marks a history entry panemux wrote itself. The CLI
// never emits this type.
const promptHistoryType = "panemux_prompt"

// defaultQueryTimeout bounds how long a single query's subprocess may run.
// Without this, a hung or very slow `claude` invocation would keep the
// Runner's single-query busy flag set indefinitely — worse, the context
// Query receives from the WS handler is the request context of an already
// http.Hijack'd connection (see internal/ws/board_command.go), which the
// standard library never cancels on client disconnect, so relying on the
// caller's own context alone is not sufficient. A generous but finite
// default keeps an abandoned or wedged query from blocking the command
// center forever, while still allowing a genuinely long agent turn to
// finish.
const defaultQueryTimeout = 5 * time.Minute

// RunnerConfig configures a Runner. NewCommand, Now, and QueryTimeout
// default to production behavior (a real subprocess, time.Now,
// defaultQueryTimeout) when left zero.
type RunnerConfig struct {
	BuildMCPConfig func() (path string, cleanup func(), err error)
	NewCommand     commandFactory
	Now            func() time.Time
	NewSessionID   func() (string, error)
	NewWorkDir     func() (dir string, cleanup func(), err error)
	ClaudeBin      string
	SessionPath    string
	HistoryPath    string
	ContextDir     string
	AllowedTools   []string
	QueryTimeout   time.Duration
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
	newSessionID   func() (string, error)
	newWorkDir     func() (dir string, cleanup func(), err error)
	claudeBin      string
	sessionPath    string
	historyPath    string
	contextDir     string
	allowedTools   []string
	queryTimeout   time.Duration
	mu             sync.Mutex
	busy           bool
}

// NewRunner constructs a Runner from cfg.
func NewRunner(cfg RunnerConfig) *Runner {
	r := &Runner{
		buildMCPConfig: cfg.BuildMCPConfig,
		newCommand:     cfg.NewCommand,
		now:            cfg.Now,
		newSessionID:   cfg.NewSessionID,
		newWorkDir:     cfg.NewWorkDir,
		claudeBin:      cfg.ClaudeBin,
		sessionPath:    cfg.SessionPath,
		historyPath:    cfg.HistoryPath,
		contextDir:     cfg.ContextDir,
		allowedTools:   cfg.AllowedTools,
		queryTimeout:   cfg.QueryTimeout,
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
	if r.newSessionID == nil {
		r.newSessionID = NewSessionID
	}
	if r.newWorkDir == nil {
		r.newWorkDir = NewWorkDir
	}
	if r.queryTimeout <= 0 {
		r.queryTimeout = defaultQueryTimeout
	}
	return r
}

// realCommandFactory wraps exec.CommandContext. name is a hardcoded literal
// ("claude") unless operator-overridden, and args are passed as discrete
// argv elements with no intermediate shell, so no argument here can be
// reinterpreted as a command.
func realCommandFactory(ctx context.Context, dir, name string, args ...string) cmdRunner {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: see doc comment above
	// An empty, panemux-created directory, so the subprocess never reads a
	// CLAUDE.md belonging to whatever project the operator started panemux
	// in. See context.go for the live verification behind this.
	cmd.Dir = dir
	return cmd
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
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	state, firstRun, err := r.loadValidatedSessionState()
	if err != nil {
		events <- errorEvent("loading command center session state: %v", err)
		return
	}

	// A first run mints its own session id rather than adopting the one the
	// CLI reports: `claude -p` with no --resume reports the *ambient*
	// session id, so adopting it attaches this subprocess to a conversation
	// panemux does not own. See context.go.
	sessionID := state.SessionID
	if firstRun {
		sessionID, err = r.newSessionID()
		if err != nil {
			events <- errorEvent("%v", err)
			return
		}
	}

	mcpPath, cleanup, err := r.buildMCPConfig()
	if err != nil {
		events <- errorEvent("building mcp config: %v", err)
		return
	}
	defer cleanup()

	workDir, cleanupWorkDir, err := r.newWorkDir()
	if err != nil {
		events <- errorEvent("%v", err)
		return
	}
	defer cleanupWorkDir()

	cmd := r.newCommand(ctx, workDir, r.claudeBin, r.buildArgs(sessionID, firstRun, mcpPath, prompt)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		events <- errorEvent("creating stdout pipe: %v", err)
		return
	}
	if err := cmd.Start(); err != nil {
		// The turn never ran, so there is no stream to pair the prompt
		// with; recording it alone would imply an exchange that did not
		// happen.
		events <- errorEvent("starting claude: %v", err)
		return
	}

	// The prompt leads its own turn in the record. The stream carries no
	// trace of it — verified against a real run — so without this the
	// history is a list of answers with nothing to attach them to.
	historyEntries := []HistoryEntry{r.promptHistoryEntry(prompt)}
	streamed, _, scanFailed := r.streamOutput(stdout, events)
	historyEntries = append(historyEntries, streamed...)

	if scanFailed {
		// The client has already been told the query failed (streamOutput
		// sent the EventError). Cancel now, before Wait(), rather than
		// relying on the deferred cancel() at the top of this function:
		// that one only fires once run() itself returns, which can't
		// happen until Wait() returns — so without this, a subprocess that
		// doesn't exit on its own after a malformed line would hold the
		// busy flag (and this goroutine) for up to the full query timeout
		// instead of being killed immediately.
		cancel()
	}

	r.finishAfterStream(ctx, cmd, finishParams{
		firstRun:       firstRun,
		sessionID:      sessionID,
		historyEntries: historyEntries,
		scanFailed:     scanFailed,
	}, events)
}

// loadValidatedSessionState loads the persisted session state and, if its
// session id doesn't match validSessionID's shape, treats that exactly like
// no persisted id at all (see validSessionID's doc comment) rather than
// letting an unsafe value reach buildArgs.
func (r *Runner) loadValidatedSessionState() (state SessionState, firstRun bool, err error) {
	state, err = LoadSessionFile(r.sessionPath)
	if err != nil {
		return SessionState{}, false, err
	}
	if state.SessionID != "" && !validSessionID.MatchString(state.SessionID) {
		// Best-effort clear so a future query doesn't keep re-tripping this
		// same fallback.
		_ = SaveSessionFile(r.sessionPath, SessionState{})
		state.SessionID = ""
	}
	return state, state.SessionID == "", nil
}

// finishParams bundles what finishAfterStream needs to know about the
// streamOutput phase that already ran before cmd.Wait().
type finishParams struct {
	sessionID      string
	historyEntries []HistoryEntry
	firstRun       bool
	scanFailed     bool
}

// finishAfterStream waits for the subprocess to exit, persists any captured
// history, and emits the query's final event. Split out of run so run stays
// under this repository's function-length lint limit.
func (r *Runner) finishAfterStream(ctx context.Context, cmd cmdRunner, p finishParams, events chan<- Event) {
	waitErr := cmd.Wait()

	if len(p.historyEntries) > 0 {
		if err := AppendHistory(r.historyPath, p.historyEntries); err != nil {
			events <- errorEvent("persisting command center history: %v", err)
		}
	}

	if p.scanFailed {
		return // an EventError for the malformed line was already sent
	}
	if waitErr != nil {
		// A query killed by this Runner's own timeout is not evidence the
		// --resume id itself is bad — it's evidence the query took too
		// long, which says nothing about whether claude would still
		// recognize the id on a fresh attempt. Only a genuine resume
		// rejection (the CLI's own signal that --resume continuity is
		// already broken — e.g. the user cleared ~/.claude, or the session
		// was garbage collected) should clear the persisted id; conflating
		// the two would silently drop a perfectly good, still-resumable
		// conversation just because one turn happened to run long.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			events <- Event{Type: EventError, Err: fmt.Sprintf("claude query timed out after %s", r.queryTimeout)}
			return
		}
		if !p.firstRun {
			// Best-effort: a failure to clear it doesn't need its own
			// separate event — the query's own error below is still the
			// actionable one, and the next query will simply hit the same
			// resume failure again.
			_ = SaveSessionFile(r.sessionPath, SessionState{})
		}
		events <- errorEvent("claude exited with error: %v", waitErr)
		return
	}
	// The id persisted here is the one panemux minted and passed as
	// --session-id, never the one the subprocess reported back: the report
	// is what used to leak an ambient session into this file.
	if p.firstRun && p.sessionID != "" {
		if err := SaveSessionFile(r.sessionPath, SessionState{SessionID: p.sessionID}); err != nil {
			events <- errorEvent("persisting command center session id: %v", err)
			return
		}
	}
	events <- Event{Type: EventDone}
}

// promptHistoryEntry records the operator's own prompt as a history entry.
// panemux owns this file's format (see docs/agent-board.md's "API and
// streaming"), and the type is one the CLI never emits, so a reader can
// always tell a panemux-written entry from a relayed subprocess line.
func (r *Runner) promptHistoryEntry(prompt string) HistoryEntry {
	// json.Marshal on a string cannot fail, so the error is not reachable;
	// the fallback keeps the entry well-formed rather than empty if that
	// ever stops being true.
	raw, err := json.Marshal(map[string]string{"type": promptHistoryType, "text": prompt})
	if err != nil {
		raw = []byte(`{"type":"` + promptHistoryType + `","text":""}`)
	}
	return HistoryEntry{At: r.now(), Raw: raw}
}

// buildArgs constructs the claude CLI argv. Two details here are load-bearing
// for safety, both verified live against the real CLI (v2.1.226), not just
// inferred from --help text:
//
//   - "--allowedTools" is declared variadic (`<tools...>`) by the CLI's own
//     parser, so passing it and its value as two separate argv elements lets
//     it swallow the very next element too — which would be the prompt,
//     silently breaking every query ("Input must be provided either through
//     stdin or as a prompt argument"). The "=" form
//     ("--allowedTools=a,b,c") is a single argv element and cannot be
//     extended by a following element.
//   - The prompt is preceded by a literal "--" end-of-options marker.
//     Without it, a prompt beginning with "-" (e.g. a user typing
//     "--help" into the palette) is parsed as a CLI flag instead of being
//     passed through as prompt text — argument injection into the claude
//     CLI's own option parser, up to and including flags that alter its
//     permission model.
func (r *Runner) buildArgs(sessionID string, firstRun bool, mcpPath, prompt string) []string {
	args := []string{"-p"}
	if firstRun {
		// Pin the conversation to an id panemux minted. Without this the
		// CLI reports an ambient session id that a later --resume would
		// attach to — see context.go for the live verification.
		args = append(args, "--session-id", sessionID)
	} else {
		args = append(args, "--resume", sessionID)
	}
	args = append(args,
		"--output-format=stream-json",
		"--verbose",
		"--mcp-config", mcpPath,
		// Only the board MCP server this query was configured with; never
		// whatever else the operator has registered globally.
		"--strict-mcp-config",
		// Load no user, project or local settings: the operator's own hooks
		// must not fire inside a subprocess panemux spawned, and their
		// CLAUDE.md is not this orchestrator's instruction set. panemux's
		// own instructions arrive via --append-system-prompt below, which
		// is independent of setting sources.
		"--setting-sources", "",
		// Slash commands sit outside both tool lists. Verified against the
		// real CLI: a prompt of "/context" returned that command's own
		// output, so the palette — reachable by anyone holding the board
		// token — could otherwise drive the CLI's whole slash-command
		// registry (/config, /model, /mcp, /doctor, ...).
		"--disable-slash-commands",
		"--append-system-prompt", SystemPrompt(r.contextDir),
		// A fixed literal that only narrows what the subprocess may do; the
		// operator's own settings are never merged in. See
		// SubprocessSettings for the escalation that rules out merging.
		"--settings", SubprocessSettings,
	)
	args = append(args,
		"--allowedTools="+strings.Join(r.allowedTools, ","),
		// The denial that survives a permissions override; see
		// DisallowedTools for the three-row experiment behind it.
		"--disallowedTools="+strings.Join(DisallowedTools(), ","),
		"--",
		prompt,
	)
	return args
}

// streamOutput reads stdout line by line, emitting an EventLine per parsed
// stream-json line and collecting HistoryEntry copies for persistence. It
// stops at the first line that fails to parse as a JSON object or the first
// underlying read error, having already sent the corresponding EventError,
// and — via the deferred drain below, covering every exit path uniformly —
// always drains any remaining output so the subprocess is never left
// blocked writing into a pipe no one is reading. Wait() must still be safe
// to call after this returns.
func (r *Runner) streamOutput(
	stdout io.ReadCloser, events chan<- Event,
) (entries []HistoryEntry, sessionID string, failed bool) {
	defer func() { _, _ = io.Copy(io.Discard, stdout) }()

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
