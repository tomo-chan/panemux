package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/homedir"
)

// attentionOutput is one host's collection with a waiting claude session, a
// stopped transcript and a stopped codex rollout. The attention collection
// never asks for the last two; they are here to show they are not listed
// when a host prints them anyway.
func attentionOutput() []byte {
	return joinLines(
		"::panemux-tasks v1",
		"::now 1000",
		"::section state",
		"::file 7.json",
		`{"pid":7,"sessionId":"live-sess","status":"waiting","waitingFor":"approve Bash","statusUpdatedAt":990000}`,
		"::section ps",
		"7 1 claude",
		"::section tmux",
		"::section cwd",
		"::section transcripts",
		"990\tstopped-sess.jsonl\t\"cwd\":\"/workspace/user/project\"\t10",
		"::section codex-rollouts",
		"990\t10\t"+rolloutName("2026-09-27T09-00-00", codexSessionB)+"\t\t\"originator\":\"codex-tui\"",
		"::end",
	)
}

// The attention collection runs the live part of the script only, lists
// only running tasks, and signs a wait exactly as the full collection does.
func TestCollectAttention_ListsLiveTasksWithTheSameSignature(t *testing.T) {
	var scripts []string
	svc := New(Options{
		RunLocal: func(_ context.Context, script string) ([]byte, error) {
			scripts = append(scripts, script)
			return attentionOutput(), nil
		},
		Now: (&clock{now: collectedAt}).Now,
	})
	defer svc.Close()

	full := svc.Collect(context.Background())
	attention := svc.CollectAttention(context.Background())

	require.Equal(t, []string{collectScript, attentionScript}, scripts)
	assert.Equal(t, full.Hosts, attention.Hosts)
	require.Len(t, attention.Tasks, 1, "stopped sessions are not listed: %+v", attention.Tasks)
	live := findTask(t, full.Tasks, "local:claude:live-sess")
	assert.Len(t, full.Tasks, 3, "the full collection lists the stopped ones")
	assert.Equal(t, live.ID, attention.Tasks[0].ID)
	assert.NotEmpty(t, attention.Tasks[0].WaitSignature)
	assert.Equal(t, live.WaitSignature, attention.Tasks[0].WaitSignature)
	assert.Equal(t, live.StatusSince, attention.Tasks[0].StatusSince)
	assert.Nil(t, attention.Tasks[0].Log, "no log version: summaries are not involved")
}

// The attention collection leaves what the summaries know alone: replacing
// a host's summary tasks with a list that has no logs would drop them.
func TestCollectAttention_LeavesTheSummaryTasksAlone(t *testing.T) {
	out := joinLines(
		"::panemux-tasks v1",
		"::now 1000",
		"::section state",
		"::file 7.json",
		`{"pid":7,"sessionId":"live-sess","status":"busy","statusUpdatedAt":990000}`,
		"::section ps",
		"7 1 claude",
		"::section transcripts",
		"990\tlive-sess.jsonl\t\t10",
		"::end",
	)
	svc := New(Options{RunLocal: localOutput(out, nil), Now: (&clock{now: collectedAt}).Now})
	defer svc.Close()

	svc.Collect(context.Background())
	before := svc.summaryTasks[""]
	require.Len(t, before, 1)

	svc.CollectAttention(context.Background())
	assert.Equal(t, before, svc.summaryTasks[""])
}

// Every host is collected on its own: one that fails, or is still
// connecting, is reported in its entry and does not hide the others, and a
// remote host gets the attention script on stdin over its connection.
func TestCollectAttention_FailuresStayPerHost(t *testing.T) {
	good := &fakeConn{output: attentionOutput()}
	dialer := &fakeDialer{conns: []*fakeConn{good}}
	svc := New(Options{
		Hosts:    func() []string { return []string{"zeta", "alpha"} },
		Dial:     func(name string) (Conn, error) { return dialByName(name, dialer) },
		RunLocal: localOutput(nil, errors.New("sh: not found")),
		Now:      (&clock{now: collectedAt}).Now,
	})
	defer svc.Close()

	snap := svc.CollectAttention(context.Background())
	names := []string{}
	for _, h := range snap.Hosts {
		names = append(names, h.Name)
	}
	assert.Equal(t, []string{"", "alpha", "zeta"}, names)
	assert.Equal(t, HostError, hostResult(t, snap, "").Status)
	assert.Equal(t, HostOK, hostResult(t, snap, "alpha").Status)
	assert.Equal(t, HostError, hostResult(t, snap, "zeta").Status)

	require.Len(t, snap.Tasks, 1)
	assert.Equal(t, "ssh:alpha:claude:live-sess", snap.Tasks[0].ID)
	assert.NotEmpty(t, snap.Tasks[0].WaitSignature)
	assert.Equal(t, []string{"sh -s"}, good.cmds)
	assert.Equal(t, []string{attentionScript}, good.stdins)
}

// With every host failing the answer still lists the hosts, and no tasks.
func TestCollectAttention_AllHostsFailing(t *testing.T) {
	svc := New(Options{RunLocal: localOutput([]byte("not the protocol"), nil), Now: (&clock{now: collectedAt}).Now})
	defer svc.Close()

	snap := svc.CollectAttention(context.Background())
	require.Len(t, snap.Hosts, 1)
	assert.Equal(t, HostError, snap.Hosts[0].Status)
	assert.NotNil(t, snap.Tasks)
	assert.Empty(t, snap.Tasks)
}

// The attention script is the full script without its two searches for
// stopped sessions: the live part is shared, not copied.
func TestAttentionScript_IsTheLivePartOfTheCollection(t *testing.T) {
	for _, stopped := range []string{
		".claude/projects", ".codex/sessions", "::section transcripts", "::section codex-rollouts", "find ",
	} {
		assert.NotContains(t, attentionScript, stopped)
		assert.Contains(t, collectScript, stopped)
	}
	assert.True(t, strings.HasPrefix(collectScript, collectLiveScript))
	assert.True(t, strings.HasPrefix(attentionScript, collectLiveScript))
	assert.True(t, strings.HasSuffix(attentionScript, "echo '::end'\nexit 0\n"))
}

// Run for real, the attention script reports the live state files and no
// transcripts, even with conversation logs and rollouts on the host.
func TestRunLocal_AttentionScriptSkipsStoppedSessions(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)

	sessions := filepath.Join(home, ".claude", "sessions")
	require.NoError(t, os.MkdirAll(sessions, 0o700))
	selfPID := os.Getpid()
	require.NoError(t, os.WriteFile(filepath.Join(sessions, strconv.Itoa(selfPID)+".json"),
		[]byte(`{"pid":`+strconv.Itoa(selfPID)+`,"sessionId":"abc","status":"idle"}`), 0o600))
	project := filepath.Join(home, ".claude", "projects", "-workspace-user-project")
	require.NoError(t, os.MkdirAll(project, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(project, "stopped-one.jsonl"), []byte(`{"cwd":"/x"}`+"\n"), 0o600))
	rollouts := filepath.Join(home, ".codex", "sessions", "2026", "09", "27")
	require.NoError(t, os.MkdirAll(rollouts, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(rollouts, rolloutName("2026-09-27T09-00-00", codexSessionB)),
		[]byte(`{"payload":{"cwd":"/x","originator":"codex-tui"}}`+"\n"), 0o600))

	out, err := runLocal(context.Background(), attentionScript)
	require.NoError(t, err)
	raw, err := parseCollectOutput(out)
	require.NoError(t, err)
	require.Len(t, raw.StateFiles, 1)
	assert.Empty(t, raw.Transcripts)
	assert.Empty(t, raw.CodexRollouts)
}

// The task event publisher observes each host on its own cycle, so it
// collects one host at a time; it reads the same live part of the collection.
func TestCollectHostLive_IsOneHostOfTheAttentionCollection(t *testing.T) {
	good := &fakeConn{output: attentionOutput()}
	dialer := &fakeDialer{conns: []*fakeConn{good}}
	var scripts []string
	svc := New(Options{
		Hosts: func() []string { return []string{"alpha", ""} },
		Dial:  func(name string) (Conn, error) { return dialByName(name, dialer) },
		RunLocal: func(_ context.Context, script string) ([]byte, error) {
			scripts = append(scripts, script)
			return attentionOutput(), nil
		},
		Now: (&clock{now: collectedAt}).Now,
	})
	defer svc.Close()

	assert.Equal(t, []string{"alpha"}, svc.Hosts(), "the panemux host is not a configured host")

	result, live := svc.CollectHostLive(context.Background(), "")
	assert.Equal(t, HostOK, result.Status)
	assert.Equal(t, []string{attentionScript}, scripts)
	require.Len(t, live, 1)
	assert.Equal(t, "local:claude:live-sess", live[0].ID)

	result, live = svc.CollectHostLive(context.Background(), "alpha")
	assert.Equal(t, "alpha", result.Name)
	assert.Equal(t, HostOK, result.Status)
	require.Len(t, live, 1)
	assert.Equal(t, "ssh:alpha:claude:live-sess", live[0].ID)
	assert.Equal(t, []string{attentionScript}, good.stdins)
}

// Reading the hosts forgets the connection of one no longer configured.
func TestServiceHosts_ForgetsRemovedHosts(t *testing.T) {
	conn := &fakeConn{output: attentionOutput()}
	dialer := &fakeDialer{conns: []*fakeConn{conn}}
	hosts := []string{"alpha"}
	svc := New(Options{
		Hosts:    func() []string { return hosts },
		Dial:     func(name string) (Conn, error) { return dialByName(name, dialer) },
		RunLocal: localOutput(attentionOutput(), nil),
		Now:      (&clock{now: collectedAt}).Now,
	})
	defer svc.Close()
	svc.CollectHostLive(context.Background(), "alpha")
	require.NotNil(t, svc.openConn("alpha"))

	hosts = nil
	assert.Empty(t, svc.Hosts())
	assert.Nil(t, svc.openConn("alpha"))
}
