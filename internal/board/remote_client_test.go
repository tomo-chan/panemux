package board

import (
	"context"
	"errors"
	"testing"
)

type fakeBoardExecutor struct { //nolint:govet // fieldalignment: clarity preferred
	calls   [][]string
	outputs [][]byte
	err     error
}

// RunBoardCommand records scriptPath and args flattened into one slice
// (scriptPath first) so existing index-based assertions in this file don't
// need to change shape — the real interface keeps them as separate
// parameters; see internal/session.BoardExecutor's doc comment for why.
func (f *fakeBoardExecutor) RunBoardCommand(_ context.Context, scriptPath string, args []string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{scriptPath}, args...))
	if f.err != nil {
		return nil, f.err
	}
	if len(f.outputs) > 0 {
		out := f.outputs[0]
		f.outputs = f.outputs[1:]
		return out, nil
	}
	return nil, nil
}

func TestRemoteAgmsgClient_Send_AlwaysIncludesForce(t *testing.T) {
	exec := &fakeBoardExecutor{}
	c := NewRemoteAgmsgClient("build-host", "/opt/agmsg", exec)

	if err := c.Send(context.Background(), "panemux", "pane-a", "pane-b", "please review; `rm -rf /`"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("expected exactly one RunBoardCommand call, got %d", len(exec.calls))
	}
	args := exec.calls[0]
	want := []string{"/opt/agmsg/scripts/send.sh", "panemux", "pane-a", "pane-b", "please review; `rm -rf /`", forceFlag}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
	// Escaping is RunBoardCommand's job (see internal/session), not
	// RemoteAgmsgClient's — the raw, unescaped body must reach the
	// executor's args slice untouched.
	if args[4] != "please review; `rm -rf /`" {
		t.Fatalf("expected the raw body to pass through unescaped to the executor, got %q", args[4])
	}
}

func TestRemoteAgmsgClient_Send_PropagatesError(t *testing.T) {
	exec := &fakeBoardExecutor{err: errors.New("ssh exec failed")}
	c := NewRemoteAgmsgClient("build-host", "/opt/agmsg", exec)
	if err := c.Send(context.Background(), "panemux", "a", "b", "x"); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestRemoteAgmsgClient_Since_BuildsExpectedArgs(t *testing.T) {
	exec := &fakeBoardExecutor{outputs: [][]byte{readFixture(t, "messages-basic.jsonl")}}
	c := NewRemoteAgmsgClient("build-host", "/opt/agmsg", exec)

	rows, err := c.Since(context.Background(), "panemux", "101", 50)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "102" || rows[0].Host != "build-host" {
		t.Fatalf("unexpected rows: %+v", rows)
	}

	args := exec.calls[0]
	want := []string{"/opt/agmsg/scripts/api.sh", apiVerbGet, apiNounTeams, "panemux", apiNounMessages, limitFlag, "50"}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

// Regression test for the earlier, incorrect "reads are digit-validated so
// don't need escaping" claim: a team value containing shell metacharacters
// must still reach the executor as a single raw argument (escaping happens
// inside RunBoardCommand, not here), never pre-mangled or split by
// RemoteAgmsgClient itself.
func TestRemoteAgmsgClient_Since_TeamWithMetacharactersPassedAsSingleArg(t *testing.T) {
	exec := &fakeBoardExecutor{outputs: [][]byte{nil}}
	c := NewRemoteAgmsgClient("build-host", "/opt/agmsg", exec)

	dangerousTeam := "panemux'; rm -rf / #"
	if _, err := c.Since(context.Background(), dangerousTeam, "", 30); err != nil {
		t.Fatalf("Since: %v", err)
	}
	args := exec.calls[0]
	if args[3] != dangerousTeam {
		t.Fatalf("expected team arg to pass through as a single raw value, got %q", args[3])
	}
}

func TestRemoteAgmsgClient_Since_RunBoardCommandErrorPropagates(t *testing.T) {
	exec := &fakeBoardExecutor{err: errors.New("ssh exec failed")}
	c := NewRemoteAgmsgClient("build-host", "/opt/agmsg", exec)
	if _, err := c.Since(context.Background(), "panemux", "", 50); err == nil {
		t.Fatal("expected an error when RunBoardCommand fails")
	}
}

func TestRemoteAgmsgClient_Since_ParseFailurePropagates(t *testing.T) {
	exec := &fakeBoardExecutor{outputs: [][]byte{[]byte("not json\n")}}
	c := NewRemoteAgmsgClient("build-host", "/opt/agmsg", exec)
	if _, err := c.Since(context.Background(), "panemux", "", 50); err == nil {
		t.Fatal("expected an error when api.sh output cannot be parsed")
	}
}

func TestRemoteAgmsgClient_HostID(t *testing.T) {
	c := NewRemoteAgmsgClient("build-host", "/opt/agmsg", &fakeBoardExecutor{})
	if c.HostID() != "build-host" {
		t.Fatalf("HostID() = %q", c.HostID())
	}
}
