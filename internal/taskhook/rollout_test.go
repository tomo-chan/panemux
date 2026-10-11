package taskhook

import (
	"errors"
	"strings"
	"testing"
)

func metadataLog(parts ...string) string {
	return `{"type":"session_meta","payload":{"id":"` + testSession + `","source":"cli"}}` +
		"\n" + strings.Join(parts, "\n") + "\n"
}

func TestRolloutExactTurnReviewerAndTerminalVeto(t *testing.T) {
	contextLine := `{"type":"turn_context","payload":{"turn_id":"` + testTurn + `","approvals_reviewer":"user"}}`
	terminalLine := `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"` + testTurn + `"}}`
	for _, tc := range []struct {
		name  string
		parts []string
		veto  bool
	}{
		{"current", []string{contextLine}, false},
		{"terminal before watermark", []string{terminalLine, contextLine}, true},
		{"terminal after context", []string{contextLine, terminalLine}, true},
		{"later turn", []string{contextLine,
			`{"type":"turn_context","payload":{"turn_id":"later","approvals_reviewer":"user"}}`}, true},
		{"other terminal", []string{contextLine,
			`{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"other"}}`}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := metadataLog(tc.parts...)
			e, err := ScanRollout(strings.NewReader(log), Permission{
				SessionID: testSession, TurnID: testTurn, ToolKind: ToolBash,
			}, "file-1")
			if err != nil || e.Veto != tc.veto || e.Anchor.Offset != int64(len(log)) || e.Anchor.FileID != "file-1" {
				t.Fatalf("metadata evidence: %+v %v", e, err)
			}
		})
	}
}

func TestRolloutUnknownNeverCreatesEligibility(t *testing.T) {
	good := metadataLog(`{"type":"turn_context","payload":{"turn_id":"` + testTurn + `","approvals_reviewer":"user"}}`)
	for name, log := range map[string]string{
		"missing reviewer": strings.ReplaceAll(good, `,"approvals_reviewer":"user"`, ""),
		"null reviewer":    strings.ReplaceAll(good, `"approvals_reviewer":"user"`, `"approvals_reviewer":null`),
		"guardian":         strings.ReplaceAll(good, `"user"`, `"guardian"`),
		"wrong session":    strings.ReplaceAll(good, testSession, "00000000-0000-4000-8000-000000000002"),
		"no exact turn":    strings.ReplaceAll(good, testTurn, "other"),
		"subagent":         strings.ReplaceAll(good, `"cli"`, `{"subagent":"thread_spawn"}`),
		"partial line":     strings.TrimSuffix(good, "\n"),
		"corrupt line":     good + "{broken}\n",
		"oversize":         good + strings.Repeat(" ", MaxRolloutBytes),
		"long line":        good + strings.Repeat(" ", MaxRolloutLineBytes) + "\n",
		"duplicate key": strings.ReplaceAll(good, `"approvals_reviewer":"user"`,
			`"approvals_reviewer":"guardian","approvals_reviewer":"user"`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ScanRollout(strings.NewReader(log), Permission{
				SessionID: testSession, TurnID: testTurn, ToolKind: ToolBash,
			}, "file-1")
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("unknown proof accepted: %v", err)
			}
		})
	}
}
