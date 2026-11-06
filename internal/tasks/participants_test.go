package tasks

import (
	"github.com/stretchr/testify/require"
	"testing"
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
			require.Equal(t, []int{31, 32}, ts[0].LivePIDs, "duplicate descriptors count once")
		})
	}
	raw := fixtureRaw{Now: hostNow, Processes: []process{codexProc(31)}}.snapshot()
	require.Equal(t, []int{31}, buildLiveTasks("", raw, collectedAt)[0].LivePIDs)
}
