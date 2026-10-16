package tasks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/agent"
	"panemux/internal/homedir"
)

const (
	codexTurnA = "01a0ec57-1885-7d80-82a2-420432147b62"
	codexTurnB = "01a0ec5e-bf05-7552-99da-92f79c51f8aa"
)

// permissionHookInput is the JSON codex gives a PermissionRequest hook on
// stdin. The command it asks about quotes keys of its own, which the hook
// must not take for the session's.
func permissionHookInput(sessionID, turnID string) string {
	return `{"session_id":"` + sessionID + `","turn_id":"` + turnID + `",` +
		`"transcript_path":"/tmp/sample-project/r.jsonl",` +
		`"cwd":"/tmp/sample-project","hook_event_name":"PermissionRequest","model":"gpt-5","permission_mode":"default",` +
		`"tool_name":"Bash","tool_input":{"command":"echo '{\"session_id\":\"` + codexSessionB + `\"}' secret-word"}}`
}

// runPermissionHook runs the hook as codex would, with home as HOME.
func runPermissionHook(t *testing.T, home, stdin string) string {
	t.Helper()
	cmd := exec.Command("sh", "codex_permission_hook.sh")
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	require.NoError(t, err, "the hook always exits 0")
	return string(out)
}

func permissionRecordPath(home, sessionID string) string {
	return filepath.Join(home, ".local", "state", "panemux", "codex-permission", sessionID)
}

// The hook keeps the session, the turn and the time, and nothing of the
// command; it answers nothing, so codex decides as it would without it.
func TestCodexPermissionHook_RecordsTheSessionTurnAndTimeOnly(t *testing.T) {
	home := t.TempDir()
	before := time.Now().Unix()
	out := runPermissionHook(t, home, permissionHookInput(codexSessionA, codexTurnA))
	assert.Empty(t, out, "no allow or deny is returned")

	data, err := os.ReadFile(permissionRecordPath(home, codexSessionA))
	require.NoError(t, err)
	fields := strings.Fields(string(data))
	require.Len(t, fields, 3, "%q", data)
	assert.Equal(t, codexSessionA, fields[0])
	assert.Equal(t, codexTurnA, fields[1])
	at, err := strconv.ParseInt(fields[2], 10, 64)
	require.NoError(t, err)
	assert.InDelta(t, before, at, 5)
	assert.NotContains(t, string(data), "secret-word")
	_, err = os.Stat(permissionRecordPath(home, codexSessionB))
	assert.True(t, os.IsNotExist(err), "a session ID quoted inside the command is not recorded")

	runPermissionHook(t, home, permissionHookInput(codexSessionA, codexTurnB))
	data, err = os.ReadFile(permissionRecordPath(home, codexSessionA))
	require.NoError(t, err)
	assert.Equal(t, codexTurnB, strings.Fields(string(data))[1], "a newer request replaces the session's record")
}

func TestCodexPermissionHook_IgnoresInputWithoutASessionOrTurn(t *testing.T) {
	for name, stdin := range map[string]string{
		"empty":          "",
		"no turn":        `{"session_id":"` + codexSessionA + `"}`,
		"no session":     `{"turn_id":"` + codexTurnA + `"}`,
		"not hex digits": `{"session_id":"../../x","turn_id":"` + codexTurnA + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			assert.Empty(t, runPermissionHook(t, home, stdin))
			entries, _ := os.ReadDir(filepath.Join(home, ".local", "state", "panemux", "codex-permission"))
			assert.Empty(t, entries)
		})
	}
}

// What the hook wrote is what the live collection reads. The hook removes
// records older than 7 days when it writes, so the collection reads them all
// with one process.
func TestRunLocal_CollectScriptReadsCodexPermissionRecords(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	runPermissionHook(t, home, permissionHookInput(codexSessionB, codexTurnB))
	old := time.Now().Add(-8 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(permissionRecordPath(home, codexSessionB), old, old))
	runPermissionHook(t, home, permissionHookInput(codexSessionA, codexTurnA))
	_, err := os.Stat(permissionRecordPath(home, codexSessionB))
	assert.True(t, os.IsNotExist(err), "the 8-day-old record is removed")

	out, err := runLocal(context.Background(), attentionScript)
	require.NoError(t, err)
	raw, err := parseCollectOutput(out, collectPlan.live)
	require.NoError(t, err)

	permissions := codexOf(raw).Permissions
	require.Len(t, permissions, 1, "%+v", permissions)
	record := permissions[codexSessionA]
	assert.Equal(t, codexTurnA, record.TurnID)
	assert.InDelta(t, time.Now().UnixMilli(), record.At, 10_000)
}

func TestParseCollectOutput_ReadsCodexPermissionRows(t *testing.T) {
	raw, err := parseCollectOutput(fullOutput(
		"::panemux-tasks v2",
		"::now 1000",
		"::section codex/permission",
		codexSessionA+" "+codexTurnA+" 1790510400",
		codexSessionB+" "+codexTurnB,
		codexSessionB+" not-a-turn 1790510400",
		"not-a-session "+codexTurnB+" 1790510400",
		codexSessionB+" "+codexTurnB+" soon",
		codexSessionB+" "+codexTurnB+" 0",
		"",
		"::end",
	), collectPlan.full)
	require.NoError(t, err)
	assert.Equal(t, map[string]codexPermission{
		codexSessionA: {TurnID: codexTurnA, At: 1790510400000},
	}, codexOf(raw).Permissions)
}

// The turn a task_started event starts is the turn a record names.
func TestParseCodexTurn_ReadsTheStartedTurnsID(t *testing.T) {
	started := `{"timestamp":"2026-09-27T11:58:30.057Z","type":"event_msg","payload":{"type":"task_started","turn_id":"` +
		codexTurnA + `"}}`
	assert.Equal(t, codexTurnA, parseCodexTurn("", started, "").TurnID)
	complete := strings.Replace(started, "task_started", "task_complete", 1)
	assert.Empty(t, parseCodexTurn("", complete, "").TurnID, "a finished turn is no turn to wait in")
}

func permissionRaw(turn codexTurn, records map[string]codexPermission) rawSnapshot {
	raw := codexRaw(openRollout(31, codexSessionA, 30, turn))
	codexOf(raw).Permissions = records
	return raw
}

// A turn in progress with a record for that session and turn waits on the
// approval, from the record's time; any other turn ignores the record.
func TestBuildTasks_CodexWaitsOnAPermissionRequestOfItsTurn(t *testing.T) {
	inTurnA := codexTurn{
		DBStatus: "inProgress", DBStartedAt: hostNow - 120,
		Event: "task_started", EventAt: (hostNow - 120) * 1000, TurnID: codexTurnA,
	}
	record := map[string]codexPermission{codexSessionA: {TurnID: codexTurnA, At: (hostNow - 40) * 1000}}

	task := buildTasks("", permissionRaw(inTurnA, record), collectedAt)[0]
	assert.Equal(t, StateWait, task.State)
	assert.Equal(t, codexWaitingForPermission, task.WaitingFor)
	assert.Equal(t, hostAgo(40), task.StatusSince)
	assert.NotEmpty(t, task.WaitSignature)
	again := buildTasks("", permissionRaw(inTurnA, record), collectedAt)[0]
	assert.Equal(t, task.WaitSignature, again.WaitSignature, "the same record is the same wait")
	later := map[string]codexPermission{codexSessionA: {TurnID: codexTurnA, At: (hostNow - 5) * 1000}}
	assert.NotEqual(t, task.WaitSignature, buildTasks("", permissionRaw(inTurnA, later), collectedAt)[0].WaitSignature,
		"a later request in the same turn is a new wait")

	// A turn whose task_started is out of the rollout's tail has no ID: a
	// record made since the turn started is taken as the turn's.
	noID := codexTurn{DBStatus: "inProgress", DBStartedAt: hostNow - 120}
	assert.Equal(t, StateWait, buildTasks("", permissionRaw(noID, record), collectedAt)[0].State)
	noID.DBStartedAt = hostNow - 40
	assert.Equal(t, StateWait, buildTasks("", permissionRaw(noID, record), collectedAt)[0].State,
		"a record made in the second the turn started")

	inTurnB := inTurnA
	inTurnB.TurnID = codexTurnB
	finished := codexTurn{DBStatus: "completed", Event: "task_complete", EventAt: (hostNow - 10) * 1000}
	leftover := codexTurn{DBStatus: "inProgress", DBStartedAt: hostNow - codexProcElapsed - 60, TurnID: codexTurnA}
	for name, tt := range map[string]struct {
		records map[string]codexPermission
		want    State
		turn    codexTurn
	}{
		"another turn":      {turn: inTurnB, records: record, want: StateBusy},
		"the turn finished": {turn: finished, records: record, want: StateIdle},
		"a leftover turn":   {turn: leftover, records: record, want: StateIdle},
		"no record":         {turn: inTurnA, want: StateBusy},
		"another session": {
			turn: inTurnA, records: map[string]codexPermission{codexSessionB: record[codexSessionA]}, want: StateBusy,
		},
		"a turn with neither ID nor start": {
			turn: codexTurn{DBStatus: "inProgress"}, records: record, want: StateBusy,
		},
		"a turn with no ID, and a record from before it started": {
			turn:    codexTurn{DBStatus: "inProgress", DBStartedAt: hostNow - 30},
			records: record, want: StateBusy,
		},
	} {
		t.Run(name, func(t *testing.T) {
			task := buildTasks("", permissionRaw(tt.turn, tt.records), collectedAt)[0]
			assert.Equal(t, tt.want, task.State)
			assert.Empty(t, task.WaitingFor)
		})
	}
}

// permissionOutput is a host with codex session A in turn A, and a record
// for turn A made at the given time.
func permissionOutput(at int64) []byte {
	return fullOutput(
		"::panemux-tasks v2",
		"::now "+strconv.FormatInt(hostNow, 10),
		"::section ps",
		"31 1 codex",
		"::section codex/open",
		"31\t10:00\t"+strconv.FormatInt(hostNow-30, 10)+" 100\tinProgress "+strconv.FormatInt(hostNow-120, 10)+"\t"+
			`{"timestamp":"2026-09-27T11:58:30.057Z","type":"event_msg",`+
			`"payload":{"type":"task_started","turn_id":"`+codexTurnA+`"}}`+
			"\t\t\t\"originator\":\"codex-tui\"\t/remote/home/demo/.codex/sessions/2026/09/27/"+
			rolloutName("2026-09-27T11-57-03", codexSessionA),
		"::section codex/permission",
		codexSessionA+" "+codexTurnA+" "+strconv.FormatInt(at, 10),
		"::end",
	)
}

// "Input given" clears the wait it names, on every collection, until the
// turn asks again.
func TestService_DismissWaitClearsThatPermissionRequestOnly(t *testing.T) {
	at := hostNow - 40
	svc := New(Options{
		RunLocal: func(context.Context, string) ([]byte, error) { return permissionOutput(at), nil },
		Now:      (&clock{now: collectedAt}).Now,
	})
	defer svc.Close()

	waiting := findTask(t, svc.CollectAttention(context.Background()).Tasks, "local:codex:"+codexSessionA)
	require.Equal(t, StateWait, waiting.State)
	require.NoError(t, svc.DismissWait(waiting.WaitSignature))

	for _, snapshot := range []Snapshot{svc.CollectAttention(context.Background()), collectAndProbe(svc)} {
		task := findTask(t, snapshot.Tasks, "local:codex:"+codexSessionA)
		assert.Equal(t, StateBusy, task.State)
		assert.Empty(t, task.WaitSignature)
	}

	at = hostNow - 5
	again := findTask(t, svc.CollectAttention(context.Background()).Tasks, "local:codex:"+codexSessionA)
	assert.Equal(t, StateWait, again.State, "the next request in the same turn waits again")
	assert.NotEqual(t, waiting.WaitSignature, again.WaitSignature)
}

func TestService_DismissWaitRefusesAnEmptySignature(t *testing.T) {
	svc := New(Options{})
	defer svc.Close()
	assert.ErrorIs(t, svc.DismissWait(""), ErrInvalidWait)
}

// A collection without codex's part — codex not collected — has no approval
// requests to drop, and dropping them does not fail.
func TestService_DropDismissedPermissionsWithoutACodexPart(t *testing.T) {
	svc := New(Options{})
	defer svc.Close()
	require.NoError(t, svc.DismissWait(codexPermissionSignature("h", codexSessionA, 1000)))
	raw := rawSnapshot{Agents: map[agent.ID]agentCollected{agent.Claude: &claudeCollected{}}}
	assert.NotPanics(t, func() { svc.dropDismissedPermissions("h", &raw) })
	assert.Len(t, raw.Agents, 1)
}
