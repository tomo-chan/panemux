package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"panemux/internal/homedir"
)

func codexTranscript(text string) []byte {
	body := lines(`{"type":"session_meta","payload":{"id":"`+summarySessionID+`"}}`, codexMessage("u", "user", text))
	return transcriptOutput(len(body), string(body))
}

func TestSummaries_CodexRoutingStatesAndAgentIsolation(t *testing.T) {
	for _, state := range []State{StateWait, StateIdle, StateBusy, StateStop, StateUnknown} {
		t.Run(string(state), func(t *testing.T) {
			claude, codex := &fakeSummarizer{result: Summary{Text: "Claude"}}, &fakeSummarizer{result: Summary{Text: "Codex"}}
			s := New(Options{RunLocal: func(_ context.Context, script string) ([]byte, error) {
				if strings.Contains(script, ".codex/sessions") {
					return codexTranscript("Codex request"), nil
				}
				return conversationLog("Claude request"), nil
			}, Summarize: claude.summarize, SummarizeCodex: codex.summarize})
			defer s.Close()
			list := []Task{
				{ID: "claude", Agent: AgentClaude, SessionID: summarySessionID,
					State: state, Log: &LogVersion{ModTime: 1, Size: 2}},
				{ID: "codex", Agent: AgentCodex, SessionID: summarySessionID, State: state, Log: &LogVersion{ModTime: 1, Size: 2}},
				{ID: "pid", Agent: AgentCodex, State: StateIdle},
			}
			s.rememberSummaryTasks("", list)
			s.Summaries(list)
			s.waitSummaries()
			if state == StateIdle || state == StateWait {
				assert.Len(t, s.Summaries(list), 2)
			} else {
				assert.Empty(t, s.Summaries(list))
			}
			_, err := s.RequestAgentSummary("", AgentCodex, summarySessionID)
			require.NoError(t, err)
			_, err = s.RequestSummary("", summarySessionID)
			require.NoError(t, err)
			s.waitSummaries()
			views := s.Summaries(list)
			assert.Equal(t, "Codex", views["codex"].Text)
			assert.Equal(t, "Claude", views["claude"].Text)
			assert.NotContains(t, views, "pid")
			assert.Len(t, codex.excerpts, 1)
			assert.Len(t, claude.excerpts, 1)
			list[1].Log = &LogVersion{ModTime: 2, Size: 2}
			views = s.Summaries(list)
			s.waitSummaries()
			if state == StateBusy || state == StateStop || state == StateUnknown {
				assert.True(t, views["codex"].Outdated)
			}
			_, err = s.RequestAgentSummary("", "other", summarySessionID)
			assert.ErrorIs(t, err, ErrInvalidSummary)
		})
	}
}

func TestBuildTasks_CodexLogSelection(t *testing.T) {
	a := codexRollout{Originator: codexOriginatorTUI, SessionID: summarySessionID,
		File: "rollout-2026-10-08T01-00-00-" + summarySessionID + ".jsonl", ModTime: 100, Size: 9}
	b := a
	b.File = "rollout-2026-10-08T02-00-00-" + summarySessionID + ".jsonl"
	b.Size = 10
	for _, running := range []bool{false, true} {
		t.Run(strconv.FormatBool(running), func(t *testing.T) {
			raw := rawSnapshot{Now: 1000, CodexRollouts: []codexRollout{a, b}}
			if running {
				raw.Processes = []process{{PID: 10, Command: "codex"}}
				raw.CodexOpen = []codexOpenRollout{
					{PID: 10, File: a.File, Rollout: a}, {PID: 10, File: b.File, Rollout: b},
				}
			}
			got := buildTasks("", raw, time.Unix(1000, 0))
			require.Len(t, got, 1)
			require.NotNil(t, got[0].Log)
			assert.Equal(t, b.File, got[0].Log.File)
			assert.Equal(t, int64(10), got[0].Log.Size)
		})
	}
}

func TestParseCodexTranscript_VerifiesSession(t *testing.T) {
	_, err := parseCodexTranscriptOutput(codexTranscript("request"), summarySessionID)
	require.NoError(t, err)
	for _, body := range []string{
		codexMessage("u", "user", "no meta"), `{"type":"session_meta","payload":{"id":"other"}}`,
	} {
		_, err = parseCodexTranscriptOutput(transcriptOutput(len(body), body), summarySessionID)
		require.Error(t, err)
	}
}

func TestCodexTranscriptReader_PinsFileAndRejectsChanges(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	root := filepath.Join(home, ".codex", "sessions", "2026", "10", "08")
	require.NoError(t, os.MkdirAll(root, 0o700))
	name := "rollout-2026-10-08T01-00-00-" + summarySessionID + ".jsonl"
	path := filepath.Join(root, name)
	body := lines(`{"type":"session_meta","payload":{"id":"`+summarySessionID+`"}}`, codexMessage("u", "user", "chosen"))
	require.NoError(t, os.WriteFile(path, body, 0o600))
	at := time.Unix(1000, 0)
	require.NoError(t, os.Chtimes(path, at, at))
	other := filepath.Join(root, "rollout-2026-10-08T02-00-00-"+summarySessionID+".jsonl")
	require.NoError(t, os.WriteFile(other, []byte("other"), 0o600))
	version := LogVersion{File: name, ModTime: 1000, Size: int64(len(body))}
	script, err := buildCodexTranscriptScriptForLog(summarySessionID, version)
	require.NoError(t, err)
	out, err := runLocal(context.Background(), script)
	require.NoError(t, err)
	got, err := parseCodexTranscriptOutput(out, summarySessionID)
	require.NoError(t, err)
	assert.Equal(t, body, got.Head)
	// A wrapper changes the file just after head has read it, inside the read window.
	out, err = runLocal(context.Background(), `head() { command head "$@"; if [ "$1" = '-c' ]; then printf x >> "$3"; fi; }
`+script)
	require.NoError(t, err)
	_, err = parseCodexTranscriptOutput(out, summarySessionID)
	require.Error(t, err)
	out, err = runLocal(context.Background(), script)
	require.NoError(t, err)
	_, err = parseCodexTranscriptOutput(out, summarySessionID)
	require.Error(t, err, "changed before read")
	for _, bad := range []LogVersion{{File: "../x"}, {File: name, Size: -1}, {File: name, ModTime: -1}} {
		_, err = buildCodexTranscriptScriptForLog(summarySessionID, bad)
		require.ErrorIs(t, err, ErrInvalidSummary)
	}
	require.NoError(t, os.Remove(path))
	out, err = runLocal(context.Background(), script)
	require.NoError(t, err)
	_, err = parseCodexTranscriptOutput(out, summarySessionID)
	require.Error(t, err, "never silently choose the other file")
}
