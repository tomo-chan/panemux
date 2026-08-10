package main

import (
	"context"
	"errors"
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
