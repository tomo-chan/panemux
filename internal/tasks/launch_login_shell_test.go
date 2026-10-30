package tasks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rc files a login shell test's home holds. Each exports
// PANEMUX_TEST_RC naming itself and changes to /, as an rc that sets up a
// shell may: the agent must still start in the task's directory. The
// .bashrc returns at once when the shell is not interactive, as the default
// .bashrc of most distributions does, so its export arrives only from an
// interactive shell (issue #445).
var loginShellRCFiles = map[string]string{
	".profile": "PANEMUX_TEST_RC=profile; export PANEMUX_TEST_RC; cd /\n" +
		"if [ -n \"${BASH_VERSION-}\" ] && [ -f \"$HOME/.bashrc\" ]; then . \"$HOME/.bashrc\"; fi\n",
	".bashrc": "case $- in *i*) ;; *) return ;; esac\nexport PANEMUX_TEST_RC=bashrc; cd /\n",
	".zshrc":  "export PANEMUX_TEST_RC=zshrc; cd /\n",
}

// writeLoginShellRCFiles puts loginShellRCFiles in the home directory the
// stub host's script runs with.
func (h stubHost) writeLoginShellRCFiles(t *testing.T) {
	t.Helper()
	for name, content := range loginShellRCFiles {
		require.NoError(t, os.WriteFile(filepath.Join(h.tmp, name), []byte(content), 0o600))
	}
}

// promptFilesLeft lists the prompt files left in the host's temp directory,
// which also holds the rc files.
func (h stubHost) promptFilesLeft(t *testing.T) []string {
	t.Helper()
	var left []string
	for _, name := range h.leftoverFiles(t) {
		if strings.HasPrefix(name, "panemux-task.") {
			left = append(left, name)
		}
	}
	return left
}

// lookUpShell finds a shell on the machine running the suite, skipping the
// test where it is not installed.
func lookUpShell(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed", name)
	}
	return path
}

// A task's agent starts through the user's own shell, run as a login and
// interactive shell as tmux runs a pane's shell, so what the user's rc files
// export — an ANTHROPIC_API_KEY in ~/.bashrc — reaches it as it reaches a
// claude typed into a pane (issue #445). The rc runs before the agent's own
// command, so the agent still starts in the task's directory after an rc that
// changes directory, and the prompt still reaches it as one argument.
func TestLaunchScript_RunsTheAgentThroughTheUsersLoginShell(t *testing.T) {
	shells := []struct {
		name   string
		wantRC string
	}{
		{name: "bash", wantRC: "bashrc"},
		{name: "zsh", wantRC: "zshrc"},
		{name: "sh", wantRC: "profile"},
	}
	for _, sh := range shells {
		for _, mode := range []launchMode{launchNew, launchResume} {
			t.Run(sh.name+"/"+string(mode), func(t *testing.T) {
				shell := lookUpShell(t, sh.name)
				h := newStubHost(t, true, true)
				h.writeLoginShellRCFiles(t)
				cwd := t.TempDir()
				p := newParams(t, cwd, hostilePrompt)
				wantArgs := []string{"--session-id=" + testSessionID, "--", hostilePrompt}
				if mode == launchResume {
					p = newParams(t, cwd, "")
					p.mode = launchResume
					wantArgs = []string{"--resume=" + testSessionID}
				}
				script, err := buildLaunchScript(p)
				require.NoError(t, err)

				require.NoError(t, parseLaunchOutput(h.run(t, script, shell)))

				assert.Equal(t, []string{sh.wantRC}, h.lines(t, "claude.rc"))
				assert.Equal(t, wantArgs, h.args(t, "claude.args"))
				assert.Equal(t, []string{cwd}, h.lines(t, "claude.pwd"))
				tmuxArgs := h.args(t, "tmux.args")
				require.GreaterOrEqual(t, len(tmuxArgs), 11)
				assert.Equal(t, []string{
					"--", shell, "-lic", `exec "$@"`, shell, filepath.Join(h.bin, "sh"), "-c",
				}, tmuxArgs[4:11], "the shell is given only the fixed command; the agent's follows as arguments")
				assert.NotContains(t, strings.Join(tmuxArgs, "\n"), "touch pwned", "the prompt must not reach tmux's arguments")
				assert.Empty(t, h.promptFilesLeft(t), "the prompt file is removed once claude has read it")
				assert.NoFileExists(t, filepath.Join(cwd, "pwned"))
				assert.NoFileExists(t, filepath.Join(h.tmp, "pwned"))
				assert.NoFileExists(t, "/pwned")
			})
		}
	}
}

// A resume that adds a window to a session left by a pane goes through the
// login shell too.
func TestLaunchScript_AReusedSessionsWindowRunsTheAgentThroughTheLoginShell(t *testing.T) {
	shell := lookUpShell(t, "bash")
	h := newStubHost(t, true, true)
	h.writeLoginShellRCFiles(t)
	require.NoError(t, os.WriteFile(filepath.Join(h.log, "exists"), nil, 0o600))
	cwd := t.TempDir()
	p := newParams(t, cwd, "")
	p.mode, p.reuseSession = launchResume, true
	script, err := buildLaunchScript(p)
	require.NoError(t, err)

	require.NoError(t, parseLaunchOutput(h.run(t, script, shell)))

	tmuxArgs := h.args(t, "tmux.args")
	assert.Equal(t, []string{"new-window", "-t", "=task-0f0e0d0c:", "--", shell, "-lic"}, tmuxArgs[:6])
	assert.Equal(t, []string{"bashrc"}, h.lines(t, "claude.rc"))
	assert.Equal(t, []string{cwd}, h.lines(t, "claude.pwd"))
}

// Only bash, zsh and sh are run through. Any other shell — fish and csh,
// whose -c does not take the fixed command's "$@", dash, ksh and mksh, or
// one panemux does not know — or a $SHELL that is not an absolute path to
// an executable is not used: the agent starts directly, as it did before, and
// the rc files are not read.
//
//efficacy:exempt pins the start every launch had before issue #445, which a revert of the change keeps
func TestLaunchScript_StartsTheAgentDirectlyWhenTheShellIsNotOneItRunsThrough(t *testing.T) {
	tests := []struct {
		shell func(t *testing.T, h stubHost) string
		name  string
	}{
		{name: "fish", shell: func(t *testing.T, h stubHost) string {
			return writeRecordingShell(t, h, "fish")
		}},
		{name: "tcsh", shell: func(t *testing.T, h stubHost) string {
			return writeRecordingShell(t, h, "tcsh")
		}},
		{name: "dash", shell: func(t *testing.T, h stubHost) string {
			return writeRecordingShell(t, h, "dash")
		}},
		{name: "ksh", shell: func(t *testing.T, h stubHost) string {
			return writeRecordingShell(t, h, "ksh")
		}},
		{name: "mksh", shell: func(t *testing.T, h stubHost) string {
			return writeRecordingShell(t, h, "mksh")
		}},
		{name: "a shell panemux has not checked", shell: func(t *testing.T, h stubHost) string {
			return writeRecordingShell(t, h, "login-shell")
		}},
		{name: "unset", shell: func(*testing.T, stubHost) string { return "" }},
		{name: "relative", shell: func(*testing.T, stubHost) string { return "bash" }},
		{name: "missing", shell: func(t *testing.T, _ stubHost) string {
			return filepath.Join(t.TempDir(), "bash")
		}},
		{name: "not executable", shell: func(t *testing.T, _ stubHost) string {
			path := filepath.Join(t.TempDir(), "bash")
			require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o600))
			return path
		}},
		{name: "a directory", shell: func(t *testing.T, _ stubHost) string {
			path := filepath.Join(t.TempDir(), "bash")
			require.NoError(t, os.Mkdir(path, 0o700))
			return path
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newStubHost(t, true, true)
			h.writeLoginShellRCFiles(t)
			cwd := t.TempDir()
			script, err := buildLaunchScript(newParams(t, cwd, "hello"))
			require.NoError(t, err)

			require.NoError(t, parseLaunchOutput(h.run(t, script, tt.shell(t, h))))

			assert.Equal(t, []string{"--session-id=" + testSessionID, "--", "hello"}, h.args(t, "claude.args"))
			assert.Equal(t, []string{cwd}, h.lines(t, "claude.pwd"))
			assert.Equal(t, []string{""}, h.lines(t, "claude.rc"))
			assert.Equal(t, []string{"new-session", "-d", "-s", "task-0f0e0d0c", "--", "sh", "-c"}, h.args(t, "tmux.args")[:7])
			assert.NoFileExists(t, filepath.Join(h.log, "shell.args"), "the shell is not run to start the agent")
		})
	}
}

// writeRecordingShell writes a stand-in shell named name that answers the
// binary lookup with nothing and records any other run.
func writeRecordingShell(t *testing.T, h stubHost, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	writeExecutable(t, path, "#!/bin/sh\n"+
		"[ \"$1\" = -lc ] && exit 0\n"+
		"printf '%s\\0' \"$@\" > '"+filepath.Join(h.log, "shell.args")+"'\n"+
		"exit 1\n")
	return path
}

// codex is given its own directory last on PATH after the rc has run, so an
// rc that sets PATH neither loses that directory nor is overridden by it.
func TestLaunchScript_CodexGetsItsDirectoryOnThePathTheRCSet(t *testing.T) {
	for _, mode := range []launchMode{launchNew, launchResume} {
		t.Run(string(mode), func(t *testing.T) {
			shell := lookUpShell(t, "bash")
			h := newStubHost(t, true, false)
			codexDir := h.withCodex(t)
			require.NoError(t, os.Symlink(filepath.Join(codexDir, "codex"), filepath.Join(h.bin, "codex")))
			rcBin := filepath.Join(h.tmp, "rc-bin")
			require.NoError(t, os.WriteFile(filepath.Join(h.tmp, ".bash_profile"),
				[]byte("PATH="+rcBin+":$PATH; export PATH; cd /\n"), 0o600))
			cwd := t.TempDir()
			prompt := "hello"
			if mode == launchResume {
				prompt = ""
			}
			script, err := buildLaunchScript(codexParams(t, mode, cwd, prompt))
			require.NoError(t, err)

			require.NoError(t, parseLaunchOutput(h.run(t, script, shell)))

			// What lies in between is the host's own: its /etc/profile may
			// add directories (a CI runner's appends /snap/bin) or set PATH
			// outright before the rc runs.
			lines := h.lines(t, "codex.path")
			require.Len(t, lines, 1)
			path := strings.Split(lines[0], ":")
			require.GreaterOrEqual(t, len(path), 2, lines[0])
			assert.Equal(t, rcBin, path[0], "the rc's PATH is kept: %s", lines[0])
			assert.Equal(t, h.bin, path[len(path)-1], "codex's directory comes last: %s", lines[0])
			assert.Equal(t, []string{cwd}, h.lines(t, "codex.pwd"))
		})
	}
}

// Under a real tmux the shell runs in the pane's terminal, as a pane's own
// shell does, and what its rc exports reaches the agent.
func TestLaunchScript_RealTmuxRunsTheAgentInTheLoginShellsEnvironment(t *testing.T) {
	for _, mode := range []launchMode{launchNew, launchResume} {
		t.Run(string(mode), func(t *testing.T) {
			shell := lookUpShell(t, "bash")
			h := newRealTmuxHost(t)
			h.writeLoginShellRCFiles(t)
			cwd := t.TempDir()
			p := newParams(t, cwd, "hello")
			if mode == launchResume {
				p = newParams(t, cwd, "")
				p.mode = launchResume
			}
			script, err := buildLaunchScript(p)
			require.NoError(t, err)

			require.NoError(t, parseLaunchOutput(h.run(t, script, shell)))

			h.waitForClaude(t)
			assert.Equal(t, []string{"bashrc"}, h.lines(t, "claude.rc"))
			assert.Equal(t, []string{cwd}, h.lines(t, "claude.pwd"))
		})
	}
}
