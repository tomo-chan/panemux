package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakePTY struct { //nolint:govet // fieldalignment: clarity preferred
	writes [][]byte
	err    error
}

func (p *fakePTY) Write(b []byte) (int, error) {
	if p.err != nil {
		return 0, p.err
	}
	p.writes = append(p.writes, append([]byte(nil), b...))
	return len(b), nil
}

func TestExpandLocalAgmsgPath_NoTilde_Unchanged(t *testing.T) {
	got, err := ExpandLocalAgmsgPath("/opt/agmsg")
	if err != nil {
		t.Fatalf("ExpandLocalAgmsgPath: %v", err)
	}
	if got != "/opt/agmsg" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandLocalAgmsgPath_TildeSlash_Expanded(t *testing.T) {
	orig := userHomeDirFn
	defer func() { userHomeDirFn = orig }()
	userHomeDirFn = func() (string, error) { return "/home/testuser", nil }

	got, err := ExpandLocalAgmsgPath("~/.agents/skills/agmsg")
	if err != nil {
		t.Fatalf("ExpandLocalAgmsgPath: %v", err)
	}
	want := filepath.Join("/home/testuser", ".agents", "skills", "agmsg")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpandLocalAgmsgPath_BareTilde_Expanded(t *testing.T) {
	orig := userHomeDirFn
	defer func() { userHomeDirFn = orig }()
	userHomeDirFn = func() (string, error) { return "/home/testuser", nil }

	got, err := ExpandLocalAgmsgPath("~")
	if err != nil {
		t.Fatalf("ExpandLocalAgmsgPath: %v", err)
	}
	if got != "/home/testuser" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandLocalAgmsgPath_HomeDirError(t *testing.T) {
	orig := userHomeDirFn
	defer func() { userHomeDirFn = orig }()
	userHomeDirFn = func() (string, error) { return "", errors.New("no home") }

	if _, err := ExpandLocalAgmsgPath("~/agmsg"); err == nil {
		t.Fatal("expected an error when the home dir cannot be resolved")
	}
}

func TestExpandRemoteAgmsgPath(t *testing.T) {
	tests := []struct {
		path, home, want string
	}{
		{"~/.agents/skills/agmsg", "/home/build-user", "/home/build-user/.agents/skills/agmsg"},
		{"~", "/home/build-user", "/home/build-user"},
		{"/opt/agmsg", "/home/build-user", "/opt/agmsg"},
	}
	for _, tt := range tests {
		if got := ExpandRemoteAgmsgPath(tt.path, tt.home); got != tt.want {
			t.Errorf("ExpandRemoteAgmsgPath(%q, %q) = %q, want %q", tt.path, tt.home, got, tt.want)
		}
	}
}

func TestHasAgmsgLocal_Found(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "api.sh"), []byte("#!/bin/sh\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !HasAgmsgLocal(dir) {
		t.Fatal("expected HasAgmsgLocal to find scripts/api.sh")
	}
}

func TestHasAgmsgLocal_NotFound(t *testing.T) {
	if HasAgmsgLocal(t.TempDir()) {
		t.Fatal("expected HasAgmsgLocal to report false for an empty directory")
	}
}

func TestHasAgmsgRemote_FoundAndNotFound(t *testing.T) {
	exec := &fakeBoardExecutor{}
	if !HasAgmsgRemote(context.Background(), exec, "/opt/agmsg") {
		t.Fatal("expected found (fakeBoardExecutor defaults to success)")
	}
	args := exec.calls[0]
	if args[0] != remoteAgmsgProbeBin || args[1] != "-f" || args[2] != "/opt/agmsg/scripts/api.sh" {
		t.Fatalf("unexpected probe args: %v", args)
	}

	execFail := &fakeBoardExecutor{err: errors.New("exit 1")}
	if HasAgmsgRemote(context.Background(), execFail, "/opt/agmsg") {
		t.Fatal("expected not found when the remote test -f fails")
	}
}

func TestBootstrapInstruction_UsesPaneIDAsAgentID(t *testing.T) {
	instr := BootstrapInstruction("pane-a", "panemux", "monitor")
	if !strings.Contains(instr, `"pane-a"`) {
		t.Fatalf("expected instruction to name the pane id, got: %s", instr)
	}
	if !strings.Contains(instr, "panemux") {
		t.Fatalf("expected instruction to name the team, got: %s", instr)
	}
	if !strings.Contains(instr, "--force") {
		t.Fatalf("expected instruction to require --force, got: %s", instr)
	}
	if !strings.Contains(instr, "board_status") {
		t.Fatalf("expected instruction to describe the board_status shape, got: %s", instr)
	}
}

func TestBootstrapInstruction_ModeMonitor_NoAgmsgModeLine(t *testing.T) {
	instr := BootstrapInstruction("pane-a", "panemux", "monitor")
	if strings.Contains(instr, "/agmsg mode") {
		t.Fatalf("monitor is agmsg's default; expected no explicit /agmsg mode line, got: %s", instr)
	}
}

func TestBootstrapInstruction_ModeTurnAndBoth_IncludeAgmsgModeLine(t *testing.T) {
	for _, mode := range []string{"turn", "both"} {
		instr := BootstrapInstruction("pane-a", "panemux", mode)
		want := "/agmsg mode " + mode
		if !strings.Contains(instr, want) {
			t.Fatalf("mode %q: expected instruction to contain %q, got: %s", mode, want, instr)
		}
	}
}

func TestBootstrap_LocalAgmsgFound_WritesInstruction(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "api.sh"), []byte(""), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	pty := &fakePTY{}
	Bootstrap(context.Background(), "pane-a", LocalHostID, "panemux", "monitor", dir, pty, nil, func(string, ...any) {})

	if len(pty.writes) != 1 {
		t.Fatalf("expected exactly one PTY write, got %d", len(pty.writes))
	}
	if !strings.Contains(string(pty.writes[0]), "pane-a") {
		t.Fatalf("unexpected instruction: %s", pty.writes[0])
	}
}

func TestBootstrap_LocalAgmsgNotFound_SkipsAndLeavesSessionUntouched(t *testing.T) {
	pty := &fakePTY{}
	var warned bool
	logf := func(format string, args ...any) { warned = true }

	Bootstrap(context.Background(), "pane-a", LocalHostID, "panemux", "monitor", t.TempDir(), pty, nil, logf)

	if len(pty.writes) != 0 {
		t.Fatal("expected no PTY write when agmsg is not found")
	}
	if !warned {
		t.Fatal("expected a warning to be logged")
	}
}

func TestBootstrap_RemoteAgmsgFound_WritesInstruction(t *testing.T) {
	pty := &fakePTY{}
	exec := &fakeBoardExecutor{}

	Bootstrap(
		context.Background(), "pane-b", "build-host", "panemux", "monitor", "/opt/agmsg",
		pty, exec, func(string, ...any) {},
	)

	if len(pty.writes) != 1 {
		t.Fatalf("expected exactly one PTY write, got %d", len(pty.writes))
	}
}

func TestBootstrap_RemoteAgmsgNotFound_SkipsAndLeavesSessionUntouched(t *testing.T) {
	pty := &fakePTY{}
	exec := &fakeBoardExecutor{err: errors.New("no such file")}
	var warned bool

	Bootstrap(
		context.Background(), "pane-b", "build-host", "panemux", "monitor", "/opt/agmsg",
		pty, exec, func(string, ...any) { warned = true },
	)

	if len(pty.writes) != 0 {
		t.Fatal("expected no PTY write when remote agmsg is not found")
	}
	if !warned {
		t.Fatal("expected a warning to be logged")
	}
}

func TestBootstrap_RemoteNoExecutor_SkipsAndLogsWarning(t *testing.T) {
	pty := &fakePTY{}
	var warned bool

	Bootstrap(
		context.Background(), "pane-b", "build-host", "panemux", "monitor", "/opt/agmsg",
		pty, nil, func(string, ...any) { warned = true },
	)

	if len(pty.writes) != 0 {
		t.Fatal("expected no PTY write with no BoardExecutor available")
	}
	if !warned {
		t.Fatal("expected a warning to be logged")
	}
}

func TestBootstrap_PTYWriteError_LogsWarning(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "api.sh"), []byte(""), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	pty := &fakePTY{err: errors.New("write failed")}
	var warned bool

	Bootstrap(
		context.Background(), "pane-a", LocalHostID, "panemux", "monitor", dir,
		pty, nil, func(string, ...any) { warned = true },
	)

	if !warned {
		t.Fatal("expected a warning to be logged on PTY write failure")
	}
}
