package session

import (
	"encoding/json"
	"errors"
)

// ClaudeSessionState is the part of ~/.claude/sessions/<pid>.json panemux
// reads. Claude Code writes this file itself; it is not a published format
// (see docs/DECISIONLOG.md), so every field but pid and sessionId is optional.
type ClaudeSessionState struct {
	SessionID  string
	CWD        string
	Status     string
	WaitingFor string
	// Version is the Claude Code version that wrote the file. It is
	// recorded, never checked.
	Version         string
	PID             int
	StatusUpdatedAt int64
	UpdatedAt       int64
	StartedAt       int64
}

// ClaudeStateProblem is why a state file could not be read; "" when it could.
type ClaudeStateProblem string

// The file is not a JSON object, its pid is missing or not a positive
// integer, or its sessionId is missing or not a session ID.
const (
	ClaudeStateNotJSON          ClaudeStateProblem = "not_json"
	ClaudeStateInvalidPID       ClaudeStateProblem = "invalid_pid"
	ClaudeStateInvalidSessionID ClaudeStateProblem = "invalid_session_id"
)

// Details that name no value from the file: one with no pid at all, and
// JSON that is not an object (an array, a scalar or null).
const (
	detailPIDMissing = "pid is missing"
	detailNotObject  = "not a JSON object"
)

// statusField is the state file's field for what the session is doing.
const statusField = "status"

// claudeStateFile is one file under ~/.claude/sessions, as a host read it.
type claudeStateFile struct {
	name string
	data []byte
}

// ParseClaudeSessionState reads one ~/.claude/sessions file, for the pane
// header and the task board alike. Only pid and sessionId decide whether it
// can be read (issue #252), so an optional field of an unexpected type is
// read as absent (issue #313). detail is what was found when it could not be
// read: the JSON error, or the offending field as written, unbounded.
func ParseClaudeSessionState(data []byte) (state ClaudeSessionState, problem ClaudeStateProblem, detail string) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return state, ClaudeStateNotJSON, detailNotObject
		}
		return state, ClaudeStateNotJSON, err.Error()
	}
	if fields == nil { // the JSON literal null
		return state, ClaudeStateNotJSON, detailNotObject
	}

	for name, dst := range map[string]any{
		"cwd":             &state.CWD,
		statusField:       &state.Status,
		"waitingFor":      &state.WaitingFor,
		"statusUpdatedAt": &state.StatusUpdatedAt,
		"updatedAt":       &state.UpdatedAt,
		"startedAt":       &state.StartedAt,
		"version":         &state.Version,
	} {
		if raw, ok := fields[name]; ok {
			_ = json.Unmarshal(raw, dst) // a value of another type leaves the field empty
		}
	}

	if raw, ok := fields["pid"]; !ok {
		return state, ClaudeStateInvalidPID, detailPIDMissing
	} else if json.Unmarshal(raw, &state.PID) != nil || state.PID <= 0 {
		return state, ClaudeStateInvalidPID, "pid: " + string(raw)
	}
	if raw, ok := fields["sessionId"]; !ok {
		return state, ClaudeStateInvalidSessionID, "sessionId is missing"
	} else if json.Unmarshal(raw, &state.SessionID) != nil || !validClaudeSessionID.MatchString(state.SessionID) {
		return state, ClaudeStateInvalidSessionID, "sessionId: " + string(raw)
	}
	return state, "", ""
}
