package tasks

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const stubCodex = `#!/bin/sh
printf '%s\0' "$@" > "$STUB_LOG/codex.args"
pwd > "$STUB_LOG/codex.pwd"
printf '%s\n' "$PATH" > "$STUB_LOG/codex.path"
`

// withCodex adds a stand-in codex to the host, in a directory of its own as
// an npm install's bin directory is.
func (h stubHost) withCodex(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(h.bin), "npm-bin")
	require.NoError(t, os.Mkdir(dir, 0o700))
	writeExecutable(t, filepath.Join(dir, "codex"), stubCodex)
	return dir
}

func codexParams(t *testing.T, mode launchMode, cwd, prompt string) launchParams {
	t.Helper()
	p := launchParams{agent: AgentCodex, mode: mode, cwd: cwd, prompt: prompt, tag: testTag}
	if mode == launchResume {
		p.sessionID = codexSessionA
		p.tmuxSession = tmuxSessionForCodex(codexSessionA)
	} else {
		p.tmuxSession = "task-0a1b2c3d"
	}
	return p
}

// A new codex task: the first instruction is codex's single argument after
// "--" (codex reads an unguarded leading "-" as an option), the update check
// that would hold the TUI at a prompt is turned off, and codex starts in the
// working directory with its own directory first on PATH — an npm install's
// codex is a node script, and node sits next to it there.
func TestLaunchScript_NewCodexTask(t *testing.T) {
	h := newStubHost(t, true, false)
	codexDir := h.withCodex(t)
	cwd := t.TempDir()
	script, err := buildLaunchScript(codexParams(t, launchNew, cwd, hostilePrompt))
	require.NoError(t, err)

	shell := filepath.Join(h.tmp, "login-shell")
	writeExecutable(t, shell, "#!/bin/sh\necho '"+filepath.Join(codexDir, "codex")+"'\n")
	require.NoError(t, parseLaunchOutput(h.run(t, script, shell)))

	assert.Equal(t, []string{"-c", "check_for_update_on_startup=false", "--", hostilePrompt}, h.args(t, "codex.args"))
	assert.Equal(t, []string{cwd}, h.lines(t, "codex.pwd"))
	assert.Equal(t, []string{codexDir + ":" + h.bin}, h.lines(t, "codex.path"))
	tmuxArgs := h.args(t, "tmux.args")
	assert.Equal(t, []string{"new-session", "-d", "-s", "task-0a1b2c3d", "--", "sh"}, tmuxArgs[:6])
	assert.NotContains(t, strings.Join(tmuxArgs, "\n"), "touch pwned")
	assert.Empty(t, h.leftoverFiles(t)[1:], "only the login shell stand-in is left: %v", h.leftoverFiles(t))
	assert.NoFileExists(t, filepath.Join(h.log, "claude.args"))
}

// Resuming passes the session ID after "--": without it, `codex resume <id>
// '-h …'` prints its help and exits 0.
func TestLaunchScript_ResumeCodexTask(t *testing.T) {
	h := newStubHost(t, true, false)
	codexDir := h.withCodex(t)
	require.NoError(t, os.Symlink(filepath.Join(codexDir, "codex"), filepath.Join(h.bin, "codex")))
	cwd := t.TempDir()
	script, err := buildLaunchScript(codexParams(t, launchResume, cwd, ""))
	require.NoError(t, err)

	require.NoError(t, parseLaunchOutput(h.run(t, script, "/bin/false")))

	assert.Equal(t, []string{"-c", "check_for_update_on_startup=false", "resume", "--", codexSessionA},
		h.args(t, "codex.args"))
	assert.Equal(t, []string{cwd}, h.lines(t, "codex.pwd"))
	assert.Equal(t, []string{h.bin + ":" + h.bin}, h.lines(t, "codex.path"),
		"codex found on PATH: its directory is PATH's own")
	assert.Equal(t, "task-5ebcc524", h.args(t, "tmux.args")[3], "named after the random end of the ID")
}

func TestLaunchScript_NoCodexOnTheHost(t *testing.T) {
	h := newStubHost(t, true, true)
	script, err := buildLaunchScript(codexParams(t, launchNew, t.TempDir(), "hello"))
	require.NoError(t, err)

	err = parseLaunchOutput(h.run(t, script, "/bin/false"))

	var launchErr *LaunchError
	require.ErrorAs(t, err, &launchErr)
	assert.Equal(t, RefusedNoCodex, launchErr.Code)
	assert.Equal(t, "codex was not found on the host", launchErr.Error())
	assert.NoFileExists(t, filepath.Join(h.log, "claude.args"), "claude is never run in codex's place")
	assert.Empty(t, h.leftoverFiles(t))
}

func TestBuildLaunchScript_AgentRules(t *testing.T) {
	_, err := buildLaunchScript(codexParams(t, launchNew, "/workspace/user/project", "hello"))
	require.NoError(t, err, "a new codex task has no session ID: codex picks its own")

	p := codexParams(t, launchNew, "/workspace/user/project", "hello")
	p.sessionID = codexSessionA
	_, err = buildLaunchScript(p)
	assert.ErrorIs(t, err, ErrInvalidLaunch, "a new codex task cannot be given a session ID")

	p = codexParams(t, launchResume, "/workspace/user/project", "")
	p.sessionID = "my-named-session"
	_, err = buildLaunchScript(p)
	assert.ErrorIs(t, err, ErrInvalidLaunch, "codex resume takes a session name too; only a UUID is passed")

	p = newParams(t, "/workspace/user/project", "hello")
	p.sessionID = ""
	_, err = buildLaunchScript(p)
	assert.ErrorIs(t, err, ErrInvalidLaunch, "a new claude task needs the minted session ID")

	p = newParams(t, "/workspace/user/project", "hello")
	p.agent = "aider"
	_, err = buildLaunchScript(p)
	assert.ErrorIs(t, err, ErrInvalidLaunch)
}

func TestTmuxSessionForCodex(t *testing.T) {
	// A version 7 UUID begins with its creation time, so sessions started
	// within a minute share their first eight characters; the name takes the
	// last eight, which are random.
	assert.Equal(t, "task-5ebcc524", tmuxSessionForCodex(codexSessionA))
	assert.Regexp(t, validTmuxSessionName, tmuxSessionForCodex(codexSessionA))
}

func TestLaunch_CodexTaskHasNoSessionIDYet(t *testing.T) {
	var scripts []string
	svc := New(Options{
		Rand: bytes.NewReader(append([]byte{0x0a, 0x1b, 0x2c, 0x3d}, bytes.Repeat([]byte{0xab}, 16)...)),
		RunLocal: func(_ context.Context, script string) ([]byte, error) {
			scripts = append(scripts, script)
			return []byte("::panemux-launch ok\n"), nil
		},
	})
	defer svc.Close()

	req := LaunchRequest{Agent: AgentCodex, CWD: "/workspace/user/project", Prompt: "go"}
	got, err := svc.Launch(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, Launched{TmuxSession: "task-0a1b2c3d"}, got)
	require.Len(t, scripts, 1)
	assert.Contains(t, scripts[0], "agent='codex'")
	assert.Contains(t, scripts[0], "sid=''")
}

func TestLaunch_UnknownAgentRunsNothing(t *testing.T) {
	svc := New(Options{Rand: launchRand(), RunLocal: func(context.Context, string) ([]byte, error) {
		t.Fatal("ran a script for an unknown agent")
		return nil, nil
	}})
	defer svc.Close()
	_, err := svc.Launch(context.Background(), LaunchRequest{Agent: "aider", CWD: "/w", Prompt: "go"})
	assert.ErrorIs(t, err, ErrInvalidLaunch)
}

func stoppedCodexOutput(stoppedID, cwd, runningID string) []byte {
	return joinLines(
		"::panemux-tasks v1",
		"::now 1000",
		"::section ps",
		"8 1 codex",
		"::section codex-open",
		"8\t00:10\t995 10\tcompleted 990\t\t\t\t/h/.codex/sessions/2026/09/27/"+rolloutName("2026-09-27T12-00-15", runningID),
		"::section codex-rollouts",
		"995\t10\t"+rolloutName("2026-09-27T12-00-15", runningID)+"\t\t"+`"source":"cli"`,
		"990\t10\t"+rolloutName("2026-09-27T11-57-03", stoppedID)+"\t"+`"cwd":"`+cwd+`"`+"\t"+`"source":"cli"`,
		"::end",
	)
}

func TestResume_RunsCodexForAStoppedCodexTask(t *testing.T) {
	var scripts []string
	svc := New(Options{
		Now: func() time.Time { return time.Unix(1000, 0) },
		RunLocal: func(_ context.Context, script string) ([]byte, error) {
			scripts = append(scripts, script)
			if script == collectScript {
				return stoppedCodexOutput(codexSessionA, "/workspace/user/api", codexSessionB), nil
			}
			return []byte("::panemux-launch ok\n"), nil
		},
		Rand: launchRand(),
	})
	defer svc.Close()

	got, err := svc.Resume(context.Background(), "", AgentCodex, codexSessionA)

	require.NoError(t, err)
	assert.Equal(t, Launched{
		TaskID: "local:codex:" + codexSessionA, SessionID: codexSessionA, TmuxSession: tmuxSessionForCodex(codexSessionA),
	}, got)
	require.Len(t, scripts, 2)
	assert.Contains(t, scripts[1], "agent='codex'")
	assert.Contains(t, scripts[1], "mode='resume'")
	assert.Contains(t, scripts[1], "\n/workspace/user/api\nPANEMUX_CWD_")
}

func TestResume_CodexRefusals(t *testing.T) {
	collection := func(t *testing.T) func(context.Context, string) ([]byte, error) {
		return func(_ context.Context, script string) ([]byte, error) {
			if script != collectScript {
				t.Fatalf("launched although the resume should have been refused")
			}
			return stoppedCodexOutput(codexSessionA, "/w", codexSessionB), nil
		}
	}
	for _, tt := range []struct {
		wantIs           error
		name, agent, sid string
	}{
		{name: "running codex session", agent: AgentCodex, sid: codexSessionB, wantIs: ErrNoStoppedTask},
		{name: "a codex session resumed as claude", agent: AgentClaude, sid: codexSessionA, wantIs: ErrNoStoppedTask},
		{name: "unknown agent", agent: "aider", sid: codexSessionA, wantIs: ErrInvalidLaunch},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := New(Options{Now: func() time.Time { return time.Unix(1000, 0) }, RunLocal: collection(t), Rand: launchRand()})
			defer svc.Close()
			_, err := svc.Resume(context.Background(), "", tt.agent, tt.sid)
			assert.ErrorIs(t, err, tt.wantIs)
		})
	}
}
