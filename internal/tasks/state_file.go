package tasks

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"panemux/internal/session"
)

// readState is one state file as read: the fields the dashboard uses, or
// why the file could not be read.
type readState struct {
	name   string
	reason UnreadableReason
	detail string
	st     claudeState
}

// readStateFile reads a ~/.claude/sessions file with the parser the pane
// header uses too, keeping why it could not be read.
func readStateFile(file stateFile) readState {
	st, problem, detail := session.ParseClaudeSessionState(file.Data)
	return readState{name: file.Name, reason: UnreadableReason(problem), detail: detail, st: st}
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
