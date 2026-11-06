package tasks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiveClaudePreservesProcessesBeforeSessionDedup(t *testing.T) {
	raw := fixtureRaw{Now: hostNow, StateFiles: []stateFile{
		{Name: "11.json", Data: stateJSON(11, "same", "busy")},
		{Name: "12.json", Data: stateJSON(12, "same", "busy")},
		{Name: "13.json", Data: stateJSON(13, "same", "busy")},
	}, Processes: []process{claudeProc(11, 1), claudeProc(12, 1)}}.snapshot()
	ts := buildLiveTasks("", raw, collectedAt)
	require.Len(t, ts, 1)
	require.ElementsMatch(t, []int{11, 12}, ts[0].LivePIDs)
}

func TestLiveCodexPreservesProcessesBeforeSessionDedup(t *testing.T) {
	for _, daemon := range []bool{false, true} {
		name := "tui"
		if daemon {
			name = "daemon"
		}
		t.Run(name, func(t *testing.T) {
			first := openRollout(31, "shared", 1, codexTurn{})
			second := openRollout(32, "shared", 1, codexTurn{})
			procs := []process{codexProc(31), codexProc(32)}
			if daemon {
				for i := range procs {
					procs[i].Command = "codex app-server"
				}
				first.Rollout.Originator = codexOriginatorTUI
				second.Rollout.Originator = codexOriginatorTUI
			}
			raw := fixtureRaw{Now: hostNow, Processes: procs, CodexOpen: []codexOpenRollout{first, second, first}}.snapshot()
			ts := buildLiveTasks("", raw, collectedAt)
			require.Len(t, ts, 1, "task display still collapses a shared session")
			want := []int{31, 32}
			if daemon {
				want = []int{31} // Helpers serving one session are not independent agents.
			}
			require.Equal(t, want, ts[0].LivePIDs, "duplicate descriptors and helper processes count once")
		})
	}
	raw := fixtureRaw{Now: hostNow, Processes: []process{codexProc(31)}}.snapshot()
	require.Equal(t, []int{31}, buildLiveTasks("", raw, collectedAt)[0].LivePIDs)
}

func TestLiveCodexExcludesHelpersOfTUIOwnedSessions(t *testing.T) {
	for _, tc := range []struct {
		name string
		tuis []int
	}{
		{name: "one_tui", tuis: []int{31}},
		{name: "two_independent_tuis", tuis: []int{31, 32}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var procs []process
			var open []codexOpenRollout
			for _, pid := range tc.tuis {
				procs = append(procs, codexProc(pid))
				open = append(open, openRollout(pid, "shared", 1, codexTurn{}))
			}
			for _, pid := range []int{40, 41} {
				procs = append(procs, process{PID: pid, PPID: 1, Command: "codex app-server"})
				helper := openRollout(pid, "shared", 1, codexTurn{})
				helper.Rollout.Originator = codexOriginatorTUI
				open = append(open, helper, helper)
			}
			raw := fixtureRaw{Now: hostNow, Processes: procs, CodexOpen: open}.snapshot()
			ts := buildLiveTasks("", raw, collectedAt)
			require.Len(t, ts, 1)
			require.Equal(t, tc.tuis, ts[0].LivePIDs)
			require.Equal(t, tc.tuis[0], ts[0].PID)
			require.NotEqual(t, LocationDaemon, ts[0].Location.Kind)
		})
	}
}

func TestLiveClaudeFallbackPreservesProcess(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "missing"},
		{name: "unreadable", data: []byte("{")},
		{name: "invalid_pid", data: []byte(`{"pid":0,"sessionId":"s"}`)},
		{name: "invalid_session", data: []byte(`{"pid":31,"sessionId":""}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var files []stateFile
			if tc.data != nil {
				files = append(files, stateFile{Name: "31.json", Data: tc.data})
			}
			raw := fixtureRaw{Now: hostNow, StateFiles: files, Processes: []process{
				claudeProc(31, 1), {PID: 32, PPID: 1, Command: "claude -p batch"},
			}}.snapshot()
			ts := buildLiveTasks("", raw, collectedAt)
			require.Len(t, ts, 1)
			require.Equal(t, "local:claude:pid-31", ts[0].ID)
			require.Empty(t, ts[0].SessionID)
			require.Equal(t, StateUnknown, ts[0].State)
			require.Equal(t, 31, ts[0].PID)
			require.Equal(t, []int{31}, ts[0].LivePIDs)
		})
	}
}
