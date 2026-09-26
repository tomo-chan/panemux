package tasks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSessionID = "0f0e0d0c-0b0a-4908-8706-050403020100"
	testTag       = "0123456789abcdef0123456789abcdef"
)

// A prompt that would do harm if any shell re-read it or the claude CLI
// parsed it as options: a leading option, command substitutions, both kinds
// of quote, a heredoc-looking line and a second line.
const hostilePrompt = "--dangerously-skip-permissions $(touch pwned) `touch pwned2` " +
	"'single' \"double\"\nEOF\n; rm -rf ~"

func newParams(t *testing.T, cwd, prompt string) launchParams {
	t.Helper()
	return launchParams{
		mode:        launchNew,
		sessionID:   testSessionID,
		tmuxSession: tmuxSessionForTask(testSessionID),
		cwd:         cwd,
		prompt:      prompt,
		tag:         testTag,
	}
}

// stubHost is a directory of stand-ins for tmux and claude, and the log the
// stand-ins write, so the launch script can run for real under sh without
// either program installed. tmuxDir is set when tmux is the real one, on a
// server of its own (TMUX_TMPDIR).
type stubHost struct {
	bin, log, tmp, tmuxDir string
}

// stubTmux runs the command it is given in HOME, as a tmux started from
// there would, and refuses -c: tmux expands -c's value as a format, so a
// directory holding "#" would silently become the home directory.
const stubTmux = `#!/bin/sh
case "$1" in
has-session)
  [ -e "$STUB_LOG/exists" ] && exit 0
  exit 1 ;;
new-session|new-window)
  printf '%s\0' "$@" > "$STUB_LOG/tmux.args"
  [ -e "$STUB_LOG/fail-new" ] && exit 1
  shift
  while [ $# -gt 0 ]; do
    case "$1" in
    -d) shift ;;
    -s|-t) shift 2 ;;
    --) shift; break ;;
    *) exit 3 ;;
    esac
  done
  cd "$HOME" || exit 4
  "$@"
  exit $? ;;
esac
exit 2
`

const stubClaude = `#!/bin/sh
printf '%s\0' "$@" > "$STUB_LOG/claude.args"
pwd > "$STUB_LOG/claude.pwd"
`

func newStubHost(t *testing.T, withTmux, withClaude bool) stubHost {
	t.Helper()
	root := t.TempDir()
	h := stubHost{bin: filepath.Join(root, "bin"), log: filepath.Join(root, "log"), tmp: filepath.Join(root, "tmp")}
	for _, dir := range []string{h.bin, h.log, h.tmp} {
		require.NoError(t, os.Mkdir(dir, 0o700))
	}
	// PATH holds only this directory, so a tmux or claude installed on the
	// machine running the suite is never found; the few utilities the script
	// uses are linked in.
	for _, tool := range []string{"sh", "cat", "mktemp", "rm", "tail"} {
		path, err := exec.LookPath(tool)
		require.NoError(t, err)
		require.NoError(t, os.Symlink(path, filepath.Join(h.bin, tool)))
	}
	if withTmux {
		writeExecutable(t, filepath.Join(h.bin, "tmux"), stubTmux)
	}
	if withClaude {
		writeExecutable(t, filepath.Join(h.bin, "claude"), stubClaude)
	}
	return h
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o700)) //nolint:gosec // test stand-in must be executable
}

// run runs script as the launch does, `sh -s` with the script on stdin, with
// only the stubs' directory on PATH.
func (h stubHost) run(t *testing.T, script, shell string) []byte {
	t.Helper()
	cmd := exec.Command("sh", "-s")
	cmd.Stdin = strings.NewReader(script)
	cmd.Dir = h.tmp
	cmd.Env = []string{
		"PATH=" + h.bin,
		"STUB_LOG=" + h.log,
		"TMPDIR=" + h.tmp,
		"HOME=" + h.tmp,
		"SHELL=" + shell,
	}
	if h.tmuxDir != "" {
		cmd.Env = append(cmd.Env, "TMUX_TMPDIR="+h.tmuxDir)
	}
	out, err := cmd.Output()
	require.NoError(t, err)
	return out
}

// args reads the arguments a stand-in recorded, NUL-separated so an
// argument holding a newline stays one argument.
func (h stubHost) args(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.log, name))
	require.NoError(t, err)
	return strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
}

func (h stubHost) lines(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.log, name))
	require.NoError(t, err)
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func (h stubHost) leftoverFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(h.tmp)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestLaunchScript_NewTaskHandsThePromptToClaudeAsOneArgumentAfterTheEndOfOptions(t *testing.T) {
	h := newStubHost(t, true, true)
	cwd := t.TempDir()
	script, err := buildLaunchScript(newParams(t, cwd, hostilePrompt))
	require.NoError(t, err)

	out := h.run(t, script, "/bin/false")

	require.NoError(t, parseLaunchOutput(out))
	assert.Equal(t, []string{"--session-id=" + testSessionID, "--", hostilePrompt}, h.args(t, "claude.args"))
	assert.Equal(t, []string{cwd}, h.lines(t, "claude.pwd"))
	tmuxArgs := h.args(t, "tmux.args")
	require.GreaterOrEqual(t, len(tmuxArgs), 8)
	assert.Equal(t, []string{"new-session", "-d", "-s", "task-0f0e0d0c", "--", "sh"}, tmuxArgs[:6])
	assert.NotContains(t, strings.Join(tmuxArgs, "\n"), "touch pwned", "the prompt must not reach tmux's arguments")
	assert.Empty(t, h.leftoverFiles(t), "the prompt file is removed once claude has read it")
	assert.NoFileExists(t, filepath.Join(cwd, "pwned"))
	assert.NoFileExists(t, filepath.Join(h.tmp, "pwned"))
}

func TestLaunchScript_ResumeRunsClaudeWithTheSessionIDInTheEqualsForm(t *testing.T) {
	h := newStubHost(t, true, true)
	cwd := t.TempDir()
	p := newParams(t, cwd, "")
	p.mode = launchResume
	script, err := buildLaunchScript(p)
	require.NoError(t, err)

	require.NoError(t, parseLaunchOutput(h.run(t, script, "/bin/false")))

	assert.Equal(t, []string{"--resume=" + testSessionID}, h.args(t, "claude.args"))
	assert.Equal(t, []string{cwd}, h.lines(t, "claude.pwd"))
	tmuxArgs := h.args(t, "tmux.args")
	assert.Equal(t, "task-0f0e0d0c", tmuxArgs[3])
	assert.Empty(t, h.leftoverFiles(t))
}

func TestLaunchScript_FindsClaudeThroughTheLoginShellWhenPathLacksIt(t *testing.T) {
	h := newStubHost(t, true, false)
	elsewhere := t.TempDir()
	claude := filepath.Join(elsewhere, "claude")
	writeExecutable(t, claude, stubClaude)
	// A login shell whose profile prints something before the answer.
	shell := filepath.Join(elsewhere, "login-shell")
	writeExecutable(t, shell, "#!/bin/sh\necho 'Welcome to the host'\necho '"+claude+"'\n")
	script, err := buildLaunchScript(newParams(t, t.TempDir(), "hello"))
	require.NoError(t, err)

	require.NoError(t, parseLaunchOutput(h.run(t, script, shell)))
	assert.Equal(t, []string{"--session-id=" + testSessionID, "--", "hello"}, h.args(t, "claude.args"))
}

// hostRefusal is one way a host refuses a launch, set up on a stubHost.
type hostRefusal struct {
	setup    func(t *testing.T, h stubHost)
	cwd      func(t *testing.T) string
	name     string
	shell    string
	wantCode string
	withTmux bool
}

func hostRefusals() []hostRefusal {
	return []hostRefusal{
		{name: "no tmux", withTmux: false, wantCode: RefusedNoTmux},
		{
			name: "missing working directory", withTmux: true, wantCode: RefusedNoCWD,
			cwd: func(t *testing.T) string { return filepath.Join(t.TempDir(), "gone") },
		},
		{
			name: "no claude on PATH or through the login shell", withTmux: true, shell: "/bin/false",
			wantCode: RefusedNoClaude,
		},
		{
			name: "login shell answers something that is not an absolute path", withTmux: true, wantCode: RefusedNoClaude,
			setup: func(t *testing.T, h stubHost) {
				writeExecutable(t, filepath.Join(h.tmp, "sh-alias"), "#!/bin/sh\necho 'claude: aliased to npx claude'\n")
			},
			shell: "alias",
		},
		{
			name: "login shell names a file that cannot run", withTmux: true, wantCode: RefusedNoClaude,
			setup: func(t *testing.T, h stubHost) {
				require.NoError(t, os.WriteFile(filepath.Join(h.tmp, "claude-noexec"), nil, 0o600))
				noexec := filepath.Join(h.tmp, "claude-noexec")
				writeExecutable(t, filepath.Join(h.tmp, "sh-noexec"), "#!/bin/sh\necho '"+noexec+"'\n")
			},
			shell: "noexec",
		},
		{
			name: "tmux session already exists", withTmux: true, wantCode: RefusedTmuxExists,
			setup: func(t *testing.T, h stubHost) {
				require.NoError(t, os.WriteFile(filepath.Join(h.log, "exists"), nil, 0o600))
			},
		},
		{
			name: "tmux fails to start the session", withTmux: true, wantCode: RefusedTmuxFailed,
			setup: func(t *testing.T, h stubHost) {
				require.NoError(t, os.WriteFile(filepath.Join(h.log, "fail-new"), nil, 0o600))
			},
		},
	}
}

func TestLaunchScript_HostRefusals(t *testing.T) {
	for _, tt := range hostRefusals() {
		t.Run(tt.name, func(t *testing.T) { checkHostRefusal(t, tt) })
	}
}

// checkHostRefusal runs the launch on a host set up to refuse it, and
// requires the refusal's code, no claude run, and no prompt file left.
func checkHostRefusal(t *testing.T, tt hostRefusal) {
	t.Helper()
	withClaude := tt.wantCode != RefusedNoClaude
	h := newStubHost(t, tt.withTmux, withClaude)
	if tt.setup != nil {
		tt.setup(t, h)
	}
	shell := tt.shell
	switch shell {
	case "":
		shell = "/bin/false"
	case "alias", "noexec":
		shell = filepath.Join(h.tmp, "sh-"+shell)
	}
	cwd := t.TempDir()
	if tt.cwd != nil {
		cwd = tt.cwd(t)
	}
	script, err := buildLaunchScript(newParams(t, cwd, "hello"))
	require.NoError(t, err)

	err = parseLaunchOutput(h.run(t, script, shell))

	var launchErr *LaunchError
	require.ErrorAs(t, err, &launchErr)
	assert.Equal(t, tt.wantCode, launchErr.Code)
	assert.NoFileExists(t, filepath.Join(h.log, "claude.args"))
	for _, name := range h.leftoverFiles(t) {
		assert.False(t, strings.HasPrefix(name, "panemux-task."), "prompt file %s left behind", name)
	}
}

func TestBuildLaunchScript_RefusesInputBeforeAnythingRuns(t *testing.T) {
	long := strings.Repeat("a", maxPromptBytes+1)
	tests := []struct {
		mutate func(p *launchParams)
		name   string
	}{
		{name: "session id that is not a UUID", mutate: func(p *launchParams) { p.sessionID = "--help" }},
		{name: "session id with a title-like value", mutate: func(p *launchParams) { p.sessionID = "my-session" }},
		{name: "tmux session name with a shell character", mutate: func(p *launchParams) { p.tmuxSession = "task;id" }},
		{name: "relative working directory", mutate: func(p *launchParams) { p.cwd = "workspace/project" }},
		{name: "empty working directory", mutate: func(p *launchParams) { p.cwd = "" }},
		{name: "working directory with a shell metacharacter", mutate: func(p *launchParams) { p.cwd = "/tmp/$(id)" }},
		{name: "working directory with a quote", mutate: func(p *launchParams) { p.cwd = "/tmp/it's" }},
		{name: "working directory with a newline", mutate: func(p *launchParams) { p.cwd = "/tmp/a\nb" }},
		{name: "blank prompt", mutate: func(p *launchParams) { p.prompt = " \n\t" }},
		{name: "prompt over the size limit", mutate: func(p *launchParams) { p.prompt = long }},
		{name: "prompt with a NUL", mutate: func(p *launchParams) { p.prompt = "a\x00b" }},
		{
			name:   "prompt containing its own heredoc terminator line",
			mutate: func(p *launchParams) { p.prompt = "a\nPANEMUX_PROMPT_" + testTag + "\nb" },
		},
		{name: "tag that is not hex", mutate: func(p *launchParams) { p.tag = "x y" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParams(t, "/workspace/user/project", "hello")
			tt.mutate(&p)
			_, err := buildLaunchScript(p)
			assert.ErrorIs(t, err, ErrInvalidLaunch)
		})
	}
}

func TestBuildLaunchScript_AcceptsTheLimits(t *testing.T) {
	p := newParams(t, "/workspace/user/my project", strings.Repeat("a", maxPromptBytes))
	_, err := buildLaunchScript(p)
	require.NoError(t, err)

	p = newParams(t, "/workspace/user/project", "")
	p.mode = launchResume
	_, err = buildLaunchScript(p)
	require.NoError(t, err, "a resume carries no prompt")
}

func TestParseLaunchOutput(t *testing.T) {
	tests := []struct {
		name     string
		out      string
		wantCode string
		wantErr  bool
	}{
		{name: "ok after login shell noise", out: "Last login: today\n::panemux-launch ok\n"},
		{name: "host refusal", out: "::panemux-launch error no-cwd\n", wantCode: RefusedNoCWD},
		{name: "unknown refusal code", out: "::panemux-launch error something\n", wantCode: "something"},
		{name: "no answer at all", out: "motd\n", wantErr: true},
		{name: "empty", out: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseLaunchOutput([]byte(tt.out))
			switch {
			case tt.wantCode != "":
				var launchErr *LaunchError
				require.ErrorAs(t, err, &launchErr)
				assert.Equal(t, tt.wantCode, launchErr.Code)
			case tt.wantErr:
				require.Error(t, err)
				var launchErr *LaunchError
				assert.False(t, errors.As(err, &launchErr))
			default:
				assert.NoError(t, err)
			}
		})
	}
}

func TestLaunchError_MessagesAreFixedPerCode(t *testing.T) {
	for code, want := range map[string]string{
		RefusedNoTmux:     "tmux is not installed on the host",
		RefusedNoCWD:      "the working directory does not exist on the host",
		RefusedNoClaude:   "claude was not found on the host",
		RefusedTmuxExists: "a tmux session with the task's name already exists on the host",
		RefusedTmuxFailed: "tmux could not start the session on the host",
		RefusedPromptFile: "the prompt could not be written to a temporary file on the host",
		"other":           `the host refused the launch ("other")`,
	} {
		assert.Equal(t, want, (&LaunchError{Code: code}).Error(), code)
	}
}

func TestTmuxSessionForTask(t *testing.T) {
	assert.Equal(t, "task-0f0e0d0c", tmuxSessionForTask(testSessionID))
	assert.Regexp(t, validTmuxSessionName, tmuxSessionForTask(testSessionID))
}

func TestNewSessionID_IsAVersion4UUID(t *testing.T) {
	id, err := newSessionID(bytes.NewReader(bytes.Repeat([]byte{0xff}, 16)))
	require.NoError(t, err)
	assert.Equal(t, "ffffffff-ffff-4fff-bfff-ffffffffffff", id)
	assert.Regexp(t, validUUID, id)

	_, err = newSessionID(bytes.NewReader(nil))
	assert.Error(t, err)
}

// launchRand is a random source for the service: 16 bytes of session ID,
// then 16 of heredoc tag.
func launchRand() io.Reader {
	return bytes.NewReader(append(
		[]byte{0x0f, 0x0e, 0x0d, 0x0c, 0x0b, 0x0a, 0x09, 0x08, 0x87, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, 0x00},
		bytes.Repeat([]byte{0xab}, 16)...,
	))
}

func TestLaunch_LocalRunsTheScriptOnThePanemuxHost(t *testing.T) {
	var scripts []string
	svc := New(Options{
		Rand: launchRand(),
		RunLocal: func(_ context.Context, script string) ([]byte, error) {
			scripts = append(scripts, script)
			return []byte("::panemux-launch ok\n"), nil
		},
	})
	defer svc.Close()

	req := LaunchRequest{CWD: "/workspace/user/project", Prompt: "  fix the tests\n"}
	got, err := svc.Launch(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, Launched{
		TaskID: "local:claude:" + testSessionID, SessionID: testSessionID, TmuxSession: "task-0f0e0d0c",
	}, got)
	require.Len(t, scripts, 1)
	assert.Contains(t, scripts[0], "\nfix the tests\nPANEMUX_PROMPT_abababababababababababababababab\n",
		"the prompt is trimmed and sits in the tagged heredoc")
	assert.Contains(t, scripts[0], "mode='new'")
}

func TestLaunch_RemoteRunsTheScriptOverTheHostConnection(t *testing.T) {
	conn := &fakeConn{output: []byte("::panemux-launch ok\n")}
	dialer := &fakeDialer{conns: []*fakeConn{conn}}
	svc := New(Options{Rand: launchRand(), Hosts: func() []string { return []string{"build-box"} }, Dial: dialer.dial})
	defer svc.Close()

	got, err := svc.Launch(context.Background(), LaunchRequest{Host: "build-box", CWD: "/remote/home/demo", Prompt: "go"})

	require.NoError(t, err)
	assert.Equal(t, "ssh:build-box:claude:"+testSessionID, got.TaskID)
	assert.Equal(t, []string{"sh -s"}, conn.cmds, "the remote login shell parses only the literal sh -s")
	require.Len(t, conn.stdins, 1)
	assert.Contains(t, conn.stdins[0], "cwd=$(cat <<'PANEMUX_CWD_")
}

func TestLaunch_Errors(t *testing.T) {
	t.Run("unknown host", func(t *testing.T) {
		svc := New(Options{Rand: launchRand(), Hosts: func() []string { return []string{"build-box"} }})
		defer svc.Close()
		_, err := svc.Launch(context.Background(), LaunchRequest{Host: "other", CWD: "/w", Prompt: "go"})
		assert.ErrorIs(t, err, ErrUnknownHost)
	})
	t.Run("invalid input runs nothing", func(t *testing.T) {
		ran := false
		svc := New(Options{Rand: launchRand(), RunLocal: func(context.Context, string) ([]byte, error) {
			ran = true
			return nil, nil
		}})
		defer svc.Close()
		_, err := svc.Launch(context.Background(), LaunchRequest{CWD: "relative", Prompt: "go"})
		assert.ErrorIs(t, err, ErrInvalidLaunch)
		assert.False(t, ran)
	})
	t.Run("random source fails", func(t *testing.T) {
		svc := New(Options{Rand: bytes.NewReader(nil)})
		defer svc.Close()
		_, err := svc.Launch(context.Background(), LaunchRequest{CWD: "/w", Prompt: "go"})
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrInvalidLaunch)
	})
	t.Run("random source fails on the tag", func(t *testing.T) {
		svc := New(Options{Rand: bytes.NewReader(make([]byte, 16))})
		defer svc.Close()
		_, err := svc.Launch(context.Background(), LaunchRequest{CWD: "/w", Prompt: "go"})
		require.Error(t, err)
	})
	t.Run("host refusal", func(t *testing.T) {
		svc := New(Options{Rand: launchRand(), RunLocal: localOutput([]byte("::panemux-launch error tmux-exists\n"), nil)})
		defer svc.Close()
		_, err := svc.Launch(context.Background(), LaunchRequest{CWD: "/w", Prompt: "go"})
		var launchErr *LaunchError
		require.ErrorAs(t, err, &launchErr)
		assert.Equal(t, RefusedTmuxExists, launchErr.Code)
	})
	t.Run("local run fails", func(t *testing.T) {
		svc := New(Options{Rand: launchRand(), RunLocal: localOutput(nil, errors.New("boom"))})
		defer svc.Close()
		_, err := svc.Launch(context.Background(), LaunchRequest{CWD: "/w", Prompt: "go"})
		assert.ErrorContains(t, err, "boom")
	})
	t.Run("a transport failure drops the connection", func(t *testing.T) {
		conn := &fakeConn{runErr: errors.New("connection reset")}
		dialer := &fakeDialer{conns: []*fakeConn{conn}}
		svc := New(Options{Rand: launchRand(), Hosts: func() []string { return []string{"build-box"} }, Dial: dialer.dial})
		defer svc.Close()
		_, err := svc.Launch(context.Background(), LaunchRequest{Host: "build-box", CWD: "/w", Prompt: "go"})
		assert.ErrorContains(t, err, "task launch on build-box")
		assert.Equal(t, 1, conn.closeCount())
	})
	t.Run("a dial failure", func(t *testing.T) {
		dialer := &fakeDialer{errs: []error{errors.New("no route")}}
		svc := New(Options{Rand: launchRand(), Hosts: func() []string { return []string{"build-box"} }, Dial: dialer.dial})
		defer svc.Close()
		_, err := svc.Launch(context.Background(), LaunchRequest{Host: "build-box", CWD: "/w", Prompt: "go"})
		assert.ErrorContains(t, err, "no route")
	})
}

// stoppedOutput is a collection holding one stopped session, whose
// transcript records cwd, and one running one.
func stoppedOutput(stoppedID, cwd, runningID string) []byte {
	return joinLines(
		"::panemux-tasks v1",
		"::now 1000",
		"::section state",
		"::file 7.json",
		`{"pid":7,"sessionId":"`+runningID+`","status":"busy","statusUpdatedAt":999000}`,
		"::section ps",
		"7 1 claude",
		"::section tmux",
		"::section cwd",
		"::section transcripts",
		"990\t"+stoppedID+".jsonl\t"+`"cwd":"`+cwd+`"`,
		"995\t"+runningID+".jsonl\t"+`"cwd":"/workspace/user/other"`,
		"::end",
	)
}

func TestResume_RunsClaudeForAStoppedTaskInItsRecordedDirectory(t *testing.T) {
	runningID := "11111111-2222-4333-8444-555555555555"
	var scripts []string
	svc := New(Options{
		Now: func() time.Time { return time.Unix(1000, 0) },
		RunLocal: func(_ context.Context, script string) ([]byte, error) {
			scripts = append(scripts, script)
			if script == collectScript {
				return stoppedOutput(testSessionID, "/workspace/user/project", runningID), nil
			}
			return []byte("::panemux-launch ok\n"), nil
		},
		Rand: launchRand(),
	})
	defer svc.Close()

	got, err := svc.Resume(context.Background(), "", testSessionID)

	require.NoError(t, err)
	assert.Equal(t, Launched{
		TaskID: "local:claude:" + testSessionID, SessionID: testSessionID, TmuxSession: "task-0f0e0d0c",
	}, got)
	require.Len(t, scripts, 2)
	assert.Contains(t, scripts[1], "mode='resume'")
	assert.Contains(t, scripts[1], "\n/workspace/user/project\nPANEMUX_CWD_")
}

// resumeRefusal is a resume that must be refused before anything launches.
type resumeRefusal struct {
	wantIs    error
	runLocal  func(context.Context, string) ([]byte, error)
	name      string
	host      string
	sessionID string
	wantText  string
}

func TestResume_Refusals(t *testing.T) {
	runningID := "11111111-2222-4333-8444-555555555555"
	collection := func(out []byte, err error) func(context.Context, string) ([]byte, error) {
		return func(_ context.Context, script string) ([]byte, error) {
			if script != collectScript {
				t.Fatalf("launched although the resume should have been refused")
			}
			return out, err
		}
	}
	tests := []resumeRefusal{
		{
			name: "session id that is not a UUID", sessionID: "my-title",
			runLocal: collection(nil, nil), wantIs: ErrInvalidLaunch,
		},
		{
			name: "unknown host", host: "other", sessionID: testSessionID,
			runLocal: collection(nil, nil), wantIs: ErrUnknownHost,
		},
		{
			name: "running task", sessionID: runningID,
			runLocal: collection(stoppedOutput(testSessionID, "/w", runningID), nil), wantIs: ErrNoStoppedTask,
		},
		{
			name: "session not listed", sessionID: "99999999-2222-4333-8444-555555555555",
			runLocal: collection(stoppedOutput(testSessionID, "/w", runningID), nil), wantIs: ErrNoStoppedTask,
		},
		{
			name: "stopped task with no recorded directory", sessionID: testSessionID,
			runLocal: collection(joinLines(
				"::panemux-tasks v1", "::now 1000", "::section transcripts",
				"990\t"+testSessionID+".jsonl", "::end",
			), nil),
			wantIs: ErrInvalidLaunch,
		},
		{
			name: "stopped task whose recorded directory has a shell character", sessionID: testSessionID,
			runLocal: collection(stoppedOutput(testSessionID, "/w/$(id)", runningID), nil), wantIs: ErrInvalidLaunch,
		},
		{
			name: "collection fails", sessionID: testSessionID,
			runLocal: collection(nil, errors.New("boom")), wantText: "boom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { checkResumeRefusal(t, tt) })
	}
}

func checkResumeRefusal(t *testing.T, tt resumeRefusal) {
	t.Helper()
	svc := New(Options{
		Now: func() time.Time { return time.Unix(1000, 0) }, RunLocal: tt.runLocal, Rand: launchRand(),
		Hosts: func() []string { return []string{"build-box"} },
	})
	defer svc.Close()
	_, err := svc.Resume(context.Background(), tt.host, tt.sessionID)
	if tt.wantIs != nil {
		assert.ErrorIs(t, err, tt.wantIs)
	}
	if tt.wantText != "" {
		assert.ErrorContains(t, err, tt.wantText)
	}
}

func TestResume_AHostStillConnectingIsNotResumed(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	dialer := &fakeDialer{gate: gate, conns: []*fakeConn{{}}}
	svc := New(Options{
		Rand: launchRand(), Hosts: func() []string { return []string{"build-box"} }, Dial: dialer.dial,
		HostTimeout: 10 * time.Millisecond,
	})
	defer svc.Close()

	_, err := svc.Resume(context.Background(), "build-box", testSessionID)

	assert.EqualError(t, err, "collect build-box before resuming: still connecting")
}

// newRealTmuxHost is a stubHost whose tmux is the machine's own, run on a
// server of its own that is killed when the test ends. It skips the test
// where tmux is not installed.
func newRealTmuxHost(t *testing.T) stubHost {
	t.Helper()
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	h := newStubHost(t, false, true)
	require.NoError(t, os.Symlink(tmux, filepath.Join(h.bin, "tmux")))
	// A socket path has a length limit, so the server directory is kept short.
	h.tmuxDir, err = os.MkdirTemp("", "tmx")
	require.NoError(t, err)
	t.Cleanup(func() {
		kill := exec.Command(tmux, "kill-server")
		kill.Env = []string{"TMUX_TMPDIR=" + h.tmuxDir}
		_ = kill.Run()
		_ = os.RemoveAll(h.tmuxDir)
	})
	return h
}

// tmux runs the real tmux against the host's own server.
func (h stubHost) tmux(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(filepath.Join(h.bin, "tmux"), args...) //nolint:gosec // this test's own tmux server
	cmd.Env = []string{"TMUX_TMPDIR=" + h.tmuxDir, "PATH=" + h.bin, "HOME=" + h.tmp, "STUB_LOG=" + h.log}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

// waitForClaude waits for the stand-in claude, which tmux runs detached, to
// record its arguments and directory.
func (h stubHost) waitForClaude(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(h.log, "claude.pwd"))
		return err == nil
	}, 5*time.Second, 20*time.Millisecond, "claude never ran")
}

// A working directory holding "#" is a format to tmux's -c: "#S" becomes the
// session name, "##" one "#", and a directory that then does not exist makes
// tmux start in the home directory while reporting success. The launch must
// start claude in the directory as typed, for a new task and a resume alike.
func TestLaunchScript_RealTmuxStartsClaudeInADirectoryHoldingAHash(t *testing.T) {
	for _, mode := range []launchMode{launchNew, launchResume} {
		t.Run(string(mode), func(t *testing.T) {
			h := newRealTmuxHost(t)
			cwd := filepath.Join(t.TempDir(), "proj#S ##x")
			require.NoError(t, os.Mkdir(cwd, 0o700))
			p := newParams(t, cwd, "hello")
			if mode == launchResume {
				p = newParams(t, cwd, "")
				p.mode = launchResume
			}
			script, err := buildLaunchScript(p)
			require.NoError(t, err)

			require.NoError(t, parseLaunchOutput(h.run(t, script, "/bin/false")))

			h.waitForClaude(t)
			assert.Equal(t, []string{cwd}, h.lines(t, "claude.pwd"))
		})
	}
}

// A pane that attached to a task's tmux session is recreated with
// `new-session -A`, which leaves a session of that name holding a shell once
// claude has exited. A resume may then add claude as a new window of that
// session, so the pane shows it; the shell's window is left alone.
func TestLaunchScript_RealTmuxResumeAddsAWindowToASessionLeftByAPane(t *testing.T) {
	h := newRealTmuxHost(t)
	cwd := t.TempDir()
	sleep, err := exec.LookPath("sleep")
	require.NoError(t, err)
	h.tmux(t, "new-session", "-d", "-s", "task-0f0e0d0c", "--", sleep, "60")
	p := newParams(t, cwd, "")
	p.mode = launchResume
	p.reuseSession = true
	script, err := buildLaunchScript(p)
	require.NoError(t, err)

	require.NoError(t, parseLaunchOutput(h.run(t, script, "/bin/false")))

	h.waitForClaude(t)
	assert.Equal(t, []string{"--resume=" + testSessionID}, h.args(t, "claude.args"))
	assert.Equal(t, []string{cwd}, h.lines(t, "claude.pwd"))
	// The stand-in exits once it has recorded itself, closing its window; the
	// window the session already had is still there, untouched.
	commands := strings.Fields(h.tmux(t, "list-panes", "-s", "-t", "=task-0f0e0d0c", "-F", "#{pane_current_command}"))
	assert.Equal(t, []string{"sleep"}, commands)
}

func TestLaunchScript_ASessionOfTheSameNameIsReusedOnlyForAPermittedResume(t *testing.T) {
	tests := []struct {
		name     string
		mode     launchMode
		wantCode string
		reuse    bool
	}{
		{name: "resume permitted to reuse", mode: launchResume, reuse: true},
		{name: "resume not permitted", mode: launchResume, wantCode: RefusedTmuxExists},
		{name: "a new task never reuses", mode: launchNew, reuse: true, wantCode: RefusedTmuxExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newStubHost(t, true, true)
			require.NoError(t, os.WriteFile(filepath.Join(h.log, "exists"), nil, 0o600))
			cwd := t.TempDir()
			p := newParams(t, cwd, "hello")
			if tt.mode == launchResume {
				p = newParams(t, cwd, "")
			}
			p.mode, p.reuseSession = tt.mode, tt.reuse
			script, err := buildLaunchScript(p)
			require.NoError(t, err)

			err = parseLaunchOutput(h.run(t, script, "/bin/false"))

			if tt.wantCode != "" {
				var launchErr *LaunchError
				require.ErrorAs(t, err, &launchErr)
				assert.Equal(t, tt.wantCode, launchErr.Code)
				assert.NoFileExists(t, filepath.Join(h.log, "claude.args"))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []string{"new-window", "-t", "=task-0f0e0d0c:", "--", "sh"}, h.args(t, "tmux.args")[:5])
			assert.Equal(t, []string{"--resume=" + testSessionID}, h.args(t, "claude.args"))
			assert.Equal(t, []string{cwd}, h.lines(t, "claude.pwd"))
		})
	}
}

// inTmuxOutput is a collection with the stopped session testSessionID and,
// when occupant is set, a running claude session inside the tmux session
// the resume would use.
func inTmuxOutput(occupant bool) []byte {
	lines := []string{"::panemux-tasks v1", "::now 1000", "::section state"}
	if occupant {
		lines = append(lines, "::file 7.json",
			`{"pid":7,"sessionId":"11111111-2222-4333-8444-555555555555","status":"idle","statusUpdatedAt":999000}`)
	}
	lines = append(lines, "::section ps")
	if occupant {
		lines = append(lines, "6 1 -bash", "7 6 claude")
	}
	lines = append(lines, "::section tmux")
	if occupant {
		lines = append(lines, "6 task-0f0e0d0c")
	}
	lines = append(lines, "::section cwd", "::section transcripts",
		"990\t"+testSessionID+".jsonl\t"+`"cwd":"/workspace/user/project"`, "::end")
	return joinLines(lines...)
}

func TestResume_ReusesTheTaskSessionOnlyWhenNoAgentRunsInIt(t *testing.T) {
	for _, occupant := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "an agent runs in it"}[occupant], func(t *testing.T) {
			var launchScript string
			svc := New(Options{
				Now: func() time.Time { return time.Unix(1000, 0) }, Rand: launchRand(),
				RunLocal: func(_ context.Context, script string) ([]byte, error) {
					if script == collectScript {
						return inTmuxOutput(occupant), nil
					}
					launchScript = script
					return []byte("::panemux-launch ok\n"), nil
				},
			})
			defer svc.Close()

			_, err := svc.Resume(context.Background(), "", testSessionID)

			require.NoError(t, err)
			want := map[bool]string{false: "reuse='yes'", true: "reuse='no'"}[occupant]
			assert.Contains(t, launchScript, want)
		})
	}
}

// Two resumes of one task must not both decide, from collections made before
// either started claude, that the task's tmux session is free: the second
// would add a second `claude --resume` of the same conversation to it. The
// collection and the launch of a resume are one step per (host, session).
func TestResume_OverlappingResumesOfOneTaskAreSerialized(t *testing.T) {
	var (
		mu          sync.Mutex
		launched    bool
		collections int
		scripts     []string
	)
	firstLaunch := make(chan struct{})
	release := make(chan struct{})
	svc := New(Options{
		Now: func() time.Time { return time.Unix(1000, 0) }, Rand: launchRand(),
		RunLocal: func(_ context.Context, script string) ([]byte, error) {
			mu.Lock()
			if script == collectScript {
				collections++
				out := inTmuxOutput(launched)
				mu.Unlock()
				return out, nil
			}
			scripts = append(scripts, script)
			first := len(scripts) == 1
			mu.Unlock()
			if first {
				close(firstLaunch)
				<-release
			}
			mu.Lock()
			launched = true
			mu.Unlock()
			return []byte("::panemux-launch ok\n"), nil
		},
	})
	defer svc.Close()

	errs := make(chan error, 2)
	go func() { _, err := svc.Resume(context.Background(), "", testSessionID); errs <- err }()
	<-firstLaunch
	go func() { _, err := svc.Resume(context.Background(), "", testSessionID); errs <- err }()
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, 1, collections, "the second resume collects only once the first has launched")
	mu.Unlock()
	close(release)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)

	require.Len(t, scripts, 2)
	assert.Contains(t, scripts[0], "reuse='yes'")
	assert.Contains(t, scripts[1], "reuse='no'", "the second resume sees the first one's claude in the session")
}

// A resume whose request ends while it waits gives up without taking the lock
// from the resume that holds it: a third resume still waits for the holder.
func TestResume_GivesUpWaitingForAnotherResumeWhenItsRequestEnds(t *testing.T) {
	var (
		mu          sync.Mutex
		collections int
		launches    int
	)
	firstLaunch := make(chan struct{})
	release := make(chan struct{})
	svc := New(Options{
		Now: func() time.Time { return time.Unix(1000, 0) }, Rand: launchRand(),
		RunLocal: func(_ context.Context, script string) ([]byte, error) {
			mu.Lock()
			if script == collectScript {
				collections++
				mu.Unlock()
				return inTmuxOutput(false), nil
			}
			launches++
			first := launches == 1
			mu.Unlock()
			if first {
				close(firstLaunch)
				<-release
			}
			return []byte("::panemux-launch ok\n"), nil
		},
	})
	defer svc.Close()
	done := make(chan error, 2)
	go func() { _, err := svc.Resume(context.Background(), "", testSessionID); done <- err }()
	<-firstLaunch

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.Resume(ctx, "", testSessionID)
	require.ErrorIs(t, err, context.Canceled)

	go func() { _, err := svc.Resume(context.Background(), "", testSessionID); done <- err }()
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, 1, collections, "the third resume still waits for the one holding the lock")
	mu.Unlock()
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	svc.mu.Lock()
	assert.Empty(t, svc.resumeLocks, "a lock nobody holds or waits for is dropped")
	svc.mu.Unlock()
}
