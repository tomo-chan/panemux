package tasks

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// detailPIDMissing is the detail of a state file with no pid at all.
const detailPIDMissing = "pid is missing"

// readState is one state file as read: the fields the dashboard uses, or
// why the file could not be read.
type readState struct {
	name   string
	reason UnreadableReason
	detail string
	st     claudeState
}

// readStateFile reads a ~/.claude/sessions file. Only pid and sessionId
// decide whether it can be read; every other field is optional (issue #252),
// so one of an unexpected type is read as absent (issue #313).
func readStateFile(file stateFile) readState {
	state := readState{name: file.Name}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(file.Data, &fields); err != nil {
		state.reason, state.detail = UnreadableNotJSON, err.Error()
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			state.detail = "not a JSON object"
		}
		return state
	}

	st := &state.st
	for name, dst := range map[string]any{
		"cwd":             &st.CWD,
		"status":          &st.Status,
		"waitingFor":      &st.WaitingFor,
		"statusUpdatedAt": &st.StatusUpdatedAt,
		"updatedAt":       &st.UpdatedAt,
		"startedAt":       &st.StartedAt,
	} {
		if raw, ok := fields[name]; ok {
			_ = json.Unmarshal(raw, dst) // a value of another type leaves the field empty
		}
	}

	if raw, ok := fields["pid"]; !ok {
		state.reason, state.detail = UnreadableInvalidPID, detailPIDMissing
	} else if json.Unmarshal(raw, &st.PID) != nil || st.PID <= 0 {
		state.reason, state.detail = UnreadableInvalidPID, "pid: "+string(raw)
	} else if raw, ok := fields["sessionId"]; !ok {
		state.reason, state.detail = UnreadableInvalidSessionID, "sessionId is missing"
	} else if json.Unmarshal(raw, &st.SessionID) != nil || !validSessionID.MatchString(st.SessionID) {
		state.reason, state.detail = UnreadableInvalidSessionID, "sessionId: "+string(raw)
	}
	return state
}

// boundedText is host-supplied text made safe to show and log: valid UTF-8,
// every control and invisible format character replaced, and at most
// runes (limit), the last of them an ellipsis when it was cut.
func boundedText(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return utf8.RuneError
		}
		return r
	}, strings.ToValidUTF8(s, string(utf8.RuneError)))
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit-1]) + "…"
}
