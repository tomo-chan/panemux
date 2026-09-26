package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sshd runs an exec request's command with the user's login shell (`$SHELL
// -c`), which need not be POSIX: fish and tcsh reject assignments, `case` and
// multi-line quotes. Every command that sets anything up is therefore one line
// handed to /bin/sh, and never contains `!`, which csh-family shells expand
// even inside single quotes. This holds on any machine, whichever shells it
// has installed.
func TestSSHShellCommandIsOneLineForSh(t *testing.T) {
	for _, shim := range []bool{true, false} {
		for name, cfg := range map[string]SSHConfig{
			"plain pane":      {},
			"cwd only":        {Cwd: "/remote/home/demo"},
			"shell only":      {Shell: "/bin/zsh"},
			"shell and cwd":   {Shell: "/bin/zsh", Cwd: "/remote/home/demo"},
			"cwd with spaces": {Cwd: "/remote/home/demo/my project"},
		} {
			t.Run(name+map[bool]string{true: " with shim", false: " without shim"}[shim], func(t *testing.T) {
				withBrowserShimEnabled(t, shim)

				cmd, err := sshShellCommand("pane-1790346631000-a1b2c", cfg)
				if err != nil {
					t.Fatalf("sshShellCommand: %v", err)
				}
				if !strings.HasPrefix(cmd, "exec /bin/sh -c '") {
					t.Fatalf("command is not handed to /bin/sh: %q", cmd)
				}
				if strings.ContainsAny(cmd, "\n!") {
					t.Fatalf("command has a newline or a '!': %q", cmd)
				}
			})
		}
	}
}

// fakeLoginShell writes a stand-in for the login shell the remote command
// execs. It prints what the pane's shell would see.
func fakeLoginShell(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "print-pane-env")
	script := "#!/bin/sh\nprintf 'id=%s browser=%s\\n' \"${PANEMUX_PANE_ID:-unset}\" \"${BROWSER:-unset}\"\n"
	//nolint:gosec // G306: the fixture stands in for an executable login shell
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake login shell: %v", err)
	}
	return path
}

// The remote command, run by every login shell this machine has as sshd
// would run it (`<shell> -c <command>`). A shell that is not installed is
// skipped; TestSSHShellCommandIsOneLineForSh keeps the shape they rely on
// checked everywhere.
func TestSSHShellCommandRunsUnderEveryLoginShell(t *testing.T) {
	for _, loginShell := range []string{"sh", "bash", "dash", "zsh", "fish", "tcsh", "csh"} {
		for _, shim := range []bool{true, false} {
			for name, cfg := range map[string]SSHConfig{"plain pane": {}, "cwd only": {Cwd: "/"}} {
				label := loginShell + " " + name + map[bool]string{true: " with shim", false: " without shim"}[shim]
				t.Run(label, func(t *testing.T) {
					shellPath, err := exec.LookPath(loginShell)
					if err != nil {
						t.Skipf("%s is not installed", loginShell)
					}
					withBrowserShimEnabled(t, shim)
					cmd, err := sshShellCommand("pane-1790346631000-a1b2c", cfg)
					if err != nil {
						t.Fatalf("sshShellCommand: %v", err)
					}

					home := t.TempDir()
					run := exec.Command(shellPath, "-c", cmd) //nolint:gosec // G204: the generated command under test
					run.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "SHELL=" + fakeLoginShell(t, t.TempDir())}
					out, err := run.CombinedOutput()
					if err != nil {
						t.Fatalf("%s -c <command>: %v\n%s\ncommand: %s", loginShell, err, out, cmd)
					}

					shimPath := filepath.Join(home, ".cache", "panemux", "bin", browserShimPrimaryName)
					want := "id=pane-1790346631000-a1b2c browser=unset\n"
					if shim {
						want = "id=pane-1790346631000-a1b2c browser=" + shimPath + "\n"
					}
					if string(out) != want {
						t.Fatalf("login shell saw %q, want %q\ncommand: %s", out, want, cmd)
					}
					if shim {
						if got := readFileString(t, shimPath); got != browserShimScript {
							t.Fatalf("installed shim differs from the script:\n%s", got)
						}
					}
				})
			}
		}
	}
}
