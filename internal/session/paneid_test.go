package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The pane IDs a pane's shell is told about: only ones the task dashboard's
// collection accepts back, so the value a process carries can never be
// something panemux itself would refuse to read.
func TestPaneIDEnvAcceptsOnlySafePaneIDs(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{name: "generated id", id: "pane-1790346631000-a1b2c", want: true},
		{name: "config id with dot and underscore", id: "api_server.2", want: true},
		{name: "128 characters", id: strings.Repeat("a", 128), want: true},
		{name: "129 characters", id: strings.Repeat("a", 129), want: false},
		{name: "empty", id: "", want: false},
		{name: "space", id: "my pane", want: false},
		{name: "single quote", id: "it's", want: false},
		{name: "shell metacharacters", id: "a;id", want: false},
		{name: "newline", id: "a\nb", want: false},
		{name: "non-ascii", id: "ペイン", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := paneIDEnv([]string{"TERM=xterm-256color"}, tt.id)
			got, ok := envValue(env, paneIDEnvName)
			if ok != tt.want {
				t.Fatalf("%s set = %v, want %v (env %q)", paneIDEnvName, ok, tt.want, env)
			}
			if tt.want && got != tt.id {
				t.Fatalf("%s = %q, want %q", paneIDEnvName, got, tt.id)
			}
			if term, _ := envValue(env, "TERM"); term != "xterm-256color" {
				t.Fatalf("TERM = %q, want the base environment kept", term)
			}
		})
	}
}

// panemux started from inside one of its own panes inherits that pane's ID.
// It must never reach a pane of this panemux, whether or not the pane gets
// an ID of its own.
func TestPaneIDEnvDropsAnInheritedPaneID(t *testing.T) {
	base := []string{"PANEMUX_PANE_ID=outer-pane", "HOME=/remote/home/demo", "PANEMUX_PANE_ID=outer-again"}

	env := paneIDEnv(base, "inner")
	if got := countEnv(env, paneIDEnvName); got != 1 {
		t.Fatalf("%s appears %d times, want once: %q", paneIDEnvName, got, env)
	}
	if got, _ := envValue(env, paneIDEnvName); got != "inner" {
		t.Fatalf("%s = %q, want the pane's own ID", paneIDEnvName, got)
	}

	env = paneIDEnv(base, "not valid")
	if _, ok := envValue(env, paneIDEnvName); ok {
		t.Fatalf("%s is set for a pane with no valid ID: %q", paneIDEnvName, env)
	}
	if home, _ := envValue(env, "HOME"); home != "/remote/home/demo" {
		t.Fatalf("HOME = %q, want other entries kept", home)
	}
}

func countEnv(env []string, key string) int {
	n := 0
	for _, entry := range env {
		if name, _, ok := strings.Cut(entry, "="); ok && name == key {
			n++
		}
	}
	return n
}

func TestNewLocalExportsThePaneID(t *testing.T) {
	for _, shim := range []bool{true, false} {
		t.Run("browser shim "+map[bool]string{true: "on", false: "off"}[shim], func(t *testing.T) {
			withBrowserShimEnabled(t, shim)
			withShimCacheDir(t)

			sess, err := NewLocal("pane-1790346631000-a1b2c", "/bin/sh", "", "pane")
			if err != nil {
				t.Fatalf("NewLocal: %v", err)
			}
			defer sess.Close()

			if got, _ := envValue(sess.cmd.Env, paneIDEnvName); got != "pane-1790346631000-a1b2c" {
				t.Fatalf("%s = %q, want the pane ID", paneIDEnvName, got)
			}
		})
	}
}

func TestNewLocalWithAnUnsafePaneIDStartsWithoutIt(t *testing.T) {
	withBrowserShimEnabled(t, false)

	sess, err := NewLocal("my pane", "/bin/sh", "", "pane")
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	defer sess.Close()

	if sess.State() != StateConnected {
		t.Fatalf("state = %v, want the pane to start anyway", sess.State())
	}
	if got, ok := envValue(sess.cmd.Env, paneIDEnvName); ok {
		t.Fatalf("%s = %q, want it unset", paneIDEnvName, got)
	}
}

// The remote command is run by a real shell, with and without the browser
// shim and for every shape of tail, and asked what it exported.
func TestSSHShellCommandExportsThePaneIDWhenRunByAShell(t *testing.T) {
	for _, shim := range []bool{true, false} {
		for name, cfg := range map[string]SSHConfig{
			"plain pane": {},
			"cwd only":   {Cwd: "/"},
		} {
			t.Run(name+map[bool]string{true: " with shim", false: " without shim"}[shim], func(t *testing.T) {
				withBrowserShimEnabled(t, shim)

				cmd, err := sshShellCommand("pane-1790346631000-a1b2c", cfg)
				if err != nil {
					t.Fatalf("sshShellCommand: %v", err)
				}
				got := runRemoteSetup(t, cmd)
				if got != "pane-1790346631000-a1b2c" {
					t.Fatalf("exported %s = %q, want the pane ID", paneIDEnvName, got)
				}
			})
		}
	}
}

// runRemoteSetup runs the whole remote command with $SHELL pointing at a
// script that prints the pane ID it was started with, standing in for the
// login shell the command execs.
func runRemoteSetup(t *testing.T, cmd string) string {
	t.Helper()
	dir := t.TempDir()
	shell := filepath.Join(dir, "print-pane-id")
	//nolint:gosec // G306: the fixture stands in for an executable login shell
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf %s \"${PANEMUX_PANE_ID:-unset}\"\n"), 0o755); err != nil {
		t.Fatalf("write shell: %v", err)
	}
	run := exec.Command("sh", "-c", cmd) //nolint:gosec // G204: the generated command under test
	run.Env = []string{"HOME=" + dir, "PATH=/usr/bin:/bin", "SHELL=" + shell}
	out, err := run.Output()
	if err != nil {
		t.Fatalf("run remote setup: %v", err)
	}
	return string(out)
}

func TestSSHShellCommandWithAnUnsafePaneIDLeavesTheCommandAlone(t *testing.T) {
	withBrowserShimEnabled(t, false)

	for _, id := range []string{"", "my pane", "x'; id; '"} {
		got, err := sshShellCommand(id, SSHConfig{})
		if err != nil {
			t.Fatalf("sshShellCommand(%q): %v", id, err)
		}
		if got != "" {
			t.Fatalf("sshShellCommand(%q) = %q, want the SSH shell request", id, got)
		}
		if strings.Contains(got, paneIDEnvName) {
			t.Fatalf("command mentions %s for an unsafe ID: %q", paneIDEnvName, got)
		}
	}
}

// A plain SSH pane with a pane ID has to run a command, so it starts the
// login shell explicitly, as a pane with the browser shim does.
func TestSSHShellCommandWithAPaneIDStartsALoginShell(t *testing.T) {
	withBrowserShimEnabled(t, false)

	tests := []struct {
		name     string
		wantTail string
		cfg      SSHConfig
	}{
		{name: "plain pane", cfg: SSHConfig{}, wantTail: remoteLoginShellExec},
		{name: "cwd only", cfg: SSHConfig{Cwd: "/remote/home/demo"}, wantTail: "cd '/remote/home/demo' && exec $SHELL"},
		{name: "shell only", cfg: SSHConfig{Shell: "/bin/zsh"}, wantTail: "exec '/bin/zsh'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sshShellCommand("p1", tt.cfg)
			if err != nil {
				t.Fatalf("sshShellCommand: %v", err)
			}
			if want := shCommand(remotePaneIDSetup("p1") + tt.wantTail); got != want {
				t.Fatalf("sshShellCommand = %q, want %q", got, want)
			}
		})
	}
}

func TestRemotePaneIDSetup(t *testing.T) {
	if got := remotePaneIDSetup("p1"); got != "PANEMUX_PANE_ID='p1'; export PANEMUX_PANE_ID; " {
		t.Fatalf("remotePaneIDSetup = %q", got)
	}
	for _, id := range []string{"", "my pane", "it's", strings.Repeat("a", 129)} {
		if got := remotePaneIDSetup(id); got != "" {
			t.Fatalf("remotePaneIDSetup(%q) = %q, want nothing", id, got)
		}
	}
}

// shCommand is the form every remote command with setup takes: the POSIX
// script handed whole to /bin/sh (see sshShellCommand).
func shCommand(script string) string {
	return "exec /bin/sh -c " + shellQuotePath(script)
}
