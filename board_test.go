package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"panemux/internal/session"
)

// fakeBoardSession is a minimal session.Session that also implements
// board.BoardExecutor, standing in for SSHSession/TmuxSSHSession in tests
// that don't need a real SSH connection.
type fakeBoardSession struct {
	id     string
	tag    string // distinguishes which fakeBoardSession instance answered a call
	closed bool
}

func (f *fakeBoardSession) ID() string                     { return f.id }
func (f *fakeBoardSession) Type() session.Type             { return session.TypeSSH }
func (f *fakeBoardSession) Title() string                  { return f.id }
func (f *fakeBoardSession) State() session.State           { return session.StateConnected }
func (f *fakeBoardSession) Read(p []byte) (int, error)     { return 0, nil }
func (f *fakeBoardSession) Write(p []byte) (int, error)    { return len(p), nil }
func (f *fakeBoardSession) Resize(cols, rows uint16) error { return nil }
func (f *fakeBoardSession) Close() error                   { f.closed = true; return nil }

func (f *fakeBoardSession) RunBoardCommand(_ context.Context, _ []string) ([]byte, error) {
	if f.closed {
		return nil, errors.New("fakeBoardSession: use of closed session")
	}
	return []byte(f.tag), nil
}

func TestDynamicBoardExecutor_ReResolvesAfterPaneRestart(t *testing.T) {
	manager := session.NewManager()
	paneHosts := map[string]string{"pane-a": "build-host"}

	original := &fakeBoardSession{id: "pane-a", tag: "original"}
	manager.Add(original)

	executor := &dynamicBoardExecutor{manager: manager, paneHosts: paneHosts, host: "build-host"}

	out, err := executor.RunBoardCommand(context.Background(), []string{"noop"})
	if err != nil {
		t.Fatalf("RunBoardCommand before restart: %v", err)
	}
	if string(out) != "original" {
		t.Fatalf("expected output from original session, got %q", out)
	}

	// Simulate an ordinary, Agent-Board-unrelated pane restart: the old
	// session is closed and removed, a brand-new one takes its place under
	// the same pane ID.
	if removeErr := manager.Remove("pane-a"); removeErr != nil {
		t.Fatalf("Remove: %v", removeErr)
	}
	replacement := &fakeBoardSession{id: "pane-a", tag: "replacement"}
	manager.Add(replacement)

	out, err = executor.RunBoardCommand(context.Background(), []string{"noop"})
	if err != nil {
		t.Fatalf("RunBoardCommand after restart: %v", err)
	}
	if string(out) != "replacement" {
		t.Fatalf("expected dynamicBoardExecutor to re-resolve to the replacement session, got %q", out)
	}
	if !original.closed {
		t.Fatalf("expected original session to have been closed by Remove")
	}
}

func TestDynamicBoardExecutor_NoLiveSessionOnHost_ReturnsError(t *testing.T) {
	manager := session.NewManager()
	paneHosts := map[string]string{"pane-a": "build-host"}

	executor := &dynamicBoardExecutor{manager: manager, paneHosts: paneHosts, host: "build-host"}

	_, err := executor.RunBoardCommand(context.Background(), []string{"noop"})
	if err == nil {
		t.Fatal("expected an error when no session is registered for the host")
	}
}

func TestDynamicBoardExecutor_FallsBackToAnotherPaneOnSameHost(t *testing.T) {
	manager := session.NewManager()
	paneHosts := map[string]string{"pane-a": "build-host", "pane-b": "build-host"}

	// pane-a is the one that would have been snapshotted at startup, but it
	// gets removed without replacement; pane-b, on the same host, is still
	// live and must be found instead.
	manager.Add(&fakeBoardSession{id: "pane-b", tag: "pane-b-session"})

	executor := &dynamicBoardExecutor{manager: manager, paneHosts: paneHosts, host: "build-host"}
	out, err := executor.RunBoardCommand(context.Background(), []string{"noop"})
	if err != nil {
		t.Fatalf("RunBoardCommand: %v", err)
	}
	if string(out) != "pane-b-session" {
		t.Fatalf("expected fallback to pane-b's session, got %q", out)
	}
}

// failingBoardSession is a session.Session that is still registered in the
// Manager and reports a normal State(), but whose underlying command always
// fails — standing in for a dead SSH connection whose read loop hasn't yet
// noticed the drop and removed the session from the Manager.
type failingBoardSession struct {
	id string
}

func (f *failingBoardSession) ID() string                     { return f.id }
func (f *failingBoardSession) Type() session.Type             { return session.TypeSSH }
func (f *failingBoardSession) Title() string                  { return f.id }
func (f *failingBoardSession) State() session.State           { return session.StateConnected }
func (f *failingBoardSession) Read(p []byte) (int, error)     { return 0, nil }
func (f *failingBoardSession) Write(p []byte) (int, error)    { return len(p), nil }
func (f *failingBoardSession) Resize(cols, rows uint16) error { return nil }
func (f *failingBoardSession) Close() error                   { return nil }

func (f *failingBoardSession) RunBoardCommand(_ context.Context, _ []string) ([]byte, error) {
	return nil, errors.New("failingBoardSession: connection dead")
}

func TestDynamicBoardExecutor_DeadButRegisteredSession_TriesAnotherCandidate(t *testing.T) {
	// Regression test (adversarial review round 2, finding R1): a session
	// can still be registered in the Manager, and still report a normal
	// State(), after its underlying connection has actually died — State()
	// reporting can lag reality. The previous fix (re-resolving on every
	// call) only helped once the dead pane was removed/replaced; it did
	// nothing for "registered but dead". dynamicBoardExecutor must not give
	// up after the first candidate it happens to try — it must keep trying
	// every other board-enabled pane on the same host until one actually
	// works.
	manager := session.NewManager()
	paneHosts := map[string]string{"pane-a": "build-host", "pane-b": "build-host"}
	manager.Add(&failingBoardSession{id: "pane-a"})
	manager.Add(&fakeBoardSession{id: "pane-b", tag: "pane-b-session"})

	executor := &dynamicBoardExecutor{manager: manager, paneHosts: paneHosts, host: "build-host"}

	// Run several times: findBoardExecutors is sorted by pane ID, so this
	// must succeed deterministically every time, not just probabilistically
	// depending on Go's randomized map iteration order.
	for i := 0; i < 20; i++ {
		out, err := executor.RunBoardCommand(context.Background(), []string{"noop"})
		if err != nil {
			t.Fatalf("RunBoardCommand (attempt %d): %v", i, err)
		}
		if string(out) != "pane-b-session" {
			t.Fatalf("attempt %d: expected fallback past the dead session to pane-b, got %q", i, out)
		}
	}
}

func TestDynamicBoardExecutor_AllCandidatesFail_ReturnsError(t *testing.T) {
	manager := session.NewManager()
	paneHosts := map[string]string{"pane-a": "build-host", "pane-b": "build-host"}
	manager.Add(&failingBoardSession{id: "pane-a"})
	manager.Add(&failingBoardSession{id: "pane-b"})

	executor := &dynamicBoardExecutor{manager: manager, paneHosts: paneHosts, host: "build-host"}
	_, err := executor.RunBoardCommand(context.Background(), []string{"noop"})
	if err == nil {
		t.Fatal("expected an error when every candidate session fails")
	}
}

func TestExpandLocalAgmsgPath_TildePrefixed_ExpandsToHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	got := expandLocalAgmsgPath("~/.agents/skills/agmsg")
	want := filepath.Join(home, ".agents", "skills", "agmsg")
	if got != want {
		t.Fatalf("expandLocalAgmsgPath(~/...) = %q, want %q", got, want)
	}
}

func TestExpandLocalAgmsgPath_AbsolutePath_Unchanged(t *testing.T) {
	got := expandLocalAgmsgPath("/opt/agmsg")
	if got != "/opt/agmsg" {
		t.Fatalf("expandLocalAgmsgPath(/opt/agmsg) = %q, want unchanged", got)
	}
}
