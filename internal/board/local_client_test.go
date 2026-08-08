package board

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// fakeCommand builds a real *exec.Cmd that runs a tiny inline shell script
// instead of the real agmsg scripts, so tests exercise the full exec.Cmd
// plumbing (argv, CombinedOutput/Output) without depending on a real agmsg
// install. It records the invoked name+args for assertions.
type execFunc func(ctx context.Context, name string, args ...string) *exec.Cmd

func fakeCommandRecorder(t *testing.T, script string) (execFunc, *[]string) {
	t.Helper()
	var recorded []string
	fn := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		recorded = append([]string{name}, args...)
		full := append([]string{"-c", script, "--"}, args...)
		return exec.CommandContext(ctx, "/bin/sh", full...)
	}
	return fn, &recorded
}

func TestLocalAgmsgClient_Send_AlwaysIncludesForce(t *testing.T) {
	execFn, recorded := fakeCommandRecorder(t, "exit 0")
	c := &LocalAgmsgClient{agmsgPath: "/opt/agmsg", execFn: execFn}

	if err := c.Send(context.Background(), "panemux", "pane-a", "pane-b", "please review"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := *recorded
	if got[len(got)-1] != "--force" {
		t.Fatalf("expected the last argument to be --force, got %v", got)
	}
	if !strings.HasSuffix(got[0], "scripts/send.sh") {
		t.Fatalf("expected send.sh to be invoked, got %v", got)
	}
	wantArgs := []string{"panemux", "pane-a", "pane-b", "please review", "--force"}
	for i, a := range wantArgs {
		if got[i+1] != a {
			t.Fatalf("arg[%d] = %q, want %q (full: %v)", i, got[i+1], a, got)
		}
	}
}

func TestLocalAgmsgClient_Send_PropagatesError(t *testing.T) {
	execFn, _ := fakeCommandRecorder(t, "echo boom >&2; exit 1")
	c := &LocalAgmsgClient{agmsgPath: "/opt/agmsg", execFn: execFn}

	err := c.Send(context.Background(), "panemux", "pane-a", "pane-b", "x")
	if err == nil {
		t.Fatal("expected an error from a non-zero send.sh exit")
	}
}

func TestLocalAgmsgClient_Since_BuildsExpectedArgsAndFiltersAfterID(t *testing.T) {
	fixture := string(readFixture(t, "messages-basic.jsonl"))
	execFn, recorded := fakeCommandRecorder(t, "cat <<'EOF'\n"+fixture+"EOF\n")
	c := &LocalAgmsgClient{agmsgPath: "/opt/agmsg", execFn: execFn}

	rows, err := c.Since(context.Background(), "panemux", "101", 50)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "102" {
		t.Fatalf("expected only id 102 after afterID=101, got %+v", rows)
	}

	got := *recorded
	if !strings.HasSuffix(got[0], "scripts/api.sh") {
		t.Fatalf("expected api.sh to be invoked, got %v", got)
	}
	want := []string{"get", "teams", "panemux", "messages", "--limit", "50"}
	for i, a := range want {
		if got[i+1] != a {
			t.Fatalf("arg[%d] = %q, want %q (full: %v)", i, got[i+1], a, got)
		}
	}
	// Regression guard: the relay's forward poll must never pass
	// --before-id (api.sh has no forward/since read — see
	// docs/agent-board.md's "Integration with agmsg").
	for _, a := range got {
		if a == "--before-id" {
			t.Fatalf("Since must never pass --before-id, got args %v", got)
		}
	}
}

func TestLocalAgmsgClient_Since_EmptyAfterIDReturnsEverything(t *testing.T) {
	fixture := string(readFixture(t, "messages-basic.jsonl"))
	execFn, _ := fakeCommandRecorder(t, "cat <<'EOF'\n"+fixture+"EOF\n")
	c := &LocalAgmsgClient{agmsgPath: "/opt/agmsg", execFn: execFn}

	rows, err := c.Since(context.Background(), "panemux", "", 50)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows with an empty afterID, got %d", len(rows))
	}
}

func TestLocalAgmsgClient_HostID(t *testing.T) {
	c := NewLocalAgmsgClient("/opt/agmsg")
	if c.HostID() != LocalHostID {
		t.Fatalf("HostID() = %q, want %q", c.HostID(), LocalHostID)
	}
}
