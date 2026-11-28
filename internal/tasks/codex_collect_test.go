package tasks

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panemux/internal/homedir"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/testcap"
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
	out := fullOutput(
		"::panemux-tasks v2",
		"::now 1790510500",
		"::section codex/open",
		"899\t   02:05",
		"901\t??",
		"899\t   02:05\t1790510485 48396\tinProgress 1790510480\t"+
			`{"timestamp":"2026-09-27T11:58:30.057Z","ordinal":40,"type":"event_msg",`+
			`"payload":{"type":"task_started","turn_id":"x"}}`+"\t"+
			`{"timestamp":"2026-09-27T11:59:52.794Z","ordinal":67,"type":"response_item",`+
			`"payload":{"type":"function_call","id":"fc_1","name":"request_user_input","arguments":"{}"`+"\t"+
			`"cwd":"/tmp/sample-project"`+"\t"+`"originator":"codex-tui"`+"\t"+openPath,
		"900\t1-02:03:04\t1790510000 10\t\t\t\t\t\t/remote/home/demo/.codex/sessions/2026/09/27/"+
			rolloutName("2026-09-27T12-00-15", codexSessionB),
		"::section codex/rollouts",
		"1790510485\t48396\t"+rolloutName("2026-09-27T11-57-03", codexSessionA)+"\t"+
			`"cwd":"/tmp/sample-project"`+"\t"+`"originator":"codex-tui"`,
		"1790500000\t17\t"+rolloutName("2026-09-27T09-00-00", codexSessionB)+"\t\t",
		"::end",
	)

	raw, err := parseCollectOutput(out, collectPlan.full)
	require.NoError(t, err)

	assert.Equal(t, map[int]int64{899: 125}, codexOf(raw).Ages, "a process whose etime ps could not give has no age")
	assert.Equal(t, []codexOpenRollout{
		{
			PID: 899, Elapsed: 125, File: rolloutName("2026-09-27T11-57-03", codexSessionA),
			Rollout: codexRollout{
				File:      rolloutName("2026-09-27T11-57-03", codexSessionA),
				SessionID: codexSessionA, CWD: "/tmp/sample-project", Originator: "codex-tui", ModTime: 1790510485, Size: 48396,
			},
			Turn: codexTurn{
				DBStatus: "inProgress", DBStartedAt: 1790510480,
				Event: "task_started", EventAt: 1790510310057,
				LastItem: "function_call", LastItemName: "request_user_input", LastItemAt: 1790510392794,
			},
		},
		{
			PID: 900, Elapsed: 93784, File: rolloutName("2026-09-27T12-00-15", codexSessionB),
			Rollout: codexRollout{
				File:      rolloutName("2026-09-27T12-00-15", codexSessionB),
				SessionID: codexSessionB, ModTime: 1790510000, Size: 10,
			},
		},
	}, codexOf(raw).Open)
	assert.Equal(t, []codexRollout{
		{File: rolloutName("2026-09-27T11-57-03", codexSessionA),
			SessionID: codexSessionA, CWD: "/tmp/sample-project", Originator: "codex-tui", ModTime: 1790510485, Size: 48396},
		{File: rolloutName("2026-09-27T09-00-00", codexSessionB), SessionID: codexSessionB, ModTime: 1790500000, Size: 17},
	}, codexOf(raw).Rollouts)
}

func TestParseCollectOutput_SkipsMalformedCodexRows(t *testing.T) {
	good := rolloutName("2026-09-27T11-57-03", codexSessionA)
	out := fullOutput(
		"::panemux-tasks v2",
		"::now 5",
		"::section codex/open",
		"x\t00:10",
		"0\t00:10",
		"899\t00:01\t1\t\t\t\t\t\t/x/"+good,
		"899\t00:01\t1 x\t\t\t\t\t\t/x/"+good,
		"not-a-pid\t00:01\t1 1\t\t\t\t\t\t/x/"+good,
		"0\t00:01\t1 1\t\t\t\t\t\t/x/"+good,
		"899\t00:01\t1 1\t\t\t\t", // too few fields
		"899\t00:01\tnope\t\t\t\t\t\t/x/"+good,
		"899\t00:01\t1 1\t\t\t\t\t\t/x/rollout-2026-09-27T11-57-03-not-a-uuid.jsonl",
		"899\t00:01\t1 1\t\t\t\t\t\t/x/other.jsonl",
		"::section codex/rollouts",
		"x\t1\t"+good+"\t\t",
		"1\tx\t"+good+"\t\t",
		"1\t1\trollout-2026-09-27T11-57-03-"+codexSessionA+"-extra.jsonl\t\t",
		"1\t1",
		"::end",
	)
	raw, err := parseCollectOutput(out, collectPlan.full)
	require.NoError(t, err)
	assert.Empty(t, codexOf(raw).Open)
	assert.Empty(t, codexOf(raw).Rollouts)
	assert.Empty(t, codexOf(raw).Ages)
}

func TestJSONFragmentString(t *testing.T) {
	assert.Equal(t, "codex-tui", jsonFragmentString(`"originator":"codex-tui"`, `"originator":`))
	assert.Empty(t, jsonFragmentString(`"cwd":"codex-tui"`, `"originator":`), "another key")
	assert.Empty(t, jsonFragmentString(`"originator":"a\q"`, `"originator":`), "not a string codex could have written")
}

// A codex-rollouts row may end after the file name (no session_meta line
// read) or after the cwd (no source in it).
func TestParseCollectOutput_ShortCodexRolloutRows(t *testing.T) {
	name := rolloutName("2026-09-27T11-57-03", codexSessionA)
	raw, err := parseCollectOutput(fullOutput(
		"::panemux-tasks v2",
		"::now 5",
		"::section codex/rollouts",
		"1\t2\t"+name,
		"3\t4\t"+name+"\t"+`"cwd":"/workspace/user/api"`,
		"::end",
	), collectPlan.full)
	require.NoError(t, err)
	assert.Equal(t, []codexRollout{
		{File: name, SessionID: codexSessionA, ModTime: 1, Size: 2},
		{File: name, SessionID: codexSessionA, CWD: "/workspace/user/api", ModTime: 3, Size: 4},
	}, codexOf(raw).Rollouts)
}

// A row whose etime ps could not give still names the rollout; the process's
// age is then unknown (-1), and whatever else is missing reads as empty.
func TestParseCollectOutput_CodexOpenRowWithOnlyTheRollout(t *testing.T) {
	name := rolloutName("2026-09-27T11-57-03", codexSessionA)
	raw, err := parseCollectOutput(fullOutput(
		"::panemux-tasks v2",
		"::now 5",
		"::section codex/open",
		"899\t\t1790510485 12\tgarbage\tnot json\t{\"type\":\"response_item\"}\t\t\t/x/"+name,
		"::end",
	), collectPlan.full)
	require.NoError(t, err)
	require.Len(t, codexOf(raw).Open, 1)
	got := codexOf(raw).Open[0]
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
		{name: "db row with a start of zero", db: "inProgress 0", want: codexTurn{DBStatus: "inProgress"}},
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
			name: "an item carries its own time",
			item: `{"timestamp":"2026-09-27T11:59:52.794Z","type":"response_item",` +
				`"payload":{"type":"function_call","name":"request_user_input","arguments":"{}"}}`,
			want: codexTurn{LastItem: "function_call", LastItemName: "request_user_input", LastItemAt: 1790510392794},
		},
		{
			name: "an item time that is not the line's own is not read",
			item: `{"type":"response_item","payload":{"type":"function_call","name":"request_user_input",` +
				`"timestamp":"2026-09-27T11:59:52.794Z"}}`,
			want: codexTurn{LastItem: "function_call", LastItemName: "request_user_input"},
		},
		{
			name: "an item with an unreadable time",
			item: `{"timestamp":"soon","type":"response_item","payload":{"type":"function_call","name":"request_user_input"}}`,
			want: codexTurn{LastItem: "function_call", LastItemName: "request_user_input"},
		},
		{
			name: "an item time before the epoch",
			item: `{"timestamp":"1960-01-01T00:00:00Z","type":"response_item","payload":{"type":"message"}}`,
			want: codexTurn{LastItem: "message"},
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

func sessionMeta(sessionID, cwd, originator string) string {
	return `{"timestamp":"2026-09-27T11:57:20.745Z","type":"session_meta","payload":{"session_id":"` + sessionID +
		`","id":"` + sessionID + `","cwd":"` + cwd + `","originator":"` + originator + `","source":"cli` +
		`","thread_source":"user","base_instructions":"say \"cwd\":\"/not-this\""}}`
}

// writeCodexHome lays out home's ~/.codex: one stopped rollout, one older
// than 7 days, one at the wrong depth, and the live rollout it returns the
// name and path of — with a thread_turns row for it where sqlite3 is
// installed, which it also reports.
func writeCodexHome(t *testing.T, home string) (live, livePath string, withSQLite bool) {
	t.Helper()
	stopped := rolloutName("2026-09-20T08-00-00", codexSessionA)
	writeRollout(t, home, "2026/09/20", stopped, sessionMeta(codexSessionA, "/workspace/user/api", "codex-tui"))
	oldID := "01a0e2bd-ce26-7d81-a280-90c4de0d0046"
	old := writeRollout(t, home, "2026/09/01", rolloutName("2026-09-01T08-00-00", oldID),
		sessionMeta(oldID, "/old", "codex-tui"))
	longAgo := time.Now().Add(-10 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(old, longAgo, longAgo))
	writeRollout(t, home, "2026", rolloutName("2026-09-01T08-00-00", codexSessionA),
		sessionMeta(codexSessionA, "/wrong-depth", "codex-tui"))

	live = rolloutName("2026-09-27T11-57-03", codexSessionB)
	livePath = writeRollout(t, home, "2026/09/27", live,
		sessionMeta(codexSessionB, "/workspace/user/web", "codex-tui"),
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

// startStandInCodex starts a process named codex that holds path open, and
// waits until the descriptor is actually open before returning its PID.
func startStandInCodex(t *testing.T, path string) int {
	t.Helper()
	shPath, err := exec.LookPath("sh")
	require.NoError(t, err)
	fake := filepath.Join(t.TempDir(), "codex")
	require.NoError(t, os.Symlink(shPath, fake))
	readyR, readyW, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = readyR.Close() }()
	// ExtraFiles[0] is inherited as fd 3; open the rollout on fd 4, signal
	// readiness on fd 3, then close fd 3 before sleeping. The trailing ":"
	// keeps sh from exec'ing sleep, which would take over the process name.
	standIn := `exec 4>>"$0"; printf ok >&3; exec 3>&-; sleep 30; :`
	cmd := exec.Command(fake, "-c", standIn, path) //nolint:gosec // test stand-in
	cmd.ExtraFiles = []*os.File{readyW}
	require.NoError(t, cmd.Start())
	_ = readyW.Close()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	type readyResult struct {
		err  error
		data []byte
	}
	ready := make(chan readyResult, 1)
	go func() {
		data, readErr := io.ReadAll(readyR)
		ready <- readyResult{data: data, err: readErr}
	}()
	select {
	case res := <-ready:
		require.NoError(t, res.err)
		require.Equal(t, "ok", string(res.data), "stand-in codex failed before opening rollout")
	case <-time.After(5 * time.Second):
		t.Fatal("stand-in codex did not open rollout in time")
	}
	return cmd.Process.Pid
}

// The script run for real: stopped rollouts come from ~/.codex/sessions, and
// the rollout a running `codex` holds open is found through its descriptors
// (/proc/<pid>/fd, or lsof), with the last turn event and response item of
// its tail and, where sqlite3 is installed, its newest thread_turns row.
//
//efficacy:exempt isolates ps probe to stand-in PID (issue #408); open-rollout reading predates branch
func TestRunLocal_CollectScriptReadsCodexRollouts(t *testing.T) {
	testcap.RequirePS(t)
	home := t.TempDir()
	homedir.SetForTest(t, home)
	live, livePath, withSQLite := writeCodexHome(t, home)
	pid := startStandInCodex(t, livePath)
	isolateHostProcessesToPIDs(t, pid)

	var raw rawSnapshot
	require.Eventually(t, func() bool {
		out, err := runLocal(context.Background(), collectScript)
		require.NoError(t, err)
		raw, err = parseCollectOutput(out, collectPlan.full)
		require.NoError(t, err)
		return len(codexOf(raw).Open) > 0
	}, 5*time.Second, 50*time.Millisecond, "the rollout the stand-in holds open is found")

	var held codexOpenRollout
	for _, o := range codexOf(raw).Open {
		if o.PID == pid {
			held = o
		}
	}
	assert.Equal(t, codexSessionB, held.Rollout.SessionID)
	assert.Equal(t, live, held.File)
	assert.Equal(t, "/workspace/user/web", held.Rollout.CWD, "the session_meta's own cwd, not one quoted inside it")
	assert.GreaterOrEqual(t, held.Elapsed, int64(0))
	assert.Contains(t, codexOf(raw).Ages, pid)
	assert.Equal(t, "task_started", held.Turn.Event)
	assert.Equal(t, "function_call", held.Turn.LastItem)
	assert.Equal(t, "request_user_input", held.Turn.LastItemName, "the token_usage_record after the call is skipped")
	if withSQLite {
		assert.Equal(t, "inProgress", held.Turn.DBStatus, "the newest turn by rollout_ordinal")
		assert.Equal(t, int64(1790510390), held.Turn.DBStartedAt)
	}

	byID := map[string]codexRollout{}
	for _, r := range codexOf(raw).Rollouts {
		byID[r.SessionID] = r
	}
	assert.Len(t, byID, 2, "rollouts older than 7 days or at another depth are not listed: %+v", codexOf(raw).Rollouts)
	stopped := byID[codexSessionA]
	assert.Equal(t, "/workspace/user/api", stopped.CWD)
	assert.Equal(t, "codex-tui", stopped.Originator)
	assert.InDelta(t, time.Now().Unix(), stopped.ModTime, 60)
	assert.Positive(t, stopped.Size)
	assert.Equal(t, "codex-tui", byID[codexSessionB].Originator)
}

// The 100 rollouts the script lists are the newest of the TUI's: newer
// ones codex exec wrote, however many, do not push them out.
//
//efficacy:exempt isolates the test from unrelated host processes (issue #408); rollout listing predates this branch
func TestRunLocal_CollectScriptListsTheTUIsRolloutsPastNewerOnes(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	isolateEmptyProcesses(t)
	tui := writeRollout(t, home, "2026/09/28", rolloutName("2026-09-28T01-00-00", codexSessionA),
		sessionMeta(codexSessionA, "/workspace/user/api", codexOriginatorTUI))
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(tui, twoHoursAgo, twoHoursAgo))
	for i := 0; i < 101; i++ {
		id := fmt.Sprintf("01a0e2b9-d054-7cc2-9278-%012d", i)
		writeRollout(t, home, "2026/09/28", rolloutName("2026-09-28T02-00-00", id), sessionMeta(id, "/w", "codex_exec"))
	}

	out, err := runLocal(context.Background(), collectScript)
	require.NoError(t, err)
	raw, err := parseCollectOutput(out, collectPlan.full)
	require.NoError(t, err)

	require.Len(t, codexOf(raw).Rollouts, 1, "only the TUI's rollouts are listed")
	assert.Equal(t, codexSessionA, codexOf(raw).Rollouts[0].SessionID)
}

func installFakeMacOSCodexOpenTools(t *testing.T, openLog, rolloutA, rolloutA2, rolloutB string) {
	t.Helper()
	bin := t.TempDir()
	fakePS := `#!/bin/sh
case "$*" in
"-U "*" -o pid=,etime=,command=")
	printf '201 01:05 /usr/local/bin/codex\n'
	printf '202 02:10 /usr/local/bin/codex\n'
	printf '203 00:30 /usr/local/bin/codex\n'
	printf '204 00:10 /bin/zsh\n'
	;;
"-U "*" -o pid=,ppid=,command=")
	printf '201 1 /usr/local/bin/codex\n'
	printf '202 1 /usr/local/bin/codex\n'
	printf '203 1 /usr/local/bin/codex\n'
	;;
"-U "*" -o pid=,command=")
	printf '201 /usr/local/bin/codex\n'
	printf '202 /usr/local/bin/codex\n'
	printf '203 /usr/local/bin/codex\n'
	;;
esac
`
	fakeLsof := `#!/bin/sh
for arg in "$@"; do
	if [ "$arg" = "cwd" ]; then
		exit 0
	fi
done
printf '%s\n' "$*" >> '` + openLog + `'
printf 'p201\nf0\nn/dev/ttys001\nf3\nn%s\nf4\nn%s\n' '` + rolloutA + `' '` + rolloutA2 + `'
printf 'p202\nf3\nn%s\n' '` + rolloutB + `'
printf 'p203\nf0\nn/dev/ttys002\n'
`
	writeExecutable(t, filepath.Join(bin, "ps"), fakePS)
	writeExecutable(t, filepath.Join(bin, "lsof"), fakeLsof)
	writeExecutable(t, filepath.Join(bin, "uname"), "#!/bin/sh\necho Darwin\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// On macOS, codex/open probes every live codex process in one lsof call with
// -n -P -Fpn rather than spawning lsof per PID (issue #408), preserving spaces
// in rollout paths and multiple rollouts held by one PID.
func TestRunLocal_CollectScriptBatchesLsofForCodexOpenOnMacOS(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home with spaces")
	homedir.SetForTest(t, home)

	codexSessionC := "01a0e2bd-ce26-7d81-a280-90c4de0d0099"
	rolloutA := writeRollout(t, home, "2026/09/27", rolloutName("2026-09-27T11-57-03", codexSessionA),
		sessionMeta(codexSessionA, "/workspace/user/api", codexOriginatorTUI),
		`{"timestamp":"2026-09-27T11:59:50.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`,
	)
	rolloutA2 := writeRollout(t, home, "2026/09/27", rolloutName("2026-09-27T12-05-00", codexSessionC),
		sessionMeta(codexSessionC, "/workspace/user/api", codexOriginatorTUI),
		`{"timestamp":"2026-09-27T12:06:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t3"}}`,
	)
	rolloutB := writeRollout(t, home, "2026/09/27", rolloutName("2026-09-27T12-00-00", codexSessionB),
		sessionMeta(codexSessionB, "/workspace/user/web", codexOriginatorTUI),
		`{"timestamp":"2026-09-27T12:01:00.000Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"t2"}}`,
	)

	openLog := filepath.Join(t.TempDir(), "lsof-open.log")
	installFakeMacOSCodexOpenTools(t, openLog, rolloutA, rolloutA2, rolloutB)

	out, err := runLocal(context.Background(), attentionScript)
	require.NoError(t, err)
	raw, err := parseCollectOutput(out, collectPlan.live)
	require.NoError(t, err)

	callsRaw, err := os.ReadFile(openLog)
	require.NoError(t, err)
	calls := strings.Split(strings.TrimSpace(string(callsRaw)), "\n")
	require.Len(t, calls, 1, "codex/open invokes lsof once for all codex PIDs: %v", calls)
	assert.Equal(t, "-n -P -p 201,202,203 -Fpn", calls[0])

	assert.Equal(t, map[int]int64{201: 65, 202: 130, 203: 30}, codexOf(raw).Ages)
	bySession := map[string]codexOpenRollout{}
	for _, o := range codexOf(raw).Open {
		bySession[o.Rollout.SessionID] = o
	}
	require.Len(t, bySession, 3)
	assert.Equal(t, 201, bySession[codexSessionA].PID)
	assert.Equal(t, int64(65), bySession[codexSessionA].Elapsed)
	assert.Equal(t, "/workspace/user/api", bySession[codexSessionA].Rollout.CWD)
	assert.Equal(t, "task_started", bySession[codexSessionA].Turn.Event)

	assert.Equal(t, 201, bySession[codexSessionC].PID)
	assert.Equal(t, int64(65), bySession[codexSessionC].Elapsed)

	assert.Equal(t, 202, bySession[codexSessionB].PID)
	assert.Equal(t, int64(130), bySession[codexSessionB].Elapsed)
	assert.Equal(t, "/workspace/user/web", bySession[codexSessionB].Rollout.CWD)
	assert.Equal(t, "task_complete", bySession[codexSessionB].Turn.Event)
}
