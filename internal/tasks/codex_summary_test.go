package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/fileops"
	"panemux/internal/homedir"
)

func codexMessage(id, role, text string) string {
	block := "input_text"
	if role == "assistant" {
		block = "output_text"
	}
	raw, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{
		"type": "message", "id": id, "role": role, "content": []any{map[string]string{"type": block, "text": text}},
	}})
	return string(raw)
}

func TestBuildCodexExcerpt_ConversationOnly(t *testing.T) {
	body := lines(
		codexMessage("global", "user", "# AGENTS.md instructions for /workspace/user/project\n\n"+
			"<INSTRUCTIONS>secret-global</INSTRUCTIONS>\n<environment_context>secret-environment</environment_context>"),
		codexMessage("env", "user", "<environment_context>secret-cwd</environment_context>"),
		codexMessage("dev", "developer", "secret-developer"),
		codexMessage("u1", "user", "Fix the race"),
		`{"type":"response_item","payload":{"type":"function_call","arguments":"secret-call"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","output":"secret-result"}}`,
		`{"type":"response_item","payload":{"type":"reasoning","summary":[{"text":"secret-reasoning"}]}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"duplicate-event"}}`,
		codexMessage("a1", "assistant", "Fixed the race"),
		codexMessage("a1", "assistant", "Fixed the race"),
		codexMessage("u2", "user", "Run checks"),
		codexMessage("u3", "user", "Run checks"),
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[`+
			`{"type":"image","image_url":"secret-image"},{"type":"output_text","text":"secret-wrong-block"}]}}`,
		"not JSON",
	)
	got, ok := buildCodexExcerpt(transcriptData{Head: body, Whole: true})
	require.True(t, ok)
	assert.Contains(t, got, "[user] Fix the race")
	assert.Equal(t, 1, strings.Count(got, "Fixed the race"))
	assert.Equal(t, 2, strings.Count(got, "Run checks"), "distinct identical user messages remain")
	assert.NotContains(t, got, "secret")
	assert.NotContains(t, got, "duplicate-event")
}

func TestBuildCodexExcerpt_BoundsAndPartialLines(t *testing.T) {
	first := codexMessage("first", "user", strings.Repeat("初", excerptFirstBytes))
	var tail []string
	for i := 0; i < 30; i++ {
		tail = append(tail, codexMessage(strconv.Itoa(i), "assistant", strings.Repeat("最", excerptMessageBytes)))
	}
	tail = append(tail, codexMessage("last", "user", "Newest instruction"))
	got, ok := buildCodexExcerpt(transcriptData{
		Head: append(lines(first), []byte(codexMessage("cut-head", "user", "CUT-HEAD"))...),
		Tail: append([]byte(codexMessage("cut-tail", "assistant", "CUT-TAIL")+"\n"), lines(tail...)...),
	})
	require.True(t, ok)
	assert.Contains(t, got, excerptFirstHeading)
	assert.Contains(t, got, "Newest instruction")
	assert.NotContains(t, got, "CUT-")
	assert.True(t, utf8.ValidString(got))
	assert.LessOrEqual(t, len(got), excerptFirstBytes+excerptRecentBytes+200)
	_, ok = buildCodexExcerpt(transcriptData{
		Head: lines(`{"type":"event_msg","payload":{"type":"user_message","message":"event-only"}}`), Whole: true,
	})
	assert.False(t, ok, "never fall back to raw logs or event text")
}

func TestBuildCodexExcerpt_ScaffoldingAndIDlessMessages(t *testing.T) {
	got, ok := buildCodexExcerpt(transcriptData{Head: lines(
		codexMessage("", "user", "<environment_context>cwd</environment_context>\nActual request"),
		codexMessage("", "assistant", "Same reply"), codexMessage("", "assistant", "Same reply"),
		codexMessage("quoted", "user", "Please explain `<environment_context>`"),
	), Whole: true})
	require.True(t, ok)
	assert.Contains(t, got, "Actual request")
	assert.NotContains(t, got, "[user] <environment_context>cwd")
	assert.Equal(t, 2, strings.Count(got, "Same reply"))
	assert.Contains(t, got, "Please explain `<environment_context>`")
}

func TestBuildCodexTranscriptScript(t *testing.T) {
	script, err := buildCodexTranscriptScript(summarySessionID)
	require.NoError(t, err)
	assert.Contains(t, script, ".codex/sessions/*/*/*/\"rollout-")
	assert.NotContains(t, script, "{{")
	for _, bad := range []string{"", "abc", "../x", "a'b", "$(touch x)", "a\nb"} {
		_, err = buildCodexTranscriptScript(bad)
		assert.ErrorIs(t, err, ErrInvalidSummary)
	}
}

func TestRunLocal_CodexTranscriptReadsNewestAndBounds(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	root := filepath.Join(home, ".codex", "sessions", "2026", "10", "08")
	require.NoError(t, os.MkdirAll(root, 0o700))
	old := filepath.Join(root, "rollout-2026-10-08T01-00-00-"+summarySessionID+".jsonl")
	best := filepath.Join(root, "rollout-2026-10-08T02-00-00-"+summarySessionID+".jsonl")
	body := strings.Repeat("h", transcriptHeadBytes) + strings.Repeat("x", 100) + strings.Repeat("t", transcriptTailBytes)
	require.NoError(t, os.WriteFile(old, []byte("old"), 0o600))
	require.NoError(t, os.WriteFile(best, []byte(body), 0o600))
	at := time.Unix(1000, 0)
	require.NoError(t, os.Chtimes(old, at, at))
	require.NoError(t, os.Chtimes(best, at.Add(time.Second), at.Add(time.Second)))
	script, err := buildCodexTranscriptScript(summarySessionID)
	require.NoError(t, err)
	out, err := runLocal(context.Background(), script)
	require.NoError(t, err)
	data, err := parseTranscriptOutput(out)
	require.NoError(t, err)
	assert.False(t, data.Whole)
	assert.Equal(t, body[:transcriptHeadBytes], string(data.Head))
	assert.Equal(t, body[len(body)-transcriptTailBytes:], string(data.Tail))
	require.NoError(t, os.Remove(best))
	out, err = runLocal(context.Background(), script)
	require.NoError(t, err)
	data, err = parseTranscriptOutput(out)
	require.NoError(t, err)
	assert.Equal(t, "old", string(data.Head))
	require.NoError(t, os.Remove(old))
	out, err = runLocal(context.Background(), script)
	require.NoError(t, err)
	_, err = parseTranscriptOutput(out)
	assert.ErrorIs(t, err, ErrNoTranscript)
}

func fakeCodex(t *testing.T, action string) string {
	t.Helper()
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := "#!/bin/sh\n{ pwd; for a in \"$@\"; do printf 'ARG:%s\\n' \"$a\"; done; echo STDIN:; cat; } > '" +
		record + "'\n" + "while [ $# -gt 0 ]; do case \"$1\" in " +
		"--output-last-message) shift; answer=$1;; --output-schema) shift; schema=$1;; esac; shift; done\n" + action + "\n"
	//nolint:gosec // executable test fixture
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record
}

func TestCodexSummarizer_UsesDefaultSettingsAndBoundedStdin(t *testing.T) {
	record := fakeCodex(t, `cat "$schema" >/dev/null
printf '%s' '{"summary":" Done ","remaining":[" Check ",""]}' > "$answer"`)
	got, err := newCodexSummarizer()(context.Background(), "[user] hello")
	require.NoError(t, err)
	assert.Equal(t, Summary{Text: "Done", Remaining: []string{"Check"}}, got)
	raw, err := os.ReadFile(record)
	require.NoError(t, err)
	text := string(raw)
	cwd, _, _ := strings.Cut(text, "\n")
	_, err = os.Stat(cwd) //nolint:gosec // test: path recorded by the controlled stand-in
	assert.True(t, os.IsNotExist(err))
	assert.Contains(t, text, "ARG:exec\n")
	assert.Contains(t, text, "ARG:--ephemeral\n")
	assert.Contains(t, text, "ARG:-\n")
	assert.Contains(t, text, summaryInstruction)
	assert.True(t, strings.HasSuffix(text, "[user] hello"))
	for _, flag := range []string{
		"--model", "--profile", "--sandbox", "--config", "--ignore-user-config",
		"--full-auto", "--dangerously-bypass", "--yolo",
	} {
		assert.NotContains(t, text, "ARG:"+flag)
	}
	_, err = newCodexSummarizer()(context.Background(), strings.Repeat("s", maxCodexExcerptBytes+1))
	require.EqualError(t, err, "codex summary excerpt is too large")
	excerpt := strings.Repeat("s", maxCodexExcerptBytes)
	_, err = newCodexSummarizer()(context.Background(), excerpt)
	require.NoError(t, err, "the exact excerpt limit is accepted")
	raw, err = os.ReadFile(record)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(string(raw), "Conversation excerpt:\n"+excerpt))
}

func TestCodexSummarizer_AnswerByteBoundary(t *testing.T) {
	const answer = `{"summary":"Done","remaining":[]}`
	for _, size := range []int{maxCodexAnswerBytes - 1, maxCodexAnswerBytes, maxCodexAnswerBytes + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			fakeCodex(t, "printf '%s' '"+answer+"' > \"$answer\"\n"+
				"printf '%"+strconv.Itoa(size-len(answer))+"s' '' >> \"$answer\"")
			got, err := newCodexSummarizer()(context.Background(), "x")
			if size > maxCodexAnswerBytes {
				require.EqualError(t, err, "codex's answer could not be read")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, Summary{Text: "Done", Remaining: []string{}}, got)
		})
	}
}

func TestCodexSummarizer_FixedFailures(t *testing.T) {
	for name, action := range map[string]string{
		"exit":           "echo secret-stderr >&2; echo secret-stdout; exit 7",
		"missing answer": "true",
		"malformed":      `printf '%s' '{"summary":"secret"}' > "$answer"`,
		"oversize":       `dd if=/dev/zero of="$answer" bs=65536 count=2 2>/dev/null`,
	} {
		t.Run(name, func(t *testing.T) {
			fakeCodex(t, action)
			_, err := newCodexSummarizer()(context.Background(), "x")
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "secret")
		})
	}
	t.Run("missing binary", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := newCodexSummarizer()(context.Background(), "x")
		require.EqualError(t, err, "codex could not run while summarizing")
	})
	t.Run("setup", func(t *testing.T) {
		fileops.SetOpsForTest(t, (&fileops.Spy{WriteErr: os.ErrPermission}).Ops())
		_, err := newCodexSummarizer()(context.Background(), "x")
		require.EqualError(t, err, "codex summary files could not be prepared")
	})
}

func TestCodexSummarizer_CancellationKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	fakeCodex(t, "sleep 30 &\necho $! > '"+pidFile+"'\nwait")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			if raw, err := os.ReadFile(pidFile); err == nil && strings.HasSuffix(string(raw), "\n") {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	_, err := newCodexSummarizer()(ctx, "x")
	require.EqualError(t, err, "codex did not finish summarizing in time")
	raw, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	assert.Eventually(t, func() bool { return !processRunning(pid) }, 5*time.Second, 20*time.Millisecond)
}

func TestParseCodexSummaryOutput(t *testing.T) {
	for _, bad := range []string{
		"not json", `{"summary":"x"}`, `{"summary":null,"remaining":[]}`, `{"summary":"x","remaining":null}`,
		`{"summary":" ","remaining":[]}`, `{"summary":1,"remaining":[]}`, `{"summary":"x","remaining":[1]}`,
		`{"summary":"x","remaining":[],"extra":"secret"}`, `{"summary":"x","remaining":[]} {}`,
	} {
		_, err := parseCodexSummaryOutput([]byte(bad))
		require.Error(t, err)
		assert.NotContains(t, err.Error(), bad)
	}
	items := []string{strings.Repeat("次", maxSummaryItemBytes)}
	for range maxSummaryRemaining + 5 {
		items = append(items, "Check")
	}
	out, _ := json.Marshal(map[string]any{"summary": strings.Repeat("日", maxSummaryTextBytes), "remaining": items})
	got, err := parseCodexSummaryOutput(out)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got.Text), maxSummaryTextBytes+len("…"))
	assert.True(t, utf8.ValidString(got.Text))
	assert.LessOrEqual(t, len(got.Remaining[0]), maxSummaryItemBytes+len("…"))
	assert.Len(t, got.Remaining, maxSummaryRemaining)
}
