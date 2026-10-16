package tasks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/agent"
	"panemux/internal/homedir"
	"panemux/internal/testcap"
)

func formatsOf(t *testing.T, raw rawSnapshot) map[agent.ID]formatReport {
	t.Helper()
	_, _, formats := buildTasksWithDiagnostics("", raw, collectedAt)
	return formats
}

// A Claude state file is in an unknown format when it cannot be read (no
// JSON object, no pid, no session ID) or its status is one this build does
// not know. A missing status is an older Claude Code, not a new format; a
// leftover file of a process that is gone is not counted.
func TestFormats_ClaudeStateFiles(t *testing.T) {
	raw := fixtureRaw{
		Now: hostNow,
		StateFiles: []stateFile{
			{Name: "5.json", Data: []byte(
				`{"pid":5,"sessionId":"s5","status":"busy","statusUpdatedAt":5000,"version":"2.1.0"}`)},
			{Name: "6.json", Data: []byte(
				`{"pid":6,"sessionId":"s6","status":"pondering","statusUpdatedAt":9000,"version":"2.2.0"}`)},
			{Name: "7.json", Data: []byte(`{"pid":7,"sessionId":"s7"}`)},
			{Name: "8.json", Data: []byte(`{`)},
			{Name: "9.json", Data: []byte(`{"pid":9,"sessionId":"s9","status":"gone-status"}`)},
			{Name: "10.json", Data: []byte(`{`)},
		},
		Processes: []process{claudeProc(5, 1), claudeProc(6, 1), claudeProc(7, 1), claudeProc(8, 1)},
	}.snapshot()

	tasks, _, formats := buildTasksWithDiagnostics("", raw, collectedAt)
	got := formats[agent.Claude]
	assert.Equal(t, map[string]int{"unknown_status": 1, "not_json": 1}, got.counts)
	assert.Equal(t, `not_json 1, unknown_status 1; `+
		`e.g. status "pondering" | 8.json: unexpected end of JSON input; versions 2.2.0, 2.1.0`, got.detail())
	assert.Equal(t, "2.2.0", got.newestVersion(), "the version of the session that changed status last")
	assert.Empty(t, formats[agent.Codex].counts)

	for _, task := range tasks {
		if task.SessionID == "s6" {
			assert.Equal(t, StateUnknown, task.State)
			assert.Empty(t, task.WaitSignature, "nothing is notified from an unknown status")
		}
	}
}

// Codex's rows are counted when they cannot be read: a codex/open row that
// is not ps's "<pid>\t<etime>" and is not a rollout codex could have
// written, and a codex/rollouts row likewise. ps's own rows and blank
// lines are not codex's format.
func TestFormats_CodexMalformedRows(t *testing.T) {
	good := rolloutName("2026-09-27T11-57-03", codexSessionA)
	raw, err := parseCollectOutput(fullOutput(
		"::panemux-tasks v2",
		"::now 5",
		"::section codex/open",
		"901\t??",
		"x\t00:10",
		"",
		"899\t00:01\t1 1\t\t\t\t\t\t/x/rollout-2026-09-27T11-57-03-not-a-uuid.jsonl",
		"899\t00:01\t1 1\t\t\t\t\t\t/x/"+good,
		"::section codex/rollouts",
		"1\t1\t"+good+"\t\t",
		"1\t1",
		"::end",
	), collectPlan.full)
	require.NoError(t, err)

	got := formatsOf(t, raw)[agent.Codex]
	assert.Equal(t, map[string]int{"malformed_row": 2}, got.counts)
	assert.Equal(t, "malformed_row 2; e.g. "+
		"codex/open: 899\t00:01\t1 1\t\t\t\t\t\t/x/rollout-2026-09-27T11-57-03-not-a-uuid.jsonl | codex/rollouts: 1\t1",
		strings.ReplaceAll(got.detail(), "�", "\t"), "a tab is shown as U+FFFD, as every control character")
}

// The thread_turns statuses and the turn events of rollouts this build does
// not know are counted, once per open rollout; the version is the newest
// rollout's cli_version.
func TestFormats_CodexUnknownStatusesAndEvents(t *testing.T) {
	raw := codexRaw(
		openRollout(31, codexSessionA, 30, codexTurn{DBStatus: "paused", UnknownEvent: "turn_started"}),
	)
	codex := raw.Agents[agent.Codex].(*codexCollected)
	codex.Rollouts = []codexRollout{
		{SessionID: codexSessionB, ModTime: 10, Version: "0.150.0"},
		{SessionID: codexSessionA, ModTime: 20, Version: "0.160.1"},
	}

	tasks, _, formats := buildTasksWithDiagnostics("", raw, collectedAt)
	got := formats[agent.Codex]
	assert.Equal(t, map[string]int{"unknown_status": 1, "unknown_event": 1}, got.counts)
	assert.Equal(t, `unknown_event 1, unknown_status 1; `+
		`e.g. status "paused" | event "turn_started"; versions 0.160.1, 0.150.0`, got.detail())
	assert.Equal(t, "0.160.1", got.newestVersion())
	require.NotEmpty(t, tasks)
	assert.Equal(t, StateUnknown, tasks[0].State)
}

func TestParseCodexTurn_AnUnknownTurnEventIsKeptApart(t *testing.T) {
	turn := parseCodexTurn("",
		`{"timestamp":"2026-09-27T11:58:30.057Z","type":"event_msg","payload":{"type":"turn_started","turn_id":"x"}}`, "")
	assert.Empty(t, turn.Event)
	assert.Equal(t, "turn_started", turn.UnknownEvent)
	assert.Zero(t, turn.EventAt, "no time is read from an event this build does not know")

	diff := parseCodexTurn("", `{"type":"event_msg","payload":{"type":"turn_diff","unified_diff":""}}`, "")
	assert.Equal(t, codexTurn{}, diff, "turn_diff is not a turn's start or end")
}

// A codex/rollouts row carries the session_meta's cli_version last.
func TestParseCollectOutput_CodexRolloutRowWithAVersion(t *testing.T) {
	name := rolloutName("2026-09-27T11-57-03", codexSessionA)
	raw, err := parseCollectOutput(fullOutput(
		"::panemux-tasks v2",
		"::now 5",
		"::section codex/rollouts",
		"7\t9\t"+name+"\t\"cwd\":\"/w\"\t\"originator\":\"codex-tui\"\t\"cli_version\":\"0.160.1\"",
		"::end",
	), collectPlan.full)
	require.NoError(t, err)
	require.Len(t, codexOf(raw).Rollouts, 1)
	assert.Equal(t, "0.160.1", codexOf(raw).Rollouts[0].Version)
	assert.Equal(t, codexOriginatorTUI, codexOf(raw).Rollouts[0].Originator)
}

// The script run for real reads the cli_version of each listed rollout.
func TestRunLocal_CollectScriptReadsTheCodexVersion(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	meta := strings.Replace(sessionMeta(codexSessionA, "/workspace/user/api", codexOriginatorTUI),
		`"source":"cli"`, `"source":"cli","cli_version":"0.160.1"`, 1)
	writeRollout(t, home, "2026/09/28", rolloutName("2026-09-28T01-00-00", codexSessionA), meta)

	out, err := runLocal(context.Background(), collectScript)
	require.NoError(t, err)
	raw, err := parseCollectOutput(out, collectPlan.full)
	require.NoError(t, err)
	require.Len(t, codexOf(raw).Rollouts, 1)
	assert.Equal(t, "0.160.1", codexOf(raw).Rollouts[0].Version)
}

// The script run for real hands over the last turn event of the tail even
// when this build does not know it, and skips turn_diff.
func TestRunLocal_CollectScriptReadsAnUnknownTurnEvent(t *testing.T) {
	testcap.RequirePS(t)
	home := t.TempDir()
	homedir.SetForTest(t, home)
	path := writeRollout(t, home, "2026/09/28", rolloutName("2026-09-28T01-00-00", codexSessionA),
		sessionMeta(codexSessionA, "/workspace/user/api", codexOriginatorTUI),
		`{"timestamp":"2026-09-28T01:00:01.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`,
		`{"timestamp":"2026-09-28T01:00:02.000Z","type":"event_msg","payload":{"type":"turn_paused","turn_id":"t1"}}`,
		`{"timestamp":"2026-09-28T01:00:03.000Z","type":"event_msg","payload":{"type":"turn_diff","unified_diff":""}}`,
	)
	pid := startStandInCodex(t, path)

	var held codexOpenRollout
	require.Eventually(t, func() bool {
		out, err := runLocal(context.Background(), collectScript)
		require.NoError(t, err)
		raw, err := parseCollectOutput(out, collectPlan.full)
		require.NoError(t, err)
		for _, o := range codexOf(raw).Open {
			if o.PID == pid {
				held = o
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond)
	assert.Equal(t, "turn_paused", held.Turn.UnknownEvent)
	assert.Empty(t, held.Turn.Event)
}

// A live codex process whose open rollout row cannot be read is listed with
// its state unknown, not as one whose session has not started; the row,
// held on two descriptors, is counted once.
func TestFormats_CodexAnUnreadableOpenRowMakesItsProcessUnknown(t *testing.T) {
	raw := codexRaw()
	codex := raw.Agents[agent.Codex].(*codexCollected)
	row := "31\t00:10\tnot a stamp\t\t\t\t\t\t/x/" + rolloutName("2026-09-27T11-57-03", codexSessionA)
	codex.addRow(codexSectionOpen, row)
	codex.addRow(codexSectionOpen, row)

	tasks, _, formats := buildTasksWithDiagnostics("", raw, collectedAt)
	require.Len(t, tasks, 1)
	assert.Equal(t, StateUnknown, tasks[0].State)
	assert.Equal(t, 31, tasks[0].PID)
	assert.Equal(t, map[string]int{"malformed_row": 1}, formats[agent.Codex].counts)
}

// A rollout held open on two descriptors is one session: what it has in
// an unknown format is counted once.
func TestFormats_CodexARolloutOnTwoDescriptorsIsCountedOnce(t *testing.T) {
	turn := codexTurn{DBStatus: "paused", UnknownEvent: "turn_started"}
	raw := codexRaw(openRollout(31, codexSessionA, 30, turn), openRollout(31, codexSessionA, 30, turn))

	got := formatsOf(t, raw)[agent.Codex]
	assert.Equal(t, map[string]int{"unknown_status": 1, "unknown_event": 1}, got.counts)
}
