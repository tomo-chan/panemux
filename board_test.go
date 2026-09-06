package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/session"
)

// fakeBoardSession is a minimal session.Session that also implements
// board.BoardExecutor, standing in for SSHSession/TmuxSSHSession in tests
// that don't need a real SSH connection.
type fakeBoardSession struct {
	id     string
	tag    string // distinguishes which fakeBoardSession instance answered a call
	calls  [][]string
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

func (f *fakeBoardSession) RunBoardCommand(_ context.Context, args []string) ([]byte, error) {
	if f.closed {
		return nil, errors.New("fakeBoardSession: use of closed session")
	}
	f.calls = append(f.calls, args)
	return []byte(f.tag), nil
}

// lastBoardCommand returns the argument list of the most recent
// RunBoardCommand call, so a test can tell a command that actually traveled
// over this session's exec channel from one that never left panemux.
func (f *fakeBoardSession) lastBoardCommand() []string {
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
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

func TestResolveAgmsgPathForHost_Local_ExpandsAgainstLocalHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	cfg := &config.Config{AgentBoard: config.AgentBoardConfig{AgmsgPath: "~/.agents/skills/agmsg"}}

	path, ok := resolveAgmsgPathForHost(cfg, session.NewManager(), nil, boardHostIDLocal)
	if !ok {
		t.Fatal("expected ok=true for the local host")
	}
	want := filepath.Join(home, ".agents", "skills", "agmsg")
	if path != want {
		t.Fatalf("resolveAgmsgPathForHost(local) = %q, want %q", path, want)
	}
}

func TestResolveAgmsgPathForHost_RemoteNoReachableSession_False(t *testing.T) {
	cfg := &config.Config{AgentBoard: config.AgentBoardConfig{AgmsgPath: "/opt/agmsg"}}
	manager := session.NewManager()
	paneHosts := map[string]string{"pane-a": "ssh:build-host"}

	_, ok := resolveAgmsgPathForHost(cfg, manager, paneHosts, "ssh:build-host")
	if ok {
		t.Fatal("expected ok=false when no session on the host implements BoardExecutor")
	}
}

func TestResolveAgmsgPathForHost_Remote_ResolvesViaLiveExecutor(t *testing.T) {
	cfg := &config.Config{AgentBoard: config.AgentBoardConfig{AgmsgPath: "/opt/agmsg"}}
	manager := session.NewManager()
	paneHosts := map[string]string{"pane-a": "ssh:build-host"}
	manager.Add(&fakeBoardSession{id: "pane-a", tag: "unused-for-absolute-paths"})

	path, ok := resolveAgmsgPathForHost(cfg, manager, paneHosts, "ssh:build-host")
	if !ok {
		t.Fatal("expected ok=true when a live BoardExecutor session exists on the host")
	}
	if path != "/opt/agmsg" {
		t.Fatalf("resolveAgmsgPathForHost(remote, absolute path) = %q, want unchanged", path)
	}
}

func TestResolveBootstrapPaths_MixOfReachableAndUnreachableHosts(t *testing.T) {
	cfg := &config.Config{AgentBoard: config.AgentBoardConfig{AgmsgPath: "/opt/agmsg"}}
	manager := session.NewManager()
	paneHosts := map[string]string{
		"pane-a": "ssh:reachable-host",
		"pane-b": "ssh:unreachable-host",
	}
	manager.Add(&fakeBoardSession{id: "pane-a", tag: "unused-for-absolute-paths"})

	resolved := resolveBootstrapPaths(cfg, manager, paneHosts)

	if got, ok := resolved["ssh:reachable-host"]; !ok || got != "/opt/agmsg" {
		t.Fatalf("resolved[reachable-host] = (%q, %v), want (/opt/agmsg, true)", got, ok)
	}
	if _, ok := resolved["ssh:unreachable-host"]; ok {
		t.Fatal("expected no entry for a host with no reachable BoardExecutor session")
	}
}

func TestSetupBoard_ReturnsNonNilBootstrapWatcher(t *testing.T) {
	cfg := config.Default()
	manager := session.NewManager()

	_, _, bootstrap := setupBoard(cfg, manager)

	if bootstrap == nil {
		t.Fatal("expected setupBoard to return a non-nil bootstrapWatcher")
	}
	if bootstrap.HasWork() {
		t.Fatal("expected HasWork()=false for a config with no board-enabled panes")
	}
}

// TestAgmsgPresentOnHostLocal covers the gate that keeps the relay from
// polling a host with no agmsg. Without it the relay logged the same exec
// failure every few seconds forever — observed against a real server, 8
// identical errors in the first 25 seconds — which buries the one line
// naming the cause in precisely the situation the README calls the most
// likely first failure.
func TestAgmsgPresentOnHostLocal(t *testing.T) {
	missing := t.TempDir()
	assert.False(t, agmsgPresentOnHost(nil, nil, boardHostIDLocal, missing),
		"no scripts/api.sh means no client, so the relay never polls this host")

	present := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(present, "scripts"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(present, "scripts", "api.sh"), []byte("#!/bin/sh\n"), 0o600))
	assert.True(t, agmsgPresentOnHost(nil, nil, boardHostIDLocal, present))
}

// TestAgmsgPresentOnHostRemoteUncheckable pins the deliberate asymmetry: a
// remote host panemux cannot reach right now is treated as present. The
// alternative — skipping it — would turn a transient connectivity problem
// into a host that stays off the board for the rest of the process's life.
func TestAgmsgPresentOnHostRemoteUncheckable(t *testing.T) {
	assert.True(t, agmsgPresentOnHost(session.NewManager(), map[string]string{}, "remote-host", "/remote/home/demo/agmsg"))
}

// The helpers below sat outside every coverage gate until the root package
// joined COVERAGE_PKGS (issue #180 roadmap item 2). They are the parts of
// board.go with real branching that the existing tests reached only
// indirectly, through setupBoard.

func boolPtr(v bool) *bool { return &v }

func paneConfig(id string, board config.PaneAgentBoardConfig) *config.PaneConfig {
	return &config.PaneConfig{ID: id, Type: "local", AgentBoard: board}
}

func configWithPanes(panes ...*config.PaneConfig) *config.Config {
	children := make([]config.LayoutChild, 0, len(panes))
	for _, pane := range panes {
		children = append(children, config.LayoutChild{Size: float64(100 / max(len(panes), 1)), Pane: pane})
	}
	return &config.Config{
		Workspaces: config.WorkspacesConfig{
			Active: "default",
			Items: []config.WorkspaceConfig{{
				ID:     "default",
				Title:  "Default",
				Layout: config.LayoutNode{Direction: "horizontal", Children: children},
			}},
		},
	}
}

func TestCurrentPaneModes(t *testing.T) {
	tests := []struct {
		want  map[string]string
		name  string
		panes []*config.PaneConfig
	}{
		{
			name:  "no panes at all",
			panes: nil,
			want:  map[string]string{},
		},
		{
			name:  "enabled pane contributes its mode",
			panes: []*config.PaneConfig{paneConfig("a", config.PaneAgentBoardConfig{Enabled: boolPtr(true), Mode: "auto"})},
			want:  map[string]string{"a": "auto"},
		},
		{
			name:  "explicitly disabled pane is omitted",
			panes: []*config.PaneConfig{paneConfig("a", config.PaneAgentBoardConfig{Enabled: boolPtr(false), Mode: "auto"})},
			want:  map[string]string{},
		},
		{
			name:  "an unset Enabled pointer is not enabled",
			panes: []*config.PaneConfig{paneConfig("a", config.PaneAgentBoardConfig{Mode: "auto"})},
			want:  map[string]string{},
		},
		{
			name:  "an enabled pane with no mode still appears, with the empty mode",
			panes: []*config.PaneConfig{paneConfig("a", config.PaneAgentBoardConfig{Enabled: boolPtr(true)})},
			want:  map[string]string{"a": ""},
		},
		{
			name: "a mix reports only the enabled panes",
			panes: []*config.PaneConfig{
				paneConfig("a", config.PaneAgentBoardConfig{Enabled: boolPtr(true), Mode: "auto"}),
				paneConfig("b", config.PaneAgentBoardConfig{Enabled: boolPtr(false), Mode: "manual"}),
				paneConfig("c", config.PaneAgentBoardConfig{Enabled: boolPtr(true), Mode: "manual"}),
			},
			want: map[string]string{"a": "auto", "c": "manual"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, currentPaneModes(configWithPanes(tc.panes...)))
		})
	}
}

// The "ssh:" prefix is what keeps an SSH connection alias named literally
// "local" from colliding with boardHostIDLocal — nothing in the config's own
// connection-name validation forbids that alias, so the separation has to be
// structural. See boardHostForPane's own comment.
func TestBoardHostForPane(t *testing.T) {
	tests := []struct {
		name string
		pane *config.PaneConfig
		want string
	}{
		{"local", &config.PaneConfig{Type: "local"}, boardHostIDLocal},
		{"tmux is still this host", &config.PaneConfig{Type: "tmux"}, boardHostIDLocal},
		{"ssh", &config.PaneConfig{Type: "ssh", Connection: "demo"}, "ssh:demo"},
		{"ssh_tmux", &config.PaneConfig{Type: "ssh_tmux", Connection: "demo"}, "ssh:demo"},
		{
			name: "an alias named \"local\" cannot collide with the local host id",
			pane: &config.PaneConfig{Type: "ssh", Connection: boardHostIDLocal},
			want: "ssh:local",
		},
		{"an unknown type falls back to this host", &config.PaneConfig{Type: "nonesuch"}, boardHostIDLocal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, boardHostForPane(tc.pane))
			if tc.want != boardHostIDLocal {
				assert.NotEqual(t, boardHostIDLocal, boardHostForPane(tc.pane))
			}
		})
	}
}

func TestPersistBoardCursors_WritesAFileLoadCursorFileCanReadBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "panemux"), 0o700))

	entries := []board.CursorEntry{{Host: "local", Team: "demo", Cursor: "abc"}}
	persistBoardCursors(entries)

	path, err := board.DefaultCursorFilePath()
	require.NoError(t, err)
	got, err := board.LoadCursorFile(path)
	require.NoError(t, err)
	assert.Equal(t, entries, got)
}

// unwritableHome points HOME at a directory whose .config can never be
// created, and returns once that is true.
//
// Making these saves actually fail takes more than an absent directory, which
// is the trap an earlier version of both tests below fell into: the persist
// helpers go through board.atomicWriteFile, whose first statement is
// os.MkdirAll, so a missing HOME is simply created on demand and the write
// SUCCEEDS. Those tests therefore drove the happy path while claiming to cover
// the failure branch — tautological tests of exactly the shape this
// repository's own red-check (docs/quality-gateway.md, D4) exists to catch,
// and `go tool cover` showed it: both functions sat at 50%.
//
// A plain FILE where the directory has to go is genuinely unwritable, and
// unlike a permission bit it cannot be bypassed by running as root: MkdirAll
// fails with ENOTDIR for every uid. internal/config's
// TestEnsureAuthToken_WriteFailure_NonFatal_LoopbackHost uses the same shape.
//
// It is one helper rather than two copies so the two tests cannot drift into
// disagreeing about what "unwritable" means — one of them silently going green
// again is the failure being guarded against.
func unwritableHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config"), []byte("not a directory"), 0o600))
}

// A save failure is a warning, never a crash and never a startup failure: the
// relay's cursor is an optimisation, and losing it costs a re-read, not
// correctness.
func TestPersistBoardCursors_UnwritableLocation_DoesNotPanic(t *testing.T) {
	unwritableHome(t)

	assert.NotPanics(t, func() {
		persistBoardCursors([]board.CursorEntry{{Host: "local", Team: "demo", Cursor: "abc"}})
	})

	path, err := board.DefaultCursorFilePath()
	require.NoError(t, err)
	_, statErr := os.Stat(path)
	assert.Error(t, statErr, "the save must actually have failed for this test to mean anything")
}

func TestPersistBootstrapState_WritesAFileLoadBootstrapStateCanReadBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "panemux"), 0o700))

	persistBootstrapState([]string{"pane-a", "pane-b"})

	path, err := board.DefaultBootstrapStateFilePath()
	require.NoError(t, err)
	got, err := board.LoadBootstrapState(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"pane-a", "pane-b"}, got)
}

func TestPersistBootstrapState_UnwritableLocation_DoesNotPanic(t *testing.T) {
	unwritableHome(t)

	assert.NotPanics(t, func() { persistBootstrapState([]string{"pane-a"}) })

	path, err := board.DefaultBootstrapStateFilePath()
	require.NoError(t, err)
	_, statErr := os.Stat(path)
	assert.Error(t, statErr, "the save must actually have failed for this test to mean anything")
}

// installAgmsg creates the shape LocalAgmsgPresent looks for: an install
// root carrying scripts/api.sh. version, when non-empty, is written to the
// VERSION file agmsg's own installer maintains.
func installAgmsg(t *testing.T, root, version string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "scripts"), 0o700))
	// LocalAgmsgPresent only stats this file, so a non-executable stub is
	// enough — and gosec's G306 wants 0600 or less even on a fixture.
	require.NoError(t, os.WriteFile(filepath.Join(root, "scripts", "api.sh"), []byte("#!/bin/sh\n"), 0o600))
	if version != "" {
		require.NoError(t, os.WriteFile(filepath.Join(root, "VERSION"), []byte(version+"\n"), 0o600))
	}
	return root
}

func TestNewAgmsgClientForHost_LocalWithAgmsgInstalled_ReturnsClient(t *testing.T) {
	agmsgPath := installAgmsg(t, t.TempDir(), board.TestedAgmsgVersion)
	cfg := &config.Config{AgentBoard: config.AgentBoardConfig{AgmsgPath: agmsgPath}}

	client, ok := newAgmsgClientForHost(cfg, session.NewManager(), map[string]string{}, boardHostIDLocal)

	require.True(t, ok)
	require.NotNil(t, client)
	// The mirror of the remote arm's own assertion (issue #209): which
	// implementation came back is the whole answer this function gives, so
	// asserting only non-nil would let either arm return the other's client
	// unnoticed.
	assert.IsType(t, &board.LocalAgmsgClient{}, client)
	assert.Equal(t, boardHostIDLocal, client.HostID())
}

// An absent agmsg is the README's most likely first failure, and the host is
// skipped rather than given a client that would log the same exec failure
// every poll for the life of the process.
func TestNewAgmsgClientForHost_LocalWithoutAgmsg_SkipsTheHost(t *testing.T) {
	cfg := &config.Config{AgentBoard: config.AgentBoardConfig{AgmsgPath: t.TempDir()}}

	client, ok := newAgmsgClientForHost(cfg, session.NewManager(), map[string]string{}, boardHostIDLocal)

	assert.False(t, ok)
	assert.Nil(t, client)
}

// A remote host with no reachable pane cannot have its agmsg_path resolved at
// all, so it is skipped before the presence probe is even reached.
//
// The log line is asserted because newAgmsgClientForHost has three ways to
// return (nil, false) and they are indistinguishable from the return value
// alone: no reachable session, a failed $HOME probe, and an absent agmsg
// install. Only the line names which one this test actually reached.
func TestNewAgmsgClientForHost_RemoteWithNoReachablePane_SkipsTheHost(t *testing.T) {
	cfg := &config.Config{AgentBoard: config.AgentBoardConfig{AgmsgPath: "/remote/home/demo/agmsg"}}
	buf := captureBoardLog(t)

	client, ok := newAgmsgClientForHost(cfg, session.NewManager(), map[string]string{"pane-a": "ssh:demo"}, "ssh:demo")

	assert.False(t, ok)
	assert.Nil(t, client)
	assert.Contains(t, buf.String(), `no reachable session for host "ssh:demo"`)
}

func TestWarnOnAgmsgVersionMismatch_DoesNotPanicAcrossHostShapes(t *testing.T) {
	matching := installAgmsg(t, filepath.Join(t.TempDir(), "matching"), board.TestedAgmsgVersion)
	mismatched := installAgmsg(t, filepath.Join(t.TempDir(), "mismatched"), "0.0.1")
	unversioned := installAgmsg(t, filepath.Join(t.TempDir(), "unversioned"), "")

	tests := []struct {
		paneHosts     map[string]string
		resolvedPaths map[string]string
		name          string
	}{
		{
			name:          "a matching install warns about nothing",
			paneHosts:     map[string]string{"pane-a": boardHostIDLocal},
			resolvedPaths: map[string]string{boardHostIDLocal: matching},
		},
		{
			name:          "a mismatched install is a warning, never a refusal",
			paneHosts:     map[string]string{"pane-a": boardHostIDLocal},
			resolvedPaths: map[string]string{boardHostIDLocal: mismatched},
		},
		{
			name:          "an install with no VERSION file is unknown, not broken",
			paneHosts:     map[string]string{"pane-a": boardHostIDLocal},
			resolvedPaths: map[string]string{boardHostIDLocal: unversioned},
		},
		{
			name:          "a host with no resolved path is skipped",
			paneHosts:     map[string]string{"pane-a": boardHostIDLocal},
			resolvedPaths: map[string]string{},
		},
		{
			name:          "a remote host with no reachable executor is skipped",
			paneHosts:     map[string]string{"pane-a": "ssh:demo"},
			resolvedPaths: map[string]string{"ssh:demo": "/remote/home/demo/agmsg"},
		},
		{
			name:          "no board-enabled pane at all",
			paneHosts:     map[string]string{},
			resolvedPaths: map[string]string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				warnOnAgmsgVersionMismatch(session.NewManager(), tc.paneHosts, tc.resolvedPaths)
			})
		})
	}
}

// VersionMismatchWarning is the decision warnOnAgmsgVersionMismatch delegates
// to, and unlike the logging above it has a return value to assert, so the
// mismatch/match distinction is pinned here rather than only observed as
// "did not panic".
func TestVersionMismatchWarning_DistinguishesMatchFromMismatch(t *testing.T) {
	assert.Empty(t, board.VersionMismatchWarning("local", board.TestedAgmsgVersion))
	assert.NotEmpty(t, board.VersionMismatchWarning("local", "0.0.1"))
}
