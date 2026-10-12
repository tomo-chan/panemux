package tasks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"panemux/internal/taskhook"
)

func installedHookHost(t *testing.T, h stubHost, customHome ...string) string {
	t.Helper()
	for _, name := range []string{"id", "stat", "uname", "sed", "tr", "perl", "sleep"} {
		path, err := exec.LookPath(name)
		require.NoError(t, err)
		require.NoError(t, os.Symlink(path, filepath.Join(h.bin, name)))
	}
	home, err := filepath.EvalSymlinks(h.tmp)
	require.NoError(t, err)
	codexHome := filepath.Join(home, ".codex")
	if len(customHome) != 0 {
		codexHome = customHome[0]
	}
	require.NoError(t, os.MkdirAll(filepath.Join(codexHome, "sessions"), 0700))
	machine := ""
	if runtime.GOOS == "darwin" {
		machine = "darwin-platform-uuid:01234567-89ab-cdef-0123-456789abcdef"
		writeExecutable(t, filepath.Join(h.bin, "dscacheutil"),
			fmt.Sprintf("#!/bin/sh\nprintf 'uid: %%s\\ndir: %%s\\n' '%d' '%s'\n", os.Geteuid(), home))
		writeExecutable(t, filepath.Join(h.bin, "ioreg"),
			"#!/bin/sh\nprintf '%s\\n' '\"IOPlatformUUID\" = \"01234567-89AB-CDEF-0123-456789ABCDEF\"'\n")
		path, e := exec.LookPath("shasum")
		require.NoError(t, e)
		require.NoError(t, os.Symlink(path, filepath.Join(h.bin, "shasum")))
	} else {
		data, e := os.ReadFile("/etc/machine-id")
		require.NoError(t, e)
		machine = "linux-machine-id:" + strings.TrimSpace(string(data))
		writeExecutable(t, filepath.Join(h.bin, "getent"),
			fmt.Sprintf("#!/bin/sh\nprintf 'fixture:x:%d:0::%%s:/bin/sh\\n' '%s'\n", os.Geteuid(), home))
		path, e := exec.LookPath("sha256sum")
		require.NoError(t, e)
		require.NoError(t, os.Symlink(path, filepath.Join(h.bin, "sha256sum")))
	}
	target := taskhook.TargetIdentity{Version: 1, UID: os.Geteuid(), AccountHome: home,
		CodexHome: codexHome, Machine: machine}
	paths, err := taskhook.ContextPaths(target)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(paths.Helper), 0700))
	require.NoError(t, os.Mkdir(paths.State, 0700))
	writeExecutable(t, paths.Helper, `#!/bin/sh
case "$1" in
selfcheck) printf '%s' '{"helper_version":"0.1.0","process_probe":"available","protocol_version":1}';;
register-managed-launch)
 printf '%s\0' "$@" > "$STUB_LOG/hook.args"
 printf '%s' "$PPID" > "$STUB_LOG/hook.parent"
 printf 'g1-00000000000000000000000000000001';;
esac
`)
	return paths.Helper
}

func TestLaunchScriptOptionalRegistrationHasOuterBudget(t *testing.T) {
	for _, mode := range []launchMode{launchNew, launchResume} {
		for _, stage := range []string{"account", "selfcheck", "register"} {
			t.Run(string(mode)+"/"+stage, func(t *testing.T) {
				h := newStubHost(t, true, false)
				codexDir := h.withCodex(t)
				require.NoError(t, os.Symlink(filepath.Join(codexDir, "codex"), filepath.Join(h.bin, "codex")))
				writeExecutable(t, filepath.Join(codexDir, "codex"), stubCodex+`
printf '%s' "${PANEMUX_TASK_OWNER_GENERATION:-}" > "$STUB_LOG/codex.generation"
`)
				helper := installedHookHost(t, h)
				if stage == "account" {
					probe := "getent"
					if runtime.GOOS == "darwin" {
						probe = "dscacheutil"
					}
					writeExecutable(t, filepath.Join(h.bin, probe), "#!/bin/sh\nexec sleep 5\n")
				} else {
					body, err := os.ReadFile(helper)
					require.NoError(t, err)
					blocked := "selfcheck"
					if stage == "register" {
						blocked = "register-managed-launch"
					}
					writeExecutable(t, helper, strings.Replace(string(body), blocked+")", blocked+") exec sleep 5;", 1))
				}
				// Measure the actual compiled exec boundary. tmux setup and help
				// discovery precede optional registration and have separate contracts.
				script := codexManagedExec + ` -- fixture-prompt`
				if mode == launchResume {
					script = codexManagedExec + ` resume -- "$3"`
				}
				//nolint:gosec // compiled script executes only this test's own Codex stand-in
				cmd := exec.Command("sh", "-s", "--", "unused", filepath.Join(codexDir, "codex"), testSessionID)
				cmd.Stdin = strings.NewReader(script)
				cmd.Dir = h.tmp
				cmd.Env = []string{"PATH=" + h.bin, "STUB_LOG=" + h.log, "TMPDIR=" + h.tmp, "HOME=" + h.tmp,
					"PANEMUX_TASK_OWNER_GENERATION=g1-00000000000000000000000000000002"}
				started := time.Now()
				_, err := cmd.Output()
				require.NoError(t, err)
				assert.Less(t, time.Since(started), 3500*time.Millisecond, "optional registration delayed ordinary exec")
				generation, err := os.ReadFile(filepath.Join(h.log, "codex.generation"))
				require.NoError(t, err)
				if stage == "selfcheck" {
					assert.Equal(t, "g1-00000000000000000000000000000001", string(generation))
				} else {
					assert.Empty(t, string(generation), "timed out launch inherited stale ownership")
				}
			})
		}
	}
}

func TestLaunchScriptOptionalHookRegistrationPreservesCodexExec(t *testing.T) {
	for _, mode := range []launchMode{launchNew, launchResume} {
		t.Run(string(mode), func(t *testing.T) {
			h := newStubHost(t, true, false)
			codexDir := h.withCodex(t)
			require.NoError(t, os.Symlink(filepath.Join(codexDir, "codex"), filepath.Join(h.bin, "codex")))
			writeExecutable(t, filepath.Join(codexDir, "codex"), stubCodex+`
printf '%s' "${PANEMUX_TASK_OWNER_GENERATION:-}" > "$STUB_LOG/codex.generation"
printf '%s' "$$" > "$STUB_LOG/codex.pid"
`)
			helper := installedHookHost(t, h)
			script, err := buildLaunchScript(codexParams(t, mode, t.TempDir(), hostilePrompt))
			require.NoError(t, err)
			require.NoError(t, parseLaunchOutput(h.run(t, script, "/bin/false")))
			data, e := os.ReadFile(filepath.Join(h.log, "codex.generation"))
			require.NoError(t, e)
			assert.Equal(t, "g1-00000000000000000000000000000001", string(data))
			args := h.args(t, "hook.args")
			assert.Equal(t, []string{"register-managed-launch", "--root",
				filepath.Join(filepath.Dir(filepath.Dir(helper)), "state")}, args[:3])
			assert.Contains(t, args, strconv.Itoa(os.Geteuid()))
			assert.NotContains(t, args, "--scope")
			parent, e := os.ReadFile(filepath.Join(h.log, "hook.parent"))
			require.NoError(t, e)
			pid, e := os.ReadFile(filepath.Join(h.log, "codex.pid"))
			require.NoError(t, e)
			assert.Equal(t, string(pid), string(parent), "register the exec shell, not command substitution")
			assert.NoFileExists(t, filepath.Join(h.tmp, "panemux-owner"))
		})
	}
}

func TestLaunchScriptUnsafeOrUnavailableHookFallsBack(t *testing.T) {
	failures := []string{
		"missing-helper", "wrong-protocol", "symlink", "shared-helper", "missing-state", "no-account",
		"missing-group-bootstrap",
	}
	for _, failure := range failures {
		t.Run(failure, func(t *testing.T) {
			h := newStubHost(t, true, false)
			codexDir := h.withCodex(t)
			require.NoError(t, os.Symlink(filepath.Join(codexDir, "codex"), filepath.Join(h.bin, "codex")))
			writeExecutable(t, filepath.Join(codexDir, "codex"), stubCodex+`
printf '%s' "${PANEMUX_TASK_OWNER_GENERATION:-}" > "$STUB_LOG/codex.generation"
`)
			helper := installedHookHost(t, h)
			switch failure {
			case "missing-group-bootstrap":
				require.NoError(t, os.Remove(filepath.Join(h.bin, "perl")))
			case "missing-helper":
				require.NoError(t, os.Remove(helper))
			case "wrong-protocol":
				writeExecutable(t, helper, "#!/bin/sh\nprintf 'incompatible'\n")
			case "symlink":
				require.NoError(t, os.Rename(helper, helper+".saved"))
				require.NoError(t, os.Symlink(helper+".saved", helper))
			case "shared-helper":
				require.NoError(t, os.Chmod(helper, 0777))
			case "missing-state":
				require.NoError(t, os.Remove(filepath.Join(filepath.Dir(filepath.Dir(helper)), "state")))
			case "no-account":
				probe := "getent"
				if runtime.GOOS == "darwin" {
					probe = "dscacheutil"
				}
				writeExecutable(t, filepath.Join(h.bin, probe), "#!/bin/sh\nexit 1\n")
			}
			script, err := buildLaunchScript(codexParams(t, launchNew, t.TempDir(), hostilePrompt))
			require.NoError(t, err)
			// Simulate a stale inherited generation in the exec environment.
			script = "PANEMUX_TASK_OWNER_GENERATION=g1-00000000000000000000000000000002\n" +
				"export PANEMUX_TASK_OWNER_GENERATION\n" + script
			require.NoError(t, parseLaunchOutput(h.run(t, script, "/bin/false")))
			generation, err := os.ReadFile(filepath.Join(h.log, "codex.generation"))
			require.NoError(t, err)
			assert.Empty(t, string(generation))
			assert.NoFileExists(t, filepath.Join(h.log, "hook.args"))
			if failure == "missing-state" {
				assert.NoDirExists(t, filepath.Join(filepath.Dir(filepath.Dir(helper)), "state"))
			}
		})
	}
}

func TestLaunchScriptUsesCanonicalExecCodexHome(t *testing.T) {
	h := newStubHost(t, true, false)
	codexDir := h.withCodex(t)
	require.NoError(t, os.Symlink(filepath.Join(codexDir, "codex"), filepath.Join(h.bin, "codex")))
	home, err := filepath.EvalSymlinks(h.tmp)
	require.NoError(t, err)
	custom := filepath.Join(home, "custom")
	helper := installedHookHost(t, h, custom)
	alias := filepath.Join(home, "alias")
	require.NoError(t, os.Symlink(custom, alias))
	script, err := buildLaunchScript(codexParams(t, launchResume, t.TempDir(), hostilePrompt))
	require.NoError(t, err)
	script = "CODEX_HOME='" + alias + "'\nexport CODEX_HOME\n" + script
	require.NoError(t, parseLaunchOutput(h.run(t, script, "/bin/false")))
	args := h.args(t, "hook.args")
	assert.Equal(t, []string{"register-managed-launch", "--root",
		filepath.Join(filepath.Dir(filepath.Dir(helper)), "state")}, args[:3])
	assert.Contains(t, args, custom)
	assert.NotContains(t, args, alias)
}
