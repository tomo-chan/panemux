package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ParseClaudeSessionState is the one reader of ~/.claude/sessions files: the
// pane header and the task board both read them through it.
func TestParseClaudeSessionState(t *testing.T) {
	cases := map[string]struct {
		data    string
		problem ClaudeStateProblem
		detail  string
		want    ClaudeSessionState
	}{
		"complete": {
			data: `{"pid":42,"sessionId":"s-1","cwd":"/workspace/user/project","status":"busy",` +
				`"waitingFor":"input","version":"2.1.3","statusUpdatedAt":3,"updatedAt":2,"startedAt":1}`,
			want: ClaudeSessionState{
				PID: 42, SessionID: "s-1", CWD: "/workspace/user/project", Status: "busy",
				WaitingFor: "input", Version: "2.1.3", StatusUpdatedAt: 3, UpdatedAt: 2, StartedAt: 1,
			},
		},
		// An optional field of another type is read as absent (issue #313),
		// for the pane header as for the task board.
		"optional field of another type": {
			data: `{"pid":42,"sessionId":"s-1","cwd":"/workspace/user/project","status":5,"version":{}}`,
			want: ClaudeSessionState{PID: 42, SessionID: "s-1", CWD: "/workspace/user/project"},
		},
		"no cwd": {
			data: `{"pid":42,"sessionId":"s-1"}`,
			want: ClaudeSessionState{PID: 42, SessionID: "s-1"},
		},
		"not json":   {data: `{`, problem: ClaudeStateNotJSON, detail: "unexpected end of JSON input"},
		"array":      {data: `[]`, problem: ClaudeStateNotJSON, detail: "not a JSON object"},
		"null":       {data: `null`, problem: ClaudeStateNotJSON, detail: "not a JSON object"},
		"no pid":     {data: `{"sessionId":"s-1"}`, problem: ClaudeStateInvalidPID, detail: "pid is missing"},
		"zero pid":   {data: `{"pid":0,"sessionId":"s-1"}`, problem: ClaudeStateInvalidPID, detail: "pid: 0"},
		"string pid": {data: `{"pid":"42","sessionId":"s-1"}`, problem: ClaudeStateInvalidPID, detail: `pid: "42"`},
		"no session": {data: `{"pid":42}`, problem: ClaudeStateInvalidSessionID, detail: "sessionId is missing"},
		"bad session": {
			data: `{"pid":42,"sessionId":"../x"}`, problem: ClaudeStateInvalidSessionID, detail: `sessionId: "../x"`,
		},
		"empty session": {data: `{"pid":42,"sessionId":""}`, problem: ClaudeStateInvalidSessionID, detail: `sessionId: ""`},
	}
	for name, tc := range cases {
		got, problem, detail := ParseClaudeSessionState([]byte(tc.data))
		assert.Equal(t, tc.problem, problem, name)
		assert.Equal(t, tc.detail, detail, name)
		if tc.problem == "" {
			assert.Equal(t, tc.want, got, name)
		}
	}
}
