package tasks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"panemux/internal/agent"
	"panemux/internal/session"
)

// Launching and resuming tasks (issues #257 and #264). A task is started on
// its host as one claude or codex process in a tmux session of its own,
// detached: no pane is created, and the dashboard attaches one only when the
// task is opened.
//
// Everything runs through launchScriptTemplate, a fixed script fed to the
// literal command `sh -s` on stdin, exactly as collection runs. No value from
// the request reaches a command line that a shell parses: the session ID and
// tmux session name are panemux's own and pass allowlist checks, the working
// directory passes the remote-path guard, and the working directory and the
// prompt travel inside single-quoted heredocs whose terminator carries a
// random tag. See docs/security/command-execution.md, "Task launch and
// resume".

// ErrInvalidLaunch is a launch or resume refused before anything ran.
var ErrInvalidLaunch = errors.New("invalid task launch")

// ErrNoStoppedTask is a resume whose session is not a stopped task of that
// agent on the host.
var ErrNoStoppedTask = errors.New("no stopped task of that agent with that session ID")

// LaunchError is a launch the host itself refused. Code is one of the words
// the launch script prints; the message is fixed per code, so nothing the
// host prints reaches the operator's screen.
type LaunchError struct {
	Code string
}

// The words the launch script refuses with, one per way a host can refuse.
// An adapter's fragment may add its own (launchAdapter.refusals).
const (
	RefusedNoTmux = "no-tmux"
	RefusedNoCWD  = "no-cwd"
	// RefusedNoClaude and RefusedNoCodex are refusedNoAgent of each: the
	// script found no binary of the agent.
	RefusedNoClaude = "no-claude"
	RefusedNoCodex  = "no-codex"
	// RefusedCodexTooOld is a codex whose help lists no --no-daemon
	// (codex-cli 0.142.2 has none, and exits 2 on it); codexLaunch declares it.
	RefusedCodexTooOld = "codex-too-old"
	RefusedTmuxExists  = "tmux-exists"
	RefusedTmuxFailed  = "tmux-failed"
	RefusedPromptFile  = "prompt-file"
	// RefusedUnsupported is an agent and mode no adapter has a command for.
	// Launch and Resume refuse those before anything runs.
	RefusedUnsupported = "unsupported"
)

// launchErrorMessages are the messages of the template's own refusals.
// composeLaunch adds each agent's no-<agent> and its adapters' refusals.
var launchErrorMessages = map[string]string{
	RefusedNoTmux:      "tmux is not installed on the host",
	RefusedNoCWD:       "the working directory does not exist on the host",
	RefusedTmuxExists:  "a tmux session with the task's name already exists on the host",
	RefusedTmuxFailed:  "tmux could not start the session on the host",
	RefusedPromptFile:  "the prompt could not be written to a temporary file on the host",
	RefusedUnsupported: "panemux cannot start the agent that way",
}

func (e *LaunchError) Error() string {
	if msg, ok := launchPlan.messages[e.Code]; ok {
		return msg
	}
	return fmt.Sprintf("the host refused the launch (%q)", e.Code)
}

// maxPromptBytes bounds a task's first instruction. It becomes one argument
// of the claude process, and Linux refuses a single argument over 128 KiB.
const maxPromptBytes = 32 << 10

// validUUID is the only session ID a launch or resume passes to claude.
// validSessionID is looser — it accepts a leading "-", and `claude --resume`
// also takes a session title — so a resume re-checks against this.
var validUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validHeredocTag is the random part of the heredoc terminators.
var validHeredocTag = regexp.MustCompile(`^[0-9a-f]{32}$`)

// LaunchRequest is a new task: the host ("" for the panemux host), the agent
// (AgentClaude when empty), the directory it starts in, and its first
// instruction.
type LaunchRequest struct {
	Host   string
	Agent  string
	CWD    string
	Prompt string
}

// Launched is a task that was started or resumed. A new codex task has no
// TaskID or SessionID: codex picks its session ID itself, once it has been
// given its first instruction, so the task is known by TmuxSession until a
// collection finds it there.
type Launched struct {
	TaskID      string `json:"id,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	TmuxSession string `json:"tmux_session"`
}

type launchMode string

const (
	launchNew    launchMode = "new"
	launchResume launchMode = "resume"
)

type launchParams struct {
	agent       string
	mode        launchMode
	sessionID   string
	tmuxSession string
	cwd         string
	prompt      string
	tag         string
	// reuseSession lets a resume add its window to an existing tmux session
	// of the task's name; see Resume.
	reuseSession bool
}

// launchScriptTemplate starts an agent in a detached tmux session. It
// answers with one "::panemux-launch ok" or "::panemux-launch error <code>"
// line and always exits 0, so a refusal is told apart from a broken
// connection.
//
//   - The agent is looked up on PATH, then through the user's login shell,
//     because an SSH exec channel's PATH rarely holds a per-user install
//     (~/.local/bin). Only an absolute path to an executable is run.
//   - What runs is the command of the agent's launch or resume adapter,
//     a fixed fragment composeLaunch puts in the case of its agent and mode
//     ({{AGENT_COMMANDS}}). It sets run or resume, and may refuse first.
//   - codex runs with its own directory last on PATH: an npm install's
//     codex is a `#!/usr/bin/env node` script, and node sits next to it
//     there, but not on the PATH of a tmux server an exec channel started.
//     Last, so the commands codex runs resolve as they would without it.
//     It runs with --no-daemon, so the TUI keeps its session itself rather
//     than in codex's shared daemon, where nothing ties the session to the
//     tmux session it runs in, and with -c check_for_update_on_startup=false,
//     since the update prompt would hold a task nobody is watching. A codex
//     whose help lists no --no-daemon is refused before anything starts: it
//     would exit at once on the option, in a tmux session that ends with it,
//     while the launch reported success.
//   - The agent's command runs through the user's own shell, started as a
//     login and interactive shell as tmux starts a pane's shell, so what the
//     user's rc files export (an ANTHROPIC_API_KEY in ~/.bashrc) reaches the
//     agent as it reaches one typed into a pane (issue #445). The shell is
//     given only the fixed command `exec "$@"`, and the `sh -c` of the
//     agent's command follows as its arguments: the rc runs first, then that
//     command reads the prompt file, changes to the working directory and
//     sets codex's PATH. Only a $SHELL that is an absolute path to an
//     executable named bash or zsh is used, the shells whose `-c` was
//     checked to hand the arguments over as "$@"; sh goes as the absolute
//     path found here, so an rc that changes PATH cannot change which sh
//     runs the command. Any other $SHELL starts the agent's command
//     directly, and no rc is read.
//   - tmux receives the command as separate arguments after "--", which tmux
//     runs without a shell (tmux 2.0 and later). The working directory is not
//     given to tmux's -c, which expands its value as a format ("#S", "##"),
//     and starts in the home directory when the result does not exist: the
//     fixed `sh -c` inside the session changes to it instead.
//   - A resume may find a tmux session of the task's name left by a pane
//     that was recreated after claude exited (`new-session -A`). Where Resume
//     found no agent running in it, claude is added to it as a new window,
//     which the pane then shows; otherwise the name refuses the resume.
//   - A new task's prompt is written to a mode-0600 temp file and read back
//     by a fixed `sh -c` inside the tmux session, which removes the file
//     before running claude. It reaches claude as the single argument after
//     "--", so a prompt that looks like an option is still a prompt.
//   - A claude resume passes the session ID as "--resume=<id>": `--resume`
//     takes an optional value, and an ID given as a separate argument that
//     began with "-" would be parsed as an option. A codex resume passes it
//     after "--" (`codex resume -- <id>`), and a codex prompt follows "--"
//     too: codex parses an unguarded leading "-" as an option, and
//     `codex resume <id> '-h …'` prints its help and exits 0.
const launchScriptTemplate = `set -u
say() { printf '::panemux-launch %s\n' "$1"; }
agent='{{AGENT}}'
mode='{{MODE}}'
reuse='{{REUSE}}'
sid='{{SESSION_ID}}'
name='{{TMUX_SESSION}}'
cwd=$(cat <<'PANEMUX_CWD_{{TAG}}'
{{CWD}}
PANEMUX_CWD_{{TAG}}
)
command -v tmux >/dev/null 2>&1 || { say 'error no-tmux'; exit 0; }
cd -- "$cwd" 2>/dev/null || { say 'error no-cwd'; exit 0; }
bin=$(command -v "$agent" 2>/dev/null)
case $bin in
/*) ;;
*) bin=$("${SHELL:-/bin/sh}" -lc "command -v $agent" </dev/null 2>/dev/null | tail -n 1) ;;
esac
case $bin in
/*) ;;
*) say "error no-$agent"; exit 0 ;;
esac
[ -f "$bin" ] && [ -x "$bin" ] || { say "error no-$agent"; exit 0; }
{{AGENT_COMMANDS}}
set -- sh
case ${SHELL:-} in
/*)
  case ${SHELL##*/} in
  bash|zsh)
    shpath=$(command -v sh 2>/dev/null)
    case $shpath in
    /*) [ -f "$SHELL" ] && [ -x "$SHELL" ] && set -- "$SHELL" -lic 'exec "$@"' "$SHELL" "$shpath" ;;
    esac
    ;;
  esac
  ;;
esac
if tmux has-session -t "=$name" 2>/dev/null; then
  if [ "$mode" = resume ] && [ "$reuse" = yes ]; then
    tmux new-window -t "=$name:" -- "$@" -c "$resume" sh "$cwd" "$bin" "$sid" 2>/dev/null ||
      { say 'error tmux-failed'; exit 0; }
    say ok
    exit 0
  fi
  say 'error tmux-exists'
  exit 0
fi
if [ "$mode" = resume ]; then
  tmux new-session -d -s "$name" -- "$@" -c "$resume" sh "$cwd" "$bin" "$sid" 2>/dev/null ||
    { say 'error tmux-failed'; exit 0; }
  say ok
  exit 0
fi
umask 077
f=$(mktemp "${TMPDIR:-/tmp}/panemux-task.XXXXXXXX" 2>/dev/null) || { say 'error prompt-file'; exit 0; }
cat >"$f" <<'PANEMUX_PROMPT_{{TAG}}'
{{PROMPT}}
PANEMUX_PROMPT_{{TAG}}
if ! tmux new-session -d -s "$name" -- "$@" -c "$run" sh "$f" "$bin" "$sid" "$cwd" 2>/dev/null; then
  rm -f -- "$f"
  say 'error tmux-failed'
  exit 0
fi
say ok
`

// buildLaunchScript checks every value that goes into the script and fills
// the template. It refuses with ErrInvalidLaunch rather than escaping.
func buildLaunchScript(p launchParams) (string, error) {
	if err := checkLaunchSessionID(p); err != nil {
		return "", err
	}
	if !validTmuxSessionName.MatchString(p.tmuxSession) {
		return "", fmt.Errorf("%w: tmux session name %q", ErrInvalidLaunch, p.tmuxSession)
	}
	if !validHeredocTag.MatchString(p.tag) {
		return "", fmt.Errorf("%w: heredoc tag", ErrInvalidLaunch)
	}
	if err := session.ValidateRemotePath("working directory", p.cwd); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidLaunch, err)
	}
	promptEnd := "PANEMUX_PROMPT_" + p.tag
	if p.mode == launchNew {
		if err := validatePrompt(p.prompt); err != nil {
			return "", err
		}
		for _, line := range strings.Split(p.prompt, "\n") {
			if line == promptEnd {
				return "", fmt.Errorf("%w: the prompt contains its own terminator line", ErrInvalidLaunch)
			}
		}
	}
	return strings.NewReplacer(
		"{{AGENT}}", p.agent,
		"{{MODE}}", string(p.mode),
		"{{REUSE}}", reuseWord(p.reuseSession),
		"{{SESSION_ID}}", p.sessionID,
		"{{TMUX_SESSION}}", p.tmuxSession,
		"{{TAG}}", p.tag,
		"{{CWD}}", p.cwd,
		"{{PROMPT}}", p.prompt,
	).Replace(launchPlan.script), nil
}

// checkLaunchSessionID checks the agent, and the session ID it is given,
// against the agent's adapter: what its newSession mints for a new task —
// the UUID of a new claude task, none for a new codex task, whose ID codex
// picks — and what its resume accepts.
func checkLaunchSessionID(p launchParams) error {
	id := agent.ID(p.agent)
	if p.mode == launchNew {
		a, ok := launchPlan.launch[id]
		switch {
		case !ok:
			return fmt.Errorf("%w: agent %q cannot be launched", ErrInvalidLaunch, p.agent)
		case !a.validNewSessionID(p.sessionID):
			return fmt.Errorf("%w: session ID %q is not one a new %s task is given", ErrInvalidLaunch, p.sessionID, p.agent)
		}
		return nil
	}
	a, ok := launchPlan.resume[id]
	switch {
	case !ok:
		return fmt.Errorf("%w: agent %q cannot be resumed", ErrInvalidLaunch, p.agent)
	case !a.validSessionID(p.sessionID):
		return fmt.Errorf("%w: session ID %q is not one %s resumes", ErrInvalidLaunch, p.sessionID, p.agent)
	}
	return nil
}

func reuseWord(reuse bool) string {
	if reuse {
		return "yes"
	}
	return "no"
}

func validatePrompt(prompt string) error {
	switch {
	case strings.TrimSpace(prompt) == "":
		return fmt.Errorf("%w: the prompt is empty", ErrInvalidLaunch)
	case len(prompt) > maxPromptBytes:
		return fmt.Errorf("%w: the prompt is longer than %d bytes", ErrInvalidLaunch, maxPromptBytes)
	case strings.ContainsRune(prompt, 0):
		return fmt.Errorf("%w: the prompt contains a NUL character", ErrInvalidLaunch)
	}
	return nil
}

// normalizePrompt is what a launch sends: line endings as LF and the
// surrounding blank space dropped, since the heredoc cannot carry a trailing
// newline anyway.
func normalizePrompt(prompt string) string {
	return strings.TrimSpace(strings.ReplaceAll(prompt, "\r\n", "\n"))
}

// parseLaunchOutput finds the launch script's answer. Anything else the host
// printed — a login banner, a profile's echo — is ignored.
func parseLaunchOutput(out []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		answer, ok := strings.CutPrefix(scanner.Text(), "::panemux-launch ")
		if !ok {
			continue
		}
		if answer == "ok" {
			return nil
		}
		if code, isErr := strings.CutPrefix(answer, "error "); isErr {
			return &LaunchError{Code: code}
		}
	}
	return errors.New("the host did not answer the launch")
}

// tmuxSessionForTask names the tmux session a launch or resume creates:
// "task-" and the first eight characters of the session ID. A resumed task
// gets the same name its launch did.
func tmuxSessionForTask(sessionID string) string {
	return "task-" + sessionID[:8]
}

// tmuxSessionForCodex names the tmux session a codex resume creates: "task-"
// and the last eight characters of the session ID. Codex's IDs are version 7
// UUIDs, which begin with their creation time, so two sessions started within
// a minute share their first eight characters; the last eight are random.
func tmuxSessionForCodex(sessionID string) string {
	return "task-" + sessionID[len(sessionID)-8:]
}

// newCodexTmuxSession names a new codex task's tmux session: "task-" and
// eight random hex digits, since codex has no session ID yet.
func newCodexTmuxSession(r io.Reader) (string, error) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", fmt.Errorf("generate tmux session name: %w", err)
	}
	return "task-" + hex.EncodeToString(b[:]), nil
}

// newSessionID mints a version 4 UUID for a new claude session, which the
// launch pins with --session-id so the task — and its labels — are known
// before claude has written anything.
func newSessionID(r io.Reader) (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}

func newHeredocTag(r io.Reader) (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", fmt.Errorf("generate heredoc tag: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func taskID(host, agent, key string) string {
	prefix := "local"
	if host != "" {
		prefix = "ssh:" + host
	}
	return prefix + ":" + agent + ":" + key
}

// Launch starts a new task of an agent that declares launch on a host.
func (s *Service) Launch(ctx context.Context, req LaunchRequest) (Launched, error) {
	id := req.Agent
	if id == "" {
		id = AgentClaude
	}
	adapter, ok := launchPlan.launch[agent.ID(id)]
	if !ok {
		return Launched{}, fmt.Errorf("%w: agent %q cannot be launched", ErrInvalidLaunch, id)
	}
	if err := s.checkHost(req.Host); err != nil {
		return Launched{}, err
	}
	if err := s.refuseMissingCLI(req.Host, agent.ID(id)); err != nil {
		return Launched{}, err
	}
	p := launchParams{agent: id, mode: launchNew, cwd: req.CWD, prompt: normalizePrompt(req.Prompt)}
	var err error
	p.sessionID, p.tmuxSession, err = adapter.newSession(s.opts.Rand)
	if err != nil {
		return Launched{}, err
	}
	return s.launch(ctx, req.Host, p)
}

// Resume reopens a stopped task of an agent that declares resume — `claude
// --resume`, `codex resume` — in the working directory its conversation log
// or rollout records. The session
// must be listed as a stopped task of that agent on the host right now: the
// host is collected again first, so an ID the dashboard did not list is
// never passed to the agent.
func (s *Service) Resume(ctx context.Context, host, agentName, sessionID string) (Launched, error) {
	adapter, ok := launchPlan.resume[agent.ID(agentName)]
	if !ok {
		return Launched{}, fmt.Errorf("%w: agent %q cannot be resumed", ErrInvalidLaunch, agentName)
	}
	if !adapter.validSessionID(sessionID) {
		return Launched{}, fmt.Errorf("%w: session ID %q is not one %s resumes", ErrInvalidLaunch, sessionID, agentName)
	}
	if err := s.checkHost(host); err != nil {
		return Launched{}, err
	}
	if err := s.refuseMissingCLI(host, agent.ID(agentName)); err != nil {
		return Launched{}, err
	}
	// Whether the task's tmux session may be reused is decided from this
	// collection, so a second resume of the same session must not collect
	// until the first has launched: both would find the session free and
	// start claude twice on one conversation.
	unlock, err := s.lockResume(ctx, host, agentName+"\x00"+sessionID)
	if err != nil {
		return Launched{}, err
	}
	defer unlock()
	result, list := s.collectHost(ctx, host)
	if result.Status != HostOK {
		return Launched{}, fmt.Errorf("collect %s before resuming: %s", hostName(host), resultError(result))
	}
	for _, task := range list {
		if task.Agent != agentName || task.SessionID != sessionID || task.State != StateStop {
			continue
		}
		if task.CWD == "" {
			return Launched{}, fmt.Errorf("%w: the session's working directory is not recorded", ErrInvalidLaunch)
		}
		name := adapter.tmuxSessionFor(sessionID)
		return s.launch(ctx, host, launchParams{
			agent: agentName, mode: launchResume, sessionID: sessionID, cwd: task.CWD, tmuxSession: name,
			reuseSession: !agentRunsInTmuxSession(list, name),
		})
	}
	return Launched{}, ErrNoStoppedTask
}

func (s *Service) launch(ctx context.Context, host string, p launchParams) (Launched, error) {
	tag, err := newHeredocTag(s.opts.Rand)
	if err != nil {
		return Launched{}, err
	}
	p.tag = tag
	script, err := buildLaunchScript(p)
	if err != nil {
		return Launched{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, s.opts.HostTimeout)
	defer cancel()
	out, err := s.runHostScript(ctx, host, script, "task launch")
	if err != nil {
		return Launched{}, err
	}
	if err := parseLaunchOutput(out); err != nil {
		return Launched{}, err
	}
	launched := Launched{SessionID: p.sessionID, TmuxSession: p.tmuxSession}
	if p.sessionID != "" {
		launched.TaskID = taskID(host, p.agent, p.sessionID)
	}
	return launched, nil
}

// resumeLock serializes the resumes of one (host, session). users counts
// the holder and the waiters, so the lock is dropped once nobody needs it.
type resumeLock struct {
	held  chan struct{}
	users int
}

// lockResume waits, as long as ctx allows, until no other resume of the same
// session on the same host is between its collection and its launch. It only
// covers this panemux process.
func (s *Service) lockResume(ctx context.Context, host, session string) (func(), error) {
	key := host + "\x00" + session
	lock := s.joinResumeLock(key)
	release := func() {
		s.mu.Lock()
		lock.users--
		if lock.users == 0 {
			delete(s.resumeLocks, key)
		}
		s.mu.Unlock()
	}
	select {
	case lock.held <- struct{}{}:
		return func() {
			<-lock.held
			release()
		}, nil
	case <-ctx.Done():
		release()
		return nil, fmt.Errorf("wait for another resume of the session: %w", ctx.Err())
	}
}

// joinResumeLock returns the key's lock, creating it, and counts the caller
// among its users.
func (s *Service) joinResumeLock(key string) *resumeLock {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock := s.resumeLocks[key]
	if lock == nil {
		lock = &resumeLock{held: make(chan struct{}, 1)}
		s.resumeLocks[key] = lock
	}
	lock.users++
	return lock
}

// agentRunsInTmuxSession reports whether any running task — claude, codex,
// or one whose state could not be read — was collected inside the named tmux
// session. A resume adds itself to a session of its name only when none is.
func agentRunsInTmuxSession(list []Task, name string) bool {
	for _, task := range list {
		if task.Location.Kind == LocationTmux && task.Location.TmuxSession == name {
			return true
		}
	}
	return false
}

// checkHost accepts the panemux host and the configured ssh_connections keys.
func (s *Service) checkHost(host string) error {
	if host == "" {
		return nil
	}
	for _, name := range s.hostNames() {
		if name == host {
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrUnknownHost, host)
}

func hostName(host string) string {
	if host == "" {
		return "the panemux host"
	}
	return host
}

func resultError(result HostResult) string {
	if result.Status == HostConnecting {
		return "still connecting"
	}
	return result.Error
}
