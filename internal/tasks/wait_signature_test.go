package tasks

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// claudeWaitRaw is one live claude session in the given state whose state
// file reports statusUpdatedAt and waitingFor as given; zero leaves the
// timestamp out.
func claudeWaitRaw(sessionID, status, waitingFor string, statusUpdatedAt int64) rawSnapshot {
	file := fmt.Sprintf(`{"pid":41,"sessionId":%q,"status":%q,"waitingFor":%q,"updatedAt":%d,"startedAt":%d`,
		sessionID, status, waitingFor, (hostNow-60)*1000, (hostNow-3600)*1000)
	if statusUpdatedAt != 0 {
		file += fmt.Sprintf(`,"statusUpdatedAt":%d`, statusUpdatedAt)
	}
	return rawSnapshot{
		Now:        hostNow,
		StateFiles: []stateFile{{Name: "41.json", Data: []byte(file + "}")}},
		Processes:  []process{claudeProc(41, 1)},
	}
}

func claudeSignature(t *testing.T, host string, raw rawSnapshot, at time.Time) string {
	t.Helper()
	tasks := buildTasks(host, raw, at)
	require.Len(t, tasks, 1)
	return tasks[0].WaitSignature
}

// A waiting claude task is signed from where its wait began on the host's
// own clock and what it waits for: collecting it again later, or with the
// host's clock reading differently against panemux's, gives the same
// signature, and a wait that began at another time or for another reason
// gives another one.
func TestBuildTasks_ClaudeWaitSignature(t *testing.T) {
	waitStart := (hostNow - 180) * 1000
	base := claudeSignature(t, "", claudeWaitRaw("s1", claudeStatusWaiting, "input needed", waitStart), collectedAt)
	require.NotEmpty(t, base)

	later := claudeWaitRaw("s1", claudeStatusWaiting, "input needed", waitStart)
	later.Now = hostNow + 30
	assert.Equal(t, base, claudeSignature(t, "", later, collectedAt.Add(30*time.Second)), "collected again 30s later")
	assert.Equal(t, base, claudeSignature(t, "", later, collectedAt.Add(-5*time.Minute)),
		"a host clock that reads differently against panemux's")

	others := map[string]string{
		"a new wait": claudeSignature(t, "",
			claudeWaitRaw("s1", claudeStatusWaiting, "input needed", waitStart+1), collectedAt),
		"another reason": claudeSignature(t, "",
			claudeWaitRaw("s1", claudeStatusWaiting, "approve Bash", waitStart), collectedAt),
		"another session": claudeSignature(t, "",
			claudeWaitRaw("s2", claudeStatusWaiting, "input needed", waitStart), collectedAt),
		"another host": claudeSignature(t, "build-box",
			claudeWaitRaw("s1", claudeStatusWaiting, "input needed", waitStart), collectedAt),
	}
	for name, sig := range others {
		assert.NotEmpty(t, sig, name)
		assert.NotEqual(t, base, sig, name)
	}
}

// Only a wait is signed, and only from a wait start the state file itself
// reports: updatedAt and startedAt are not taken in its place.
func TestBuildTasks_ClaudeWaitSignatureNeedsAWaitStart(t *testing.T) {
	cases := []struct {
		name            string
		status          string
		statusUpdatedAt int64
	}{
		{name: "busy", status: claudeStatusBusy, statusUpdatedAt: (hostNow - 180) * 1000},
		{name: "idle", status: claudeStatusIdle, statusUpdatedAt: (hostNow - 180) * 1000},
		{name: "wait without statusUpdatedAt", status: claudeStatusWaiting},
		{name: "wait with a negative statusUpdatedAt", status: claudeStatusWaiting, statusUpdatedAt: -5},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			raw := claudeWaitRaw("s1", tt.status, "input needed", tt.statusUpdatedAt)
			assert.Empty(t, claudeSignature(t, "", raw, collectedAt))
		})
	}
}

// An unreadable state file is unknown, never a signed wait.
func TestBuildTasks_UnreadableStateFileHasNoWaitSignature(t *testing.T) {
	raw := rawSnapshot{
		Now:        hostNow,
		StateFiles: []stateFile{{Name: "41.json", Data: []byte(`{"pid":41,"status":claudeStatusWaiting,"statusUpdatedAt`)}},
		Processes:  []process{claudeProc(41, 1)},
	}
	task := buildTasks("", raw, collectedAt)[0]
	assert.Equal(t, StateUnknown, task.State)
	assert.Empty(t, task.WaitSignature)
}

func codexWaitTurn(askedAt int64) codexTurn {
	return codexTurn{
		DBStatus: "inProgress", DBStartedAt: hostNow - 120,
		LastItem: "function_call", LastItemName: "request_user_input", LastItemAt: askedAt,
	}
}

func codexSignature(t *testing.T, raw rawSnapshot, at time.Time) Task {
	t.Helper()
	tasks := buildTasks("", raw, at)
	require.Len(t, tasks, 1)
	return tasks[0]
}

// A codex question is signed from the time its own request_user_input item
// carries. The rollout being written again (its mtime) or the turn's start
// does not change it; a second question in the same turn does.
func TestBuildTasks_CodexWaitSignature(t *testing.T) {
	askedAt := (hostNow - 45) * 1000
	base := codexSignature(t, codexRaw(openRollout(31, codexSessionA, 45, codexWaitTurn(askedAt))), collectedAt)
	require.Equal(t, StateWait, base.State)
	require.NotEmpty(t, base.WaitSignature)

	touched := codexRaw(openRollout(31, codexSessionA, 2, codexWaitTurn(askedAt)))
	touched.Now = hostNow + 10
	assert.Equal(t, base.WaitSignature,
		codexSignature(t, touched, collectedAt.Add(time.Minute)).WaitSignature, "rollout rewritten, collected later")

	again := codexSignature(t, codexRaw(openRollout(31, codexSessionA, 5, codexWaitTurn(askedAt+40_000))), collectedAt)
	assert.NotEmpty(t, again.WaitSignature)
	assert.NotEqual(t, base.WaitSignature, again.WaitSignature, "a second question in the same turn")

	other := codexSignature(t, codexRaw(openRollout(31, codexSessionB, 45, codexWaitTurn(askedAt))), collectedAt)
	assert.NotEqual(t, base.WaitSignature, other.WaitSignature, "another session")
}

// A question whose item carries no readable time is still a wait, unsigned:
// the rollout's mtime and the turn's start are not used in its place. A
// codex task that is not waiting has no signature.
func TestBuildTasks_CodexWaitSignatureNeedsTheQuestionsOwnTime(t *testing.T) {
	unsigned := codexSignature(t, codexRaw(openRollout(31, codexSessionA, 45, codexWaitTurn(0))), collectedAt)
	assert.Equal(t, StateWait, unsigned.State)
	assert.Empty(t, unsigned.WaitSignature)

	busy := codexWaitTurn((hostNow - 45) * 1000)
	busy.LastItem, busy.LastItemName = "function_call_output", ""
	assert.Empty(t, codexSignature(t, codexRaw(openRollout(31, codexSessionA, 45, busy)), collectedAt).WaitSignature)

	leftover := codexWaitTurn((hostNow - 45) * 1000)
	leftover.DBStartedAt = hostNow - codexProcElapsed - 60
	task := codexSignature(t, codexRaw(openRollout(31, codexSessionA, 45, leftover)), collectedAt)
	assert.Equal(t, StateIdle, task.State)
	assert.Empty(t, task.WaitSignature)
}

// The same session ID and wait start under the two agents are two waits.
func TestWaitSignature_DependsOnEveryPart(t *testing.T) {
	base := waitSignature("h", AgentClaude, "s", 1000, "k")
	require.NotEmpty(t, base)
	assert.Equal(t, base, waitSignature("h", AgentClaude, "s", 1000, "k"))
	for name, sig := range map[string]string{
		"agent":   waitSignature("h", AgentCodex, "s", 1000, "k"),
		"host":    waitSignature("g", AgentClaude, "s", 1000, "k"),
		"session": waitSignature("h", AgentClaude, "t", 1000, "k"),
		"start":   waitSignature("h", AgentClaude, "s", 1001, "k"),
		"kind":    waitSignature("h", AgentClaude, "s", 1000, "l"),
		// A separator inside one part must not let two parts trade bytes.
		"shifted": waitSignature("h", AgentClaude, "s\x00", 1000, "k"),
	} {
		assert.NotEqual(t, base, sig, name)
	}
	assert.Empty(t, waitSignature("h", AgentClaude, "s", 0, "k"), "no wait start")
	assert.Empty(t, waitSignature("h", AgentClaude, "", 1000, "k"), "no session")
}
