package tasks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panemux/internal/homedir"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	codexSessionA = "01a0e2b9-d054-7cc2-9278-a5e25ebcc524"
	codexSessionB = "01a0e2bc-bdf9-71b2-867a-df8302180efe"
)

func rolloutName(stamp, sessionID string) string {
	return "rollout-" + stamp + "-" + sessionID + ".jsonl"
}

// Each row of the codex-open section is one rollout a codex process holds
// open: its pid, how long it has run (ps's etime), the rollout's mtime and
// size, the newest thread_turns row, the last turn event and response item
// the rollout's tail holds, the cwd of its session_meta, and its path.
func TestParseCollectOutput_ReadsTheCodexSections(t *testing.T) {
	openPath := "/remote/home/demo/.codex/sessions/2026/09/27/" + rolloutName("2026-09-27T11-57-03", codexSessionA)
	out := joinLines(
		"::panemux-tasks v1",
		"::now 1790510500",
		"::section codex-open",
		"899\t   02:05",
		"901\t??",
		"899\t   02:05\t1790510485 48396\tinProgress 1790510480\t"+
			`{"timestamp":"2026-09-27T11:58:30.057Z","ordinal":40,"type":"event_msg",`+
			`"payload":{"type":"task_started","turn_id":"x"}}`+"\t"+
			`{"timestamp":"2026-09-27T11:59:52.794Z","ordinal":67,"type":"response_item",`+
			`"payload":{"type":"function_call","id":"fc_1","name":"request_user_input","arguments":"{}"`+"\t"+
			`"cwd":"/tmp/sample-project"`+"\t"+openPath,
		"900\t1-02:03:04\t1790510000 10\t\t\t\t\t/remote/home/demo/.codex/sessions/2026/09/27/"+
			rolloutName("2026-09-27T12-00-15", codexSessionB),
		"::section codex-rollouts",
		"1790510485\t48396\t"+rolloutName("2026-09-27T11-57-03", codexSessionA)+"\t"+
			`"cwd":"/tmp/sample-project"`+"\t"+`"source":"cli"`,
		"1790500000\t17\t"+rolloutName("2026-09-27T09-00-00", codexSessionB)+"\t\t",
		"::end",
	)

	raw, err := parseCollectOutput(out)
	require.NoError(t, err)

	assert.Equal(t, map[int]int64{899: 125}, raw.CodexAges, "a process whose etime ps could not give has no age")
	assert.Equal(t, []codexOpenRollout{
		{
			PID: 899, Elapsed: 125, File: rolloutName("2026-09-27T11-57-03", codexSessionA),
			Rollout: codexRollout{SessionID: codexSessionA, CWD: "/tmp/sample-project", ModTime: 1790510485, Size: 48396},
			Turn: codexTurn{
				DBStatus: "inProgress", DBStartedAt: 1790510480,
				Event: "task_started", EventAt: 1790510310057,
				LastItem: "function_call", LastItemName: "request_user_input",
			},
		},
		{
			PID: 900, Elapsed: 93784, File: rolloutName("2026-09-27T12-00-15", codexSessionB),
			Rollout: codexRollout{SessionID: codexSessionB, ModTime: 1790510000, Size: 10},
		},
	}, raw.CodexOpen)
	assert.Equal(t, []codexRollout{
		{SessionID: codexSessionA, CWD: "/tmp/sample-project", Source: "cli", ModTime: 1790510485, Size: 48396},
		{SessionID: codexSessionB, ModTime: 1790500000, Size: 17},
	}, raw.CodexRollouts)
}

func TestParseCollectOutput_SkipsMalformedCodexRows(t *testing.T) {
	good := rolloutName("2026-09-27T11-57-03", codexSessionA)
	out := joinLines(
		"::panemux-tasks v1",
		"::now 5",
		"::section codex-open",
		"x\t00:10",
		"0\t00:10",
		"899\t00:01\t1\t\t\t\t\t/x/"+good,
		"899\t00:01\t1 x\t\t\t\t\t/x/"+good,
		"not-a-pid\t00:01\t1 1\t\t\t\t\t/x/"+good,
		"0\t00:01\t1 1\t\t\t\t\t/x/"+good,
		"899\t00:01\t1 1\t\t\t\t", // too few fields
		"899\t00:01\tnope\t\t\t\t\t/x/"+good,
		"899\t00:01\t1 1\t\t\t\t\t/x/rollout-2026-09-27T11-57-03-not-a-uuid.jsonl",
		"899\t00:01\t1 1\t\t\t\t\t/x/other.jsonl",
		"::section codex-rollouts",
		"x\t1\t"+good+"\t\t",
		"1\tx\t"+good+"\t\t",
		"1\t1\trollout-2026-09-27T11-57-03-"+codexSessionA+"-extra.jsonl\t\t",
		"1\t1",
		"::end",
	)
	raw, err := parseCollectOutput(out)
	require.NoError(t, err)
	assert.Empty(t, raw.CodexOpen)
	assert.Empty(t, raw.CodexRollouts)
	assert.Empty(t, raw.CodexAges)
}

func TestJSONFragmentString(t *testing.T) {
	assert.Equal(t, "cli", jsonFragmentString(`"source":"cli"`, `"source":`))
	assert.Empty(t, jsonFragmentString(`"cwd":"cli"`, `"source":`), "another key")
	assert.Empty(t, jsonFragmentString(`"source":"a\q"`, `"source":`), "not a string codex could have written")
}

// A row whose etime ps could not give still names the rollout; the process's
// age is then unknown (-1), and whatever else is missing reads as empty.
func TestParseCollectOutput_CodexOpenRowWithOnlyTheRollout(t *testing.T) {
	name := rolloutName("2026-09-27T11-57-03", codexSessionA)
	raw, err := parseCollectOutput(joinLines(
		"::panemux-tasks v1",
		"::now 5",
		"::section codex-open",
		"899\t\t1790510485 12\tgarbage\tnot json\t{\"type\":\"response_item\"}\t\t/x/"+name,
		"::end",
	))
	require.NoError(t, err)
	require.Len(t, raw.CodexOpen, 1)
	got := raw.CodexOpen[0]
	assert.Equal(t, int64(-1), got.Elapsed)
	assert.Equal(t, codexTurn{}, got.Turn,
		"a thread_turns row without a start time and fragments without the markers read as nothing")
}

func TestParseEtime(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"00:00", 0},
		{"05", -1},
		{"02:05", 125},
		{"  02:05 ", 125},
		{"1:02:05", 3725},
		{"3-01:02:05", 3*86400 + 3725},
		{"", -1},
		{"a:b", -1},
		{"1:2:3:4", -1},
		{"x-01:02:03", -1},
		{"-1:00", -1},
		{"01:-5", -1},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, parseEtime(tt.in), "%q", tt.in)
	}
}

func TestRolloutSessionID(t *testing.T) {
	id, ok := rolloutSessionID(rolloutName("2026-09-27T11-57-03", codexSessionA))
	assert.True(t, ok)
	assert.Equal(t, codexSessionA, id)

	for _, bad := range []string{
		"rollout-" + codexSessionA + ".jsonl",
		rolloutName("2026-09-27T11-57-03", "--help"),
		rolloutName("2026-09-27T11-57-03", codexSessionA) + ".bak",
		"x" + rolloutName("2026-09-27T11-57-03", codexSessionA),
	} {
		_, ok := rolloutSessionID(bad)
		assert.False(t, ok, bad)
	}
}

func TestParseCodexTurn(t *testing.T) {
	tests := []struct {
		name, db, event, item string
		want                  codexTurn
	}{
		{name: "nothing", want: codexTurn{}},
		{name: "db row", db: "completed 1790510240", want: codexTurn{DBStatus: "completed", DBStartedAt: 1790510240}},
		{name: "db row without a start", db: "failed ", want: codexTurn{DBStatus: "failed"}},
		{name: "db row with a start that is not a number", db: "failed x", want: codexTurn{DBStatus: "failed"}},
		{
			name: "turn aborted",
			event: `{"timestamp":"2026-09-27T11:58:30.5Z","type":"event_msg",` +
				`"payload":{"type":"turn_aborted","reason":"interrupted"}}`,
			want: codexTurn{Event: "turn_aborted", EventAt: 1790510310500},
		},
		{
			name:  "event without a timestamp",
			event: `{"type":"event_msg","payload":{"type":"task_complete"}}`,
			want:  codexTurn{Event: "task_complete"},
		},
		{
			name:  "event with an unreadable timestamp",
			event: `{"timestamp":"yesterday","type":"event_msg","payload":{"type":"task_complete"}}`,
			want:  codexTurn{Event: "task_complete"},
		},
		{
			name: "an item that is not a call has no name",
			item: `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[]}}`,
			want: codexTurn{LastItem: "message"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseCodexTurn(tt.db, tt.event, tt.item))
		})
	}
}

// writeRollout writes a rollout file under home's ~/.codex/sessions.
func writeRollout(t *testing.T, home, day, name string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(append([]string{home, ".codex", "sessions"}, strings.Split(day, "/")...)...)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return path
}

func sessionMeta(sessionID, cwd, source string) string {
	return `{"timestamp":"2026-09-27T11:57:20.745Z","type":"session_meta","payload":{"session_id":"` + sessionID +
		`","id":"` + sessionID + `","cwd":"` + cwd + `","originator":"codex-tui","source":"` + source +
		`","thread_source":"user","base_instructions":"say \"cwd\":\"/not-this\""}}`
}

// writeCodexHome lays out home's ~/.codex: one stopped rollout, one older
// than 7 days, one at the wrong depth, and the live rollout it returns the
// name and path of — with a thread_turns row for it where sqlite3 is
// installed, which it also reports.
func writeCodexHome(t *testing.T, home string) (live, livePath string, withSQLite bool) {
	t.Helper()
	stopped := rolloutName("2026-09-20T08-00-00", codexSessionA)
	writeRollout(t, home, "2026/09/20", stopped, sessionMeta(codexSessionA, "/workspace/user/api", "cli"))
	oldID := "01a0e2bd-ce26-7d81-a280-90c4de0d0046"
	old := writeRollout(t, home, "2026/09/01", rolloutName("2026-09-01T08-00-00", oldID),
		sessionMeta(oldID, "/old", "cli"))
	longAgo := time.Now().Add(-10 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(old, longAgo, longAgo))
	writeRollout(t, home, "2026", rolloutName("2026-09-01T08-00-00", codexSessionA),
		sessionMeta(codexSessionA, "/wrong-depth", "cli"))

	live = rolloutName("2026-09-27T11-57-03", codexSessionB)
	livePath = writeRollout(t, home, "2026/09/27", live,
		sessionMeta(codexSessionB, "/workspace/user/web", "cli"),
		`{"timestamp":"2026-09-27T11:59:50.000Z","ordinal":60,"type":"event_msg",`+
			`"payload":{"type":"task_started","turn_id":"t1"}}`,
		`{"timestamp":"2026-09-27T11:59:52.794Z","ordinal":67,"type":"response_item",`+
			`"payload":{"type":"function_call","id":"fc_1","name":"request_user_input","arguments":"{}"}}`,
		`{"timestamp":"2026-09-27T11:59:52.796Z","ordinal":68,"type":"token_usage_record","payload":{"thread_id":"x"}}`,
	)
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		return live, livePath, false
	}
	db := filepath.Join(home, ".codex", "thread_history_1.sqlite")
	out, err := exec.Command(sqlite, db, //nolint:gosec // this test's own database
		"create table thread_turns (thread_id text, turn_id text, rollout_ordinal integer, status text, "+
			"started_at integer);"+
			"insert into thread_turns values ('"+codexSessionB+"', 'old', 1, 'completed', 100);"+
			"insert into thread_turns values ('"+codexSessionB+"', 't1', 60, 'inProgress', 1790510390);").CombinedOutput()
	require.NoError(t, err, string(out))
	return live, livePath, true
}

// startStandInCodex starts a process named codex that holds path open.
func startStandInCodex(t *testing.T, path string) int {
	t.Helper()
	shPath, err := exec.LookPath("sh")
	require.NoError(t, err)
	fake := filepath.Join(t.TempDir(), "codex")
	require.NoError(t, os.Symlink(shPath, fake))
	// The trailing ":" keeps sh from exec'ing sleep, which would take over
	// the process and its name.
	cmd := exec.Command(fake, "-c", `exec 3>>"$0"; sleep 30; :`, path) //nolint:gosec // test stand-in
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

// The script run for real: stopped rollouts come from ~/.codex/sessions, and
// the rollout a running `codex` holds open is found through its descriptors
// (/proc/<pid>/fd, or lsof), with the last turn event and response item of
// its tail and, where sqlite3 is installed, its newest thread_turns row.
func TestRunLocal_CollectScriptReadsCodexRollouts(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	live, livePath, withSQLite := writeCodexHome(t, home)
	pid := startStandInCodex(t, livePath)

	var raw rawSnapshot
	require.Eventually(t, func() bool {
		out, err := runLocal(context.Background(), collectScript)
		require.NoError(t, err)
		raw, err = parseCollectOutput(out)
		require.NoError(t, err)
		return len(raw.CodexOpen) > 0
	}, 5*time.Second, 50*time.Millisecond, "the rollout the stand-in holds open is found")

	var held codexOpenRollout
	for _, o := range raw.CodexOpen {
		if o.PID == pid {
			held = o
		}
	}
	assert.Equal(t, codexSessionB, held.Rollout.SessionID)
	assert.Equal(t, live, held.File)
	assert.Equal(t, "/workspace/user/web", held.Rollout.CWD, "the session_meta's own cwd, not one quoted inside it")
	assert.GreaterOrEqual(t, held.Elapsed, int64(0))
	assert.Contains(t, raw.CodexAges, pid)
	assert.Equal(t, "task_started", held.Turn.Event)
	assert.Equal(t, "function_call", held.Turn.LastItem)
	assert.Equal(t, "request_user_input", held.Turn.LastItemName, "the token_usage_record after the call is skipped")
	if withSQLite {
		assert.Equal(t, "inProgress", held.Turn.DBStatus, "the newest turn by rollout_ordinal")
		assert.Equal(t, int64(1790510390), held.Turn.DBStartedAt)
	}

	byID := map[string]codexRollout{}
	for _, r := range raw.CodexRollouts {
		byID[r.SessionID] = r
	}
	assert.Len(t, byID, 2, "rollouts older than 7 days or at another depth are not listed: %+v", raw.CodexRollouts)
	stopped := byID[codexSessionA]
	assert.Equal(t, "/workspace/user/api", stopped.CWD)
	assert.Equal(t, "cli", stopped.Source)
	assert.InDelta(t, time.Now().Unix(), stopped.ModTime, 60)
	assert.Positive(t, stopped.Size)
	assert.Equal(t, "cli", byID[codexSessionB].Source)
}
