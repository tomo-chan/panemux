package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/homedir"
)

func TestBuildTranscriptScript(t *testing.T) {
	script, err := buildTranscriptScript("5d7e3a90-1b2c-4d3e-8f40-51627384a5b6")
	require.NoError(t, err)
	assert.Contains(t, script, "sid='5d7e3a90-1b2c-4d3e-8f40-51627384a5b6'")
	assert.NotContains(t, script, "{{", "every placeholder is filled")
	assert.Contains(t, script, "head -c "+strconv.Itoa(transcriptHeadBytes))
	assert.Contains(t, script, "tail -c "+strconv.Itoa(transcriptTailBytes))

	for _, bad := range []string{"", "a'b", "a b", "../x", "a/b", "a;b", "$(x)", "a\nb"} {
		_, err := buildTranscriptScript(bad)
		assert.ErrorIs(t, err, ErrInvalidSummary, "%q", bad)
	}
}

func transcriptOutput(size int, body string) []byte {
	return []byte("::panemux-transcript v1 " + strconv.Itoa(size) + "\n" + body + "\n::end\n")
}

func TestParseTranscriptOutput_WholeFile(t *testing.T) {
	body := "{\"a\":1}\n{\"b\":2}\n"
	out := append([]byte("login banner\n"), transcriptOutput(len(body), body)...)

	got, err := parseTranscriptOutput(out)
	require.NoError(t, err)
	assert.True(t, got.Whole)
	assert.Equal(t, body, string(got.Head))
	assert.Empty(t, got.Tail)
}

func TestParseTranscriptOutput_HeadAndTail(t *testing.T) {
	head := strings.Repeat("h", transcriptHeadBytes)
	tail := strings.Repeat("t", transcriptTailBytes)
	size := transcriptHeadBytes + transcriptTailBytes + 1

	got, err := parseTranscriptOutput(transcriptOutput(size, head+tail))
	require.NoError(t, err)
	assert.False(t, got.Whole)
	assert.Equal(t, head, string(got.Head))
	assert.Equal(t, tail, string(got.Tail))
}

// A log whose body itself holds a line that looks like a marker is still
// read by length, not by searching for the marker.
func TestParseTranscriptOutput_ReadsTheBodyByLength(t *testing.T) {
	body := "x\n::end\n::panemux-transcript none\n"
	got, err := parseTranscriptOutput(transcriptOutput(len(body), body))
	require.NoError(t, err)
	assert.Equal(t, body, string(got.Head))
}

func TestParseTranscriptOutput_Errors(t *testing.T) {
	cases := []struct {
		name, out, want string
	}{
		{name: "no header", out: "hello\n", want: "has no header"},
		{name: "no header, no newline", out: "hello", want: "has no header"},
		{name: "header not at a line start", out: "x::panemux-transcript v1 3\nabc\n::end\n", want: "has no header"},
		{name: "ended in header", out: "::panemux-transcript v1 3", want: "ended in its header"},
		{name: "empty header", out: "::panemux-transcript \n\n::end\n", want: "unknown header"},
		{name: "bad size", out: "::panemux-transcript v1 many\nabc\n::end\n", want: "unknown header"},
		{name: "negative size", out: "::panemux-transcript v1 -3\nabc\n::end\n", want: "unknown header"},
		{name: "other version", out: "::panemux-transcript v2 3\nabc\n::end\n", want: "unknown header"},
		{name: "body too short", out: "::panemux-transcript v1 10\nabc\n::end\n", want: "incomplete"},
		{name: "no end", out: "::panemux-transcript v1 3\nabc\n", want: "incomplete"},
		{name: "cut off", out: "::panemux-transcript v1 3\nab", want: "incomplete"},
		{name: "text before end", out: "::panemux-transcript v1 3\nabcdef\n::end\n", want: "incomplete"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseTranscriptOutput([]byte(tc.out))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotErrorIs(t, err, ErrNoTranscript)
		})
	}
}

// The header is found after a login banner, blank lines included, and at
// the very start of the output.
func TestParseTranscriptOutput_FindsTheHeaderAfterABanner(t *testing.T) {
	body := "{}\n"
	for name, prefix := range map[string]string{
		"at the start":     "",
		"after a banner":   "Welcome\n",
		"after a blank":    "\n",
		"after two blanks": "motd\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseTranscriptOutput(append([]byte(prefix), transcriptOutput(len(body), body)...))
			require.NoError(t, err)
			assert.Equal(t, body, string(got.Head))
		})
	}
}

// An empty log is a log, with nothing in it.
func TestParseTranscriptOutput_AnEmptyLog(t *testing.T) {
	got, err := parseTranscriptOutput(transcriptOutput(0, ""))
	require.NoError(t, err)
	assert.True(t, got.Whole)
	assert.Empty(t, got.Head)
}

// A log exactly as large as the head and the tail together still arrives
// whole.
func TestParseTranscriptOutput_ALogAtTheWholeLimitIsWhole(t *testing.T) {
	size := transcriptHeadBytes + transcriptTailBytes
	got, err := parseTranscriptOutput(transcriptOutput(size, strings.Repeat("w", size)))
	require.NoError(t, err)
	assert.True(t, got.Whole)
	assert.Len(t, got.Head, size)
	assert.Empty(t, got.Tail)
}

func TestParseTranscriptOutput_NoLog(t *testing.T) {
	_, err := parseTranscriptOutput([]byte("banner\n::panemux-transcript none\n"))
	assert.ErrorIs(t, err, ErrNoTranscript)
}

// The fetch script runs under sh and finds the log in whichever project
// directory holds it, whole when it is small.
func TestRunLocal_TranscriptScriptReadsTheLog(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	project := filepath.Join(home, ".claude", "projects", "-workspace-user-project")
	require.NoError(t, os.MkdirAll(project, 0o700))
	body := `{"type":"user","message":{"role":"user","content":"hi"}}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(project, "abc.jsonl"), []byte(body), 0o600))

	script, err := buildTranscriptScript("abc")
	require.NoError(t, err)
	out, err := runLocal(context.Background(), script)
	require.NoError(t, err)
	got, err := parseTranscriptOutput(out)
	require.NoError(t, err)
	assert.True(t, got.Whole)
	assert.Equal(t, body, string(got.Head))

	script, err = buildTranscriptScript("missing")
	require.NoError(t, err)
	out, err = runLocal(context.Background(), script)
	require.NoError(t, err)
	_, err = parseTranscriptOutput(out)
	assert.ErrorIs(t, err, ErrNoTranscript)
}

// A log larger than the head and tail together arrives as exactly those
// two pieces.
func TestRunLocal_TranscriptScriptSendsHeadAndTailOfALargeLog(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	project := filepath.Join(home, ".claude", "projects", "p")
	require.NoError(t, os.MkdirAll(project, 0o700))
	size := transcriptHeadBytes + transcriptTailBytes + 100
	data := make([]byte, size)
	for i := range data {
		data[i] = byte('a' + i%26)
	}
	require.NoError(t, os.WriteFile(filepath.Join(project, "big.jsonl"), data, 0o600))

	script, err := buildTranscriptScript("big")
	require.NoError(t, err)
	out, err := runLocal(context.Background(), script)
	require.NoError(t, err)
	got, err := parseTranscriptOutput(out)
	require.NoError(t, err)
	assert.False(t, got.Whole)
	assert.Equal(t, data[:transcriptHeadBytes], got.Head)
	assert.Equal(t, data[size-transcriptTailBytes:], got.Tail)
}

func userLine(text string) string {
	return `{"type":"user","message":{"role":"user","content":` + strconv.Quote(text) + `}}`
}

func assistantLine(text string) string {
	return `{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hidden"},` +
		`{"type":"text","text":` + strconv.Quote(text) + `},` +
		`{"type":"tool_use","name":"Bash","input":{"command":"cat .env"}}]}}`
}

func lines(ls ...string) []byte {
	return []byte(strings.Join(ls, "\n") + "\n")
}

// Only the conversation's text reaches the excerpt: tool calls, tool
// results, thinking and attachments — where file contents and credentials
// sit — are dropped, and so are a subagent's messages.
func TestBuildExcerpt_KeepsOnlyConversationText(t *testing.T) {
	data := lines(
		userLine("Fix the flaky test"),
		`{"type":"attachment","attachment":{"type":"credential_org","value":"secret-attachment"}}`,
		assistantLine("I found the race."),
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"API_KEY=secret-tool-output"}]}}`,
		`{"type":"assistant","isSidechain":true,"message":{"role":"assistant",`+
			`"content":[{"type":"text","text":"subagent chatter"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Also update the docs"}]}}`,
		`not json at all`,
		`{"type":"summary","summary":"something else"}`,
	)
	excerpt, ok := buildExcerpt(transcriptData{Head: data, Whole: true})
	require.True(t, ok)

	assert.Contains(t, excerpt, "[user] Fix the flaky test")
	assert.Contains(t, excerpt, "[assistant] I found the race.")
	assert.Contains(t, excerpt, "[user] Also update the docs")
	hiddenTexts := []string{
		"secret-attachment", "secret-tool-output", "cat .env", "hidden", "subagent chatter", "something else",
	}
	for _, hidden := range hiddenTexts {
		assert.NotContains(t, excerpt, hidden)
	}
	assert.Equal(t, 1, strings.Count(excerpt, "Fix the flaky test"),
		"the first instruction is not repeated when the recent messages already hold it")
}

// A log with no message this reading understands — a changed format — is
// not summarized, rather than sent to claude as raw text.
func TestBuildExcerpt_NothingReadableIsNotSummarized(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":         nil,
		"not json":      lines("hello", "world"),
		"other shape":   lines(`{"kind":"message","role":"user","text":"hi"}`),
		"only tools":    lines(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"x"}]}}`),
		"blank text":    lines(userLine("   ")),
		"content types": lines(`{"type":"user","message":{"role":"user","content":42}}`),
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := buildExcerpt(transcriptData{Head: data, Whole: true})
			assert.False(t, ok)
		})
	}
}

// The head's last line and the tail's first line are cut mid-line by the
// byte limits and are not read; the first instruction comes from the head.
func TestBuildExcerpt_HeadAndTail(t *testing.T) {
	head := []byte(userLine("The first instruction") + "\n" + assistantLine("early work") + "\n" + `{"type":"user","mess`)
	tail := []byte(`age":"cut"}}` + "\n" + assistantLine("late work") + "\n" + userLine("the latest ask") + "\n")

	excerpt, ok := buildExcerpt(transcriptData{Head: head, Tail: tail})
	require.True(t, ok)
	assert.Contains(t, excerpt, "[user] The first instruction")
	assert.NotContains(t, excerpt, "early work", "the head supplies only the first instruction")
	assert.Contains(t, excerpt, "[assistant] late work")
	assert.Contains(t, excerpt, "[user] the latest ask")
	assert.Less(t, strings.Index(excerpt, "late work"), strings.Index(excerpt, "the latest ask"), "oldest first")
}

// A head without a user message still leaves the recent messages.
func TestBuildExcerpt_WithoutAFirstInstruction(t *testing.T) {
	excerpt, ok := buildExcerpt(transcriptData{Head: lines(assistantLine("hello")), Tail: lines("x", userLine("recent"))})
	require.True(t, ok)
	assert.NotContains(t, excerpt, excerptFirstHeading)
	assert.Contains(t, excerpt, "[user] recent")
}

// The excerpt is bounded: each message and the first instruction are cut,
// and only the newest messages up to the budget are kept.
func TestBuildExcerpt_IsBounded(t *testing.T) {
	var ls []string
	ls = append(ls, userLine(strings.Repeat("F", 3*excerptFirstBytes)))
	for i := 0; i < 100; i++ {
		ls = append(ls, assistantLine(strconv.Itoa(i)+strings.Repeat("m", 2*excerptMessageBytes)))
	}
	excerpt, ok := buildExcerpt(transcriptData{Head: lines(ls...), Whole: true})
	require.True(t, ok)

	assert.LessOrEqual(t, len(excerpt), excerptFirstBytes+excerptRecentBytes+1024)
	assert.Contains(t, excerpt, "99mmm", "the newest message is kept")
	assert.NotContains(t, excerpt, "\n[assistant] 0m", "the oldest ones are dropped")
	assert.Less(t, strings.Count(excerpt, "F"), excerptFirstBytes+1)
	assert.Less(t, strings.Count(excerpt, "m"), excerptRecentBytes)
}

func TestTruncateUTF8(t *testing.T) {
	assert.Equal(t, "abc", truncateUTF8("abc", 3))
	assert.Equal(t, "ab…", truncateUTF8("abcd", 2))
	cut := truncateUTF8(strings.Repeat("あ", 10), 5)
	assert.True(t, utf8.ValidString(cut), "never splits a character: %q", cut)
	assert.Equal(t, "あ…", cut)
}

// The lines the byte limits cut are skipped even when they happen to be a
// whole message: the head's last line and the tail's first.
func TestBuildExcerpt_SkipsTheLinesTheLimitsCut(t *testing.T) {
	head := []byte(assistantLine("early") + "\n" + userLine("head-last-line"))
	tail := []byte(userLine("tail-first-line") + "\n" + userLine("kept") + "\n")

	excerpt, ok := buildExcerpt(transcriptData{Head: head, Tail: tail})
	require.True(t, ok)
	assert.NotContains(t, excerpt, "head-last-line")
	assert.NotContains(t, excerpt, "tail-first-line")
	assert.Contains(t, excerpt, "[user] kept")
}

// A first instruction with no readable recent message is still an excerpt,
// with no empty section for the recent messages.
func TestBuildExcerpt_OnlyAFirstInstruction(t *testing.T) {
	excerpt, ok := buildExcerpt(transcriptData{Head: lines(userLine("the goal"), "cut"), Tail: lines("cut", "not json")})
	require.True(t, ok)
	assert.Contains(t, excerpt, excerptFirstHeading+"\n[user] the goal")
	assert.NotContains(t, excerpt, excerptRecentHeading)
}

// Messages that fill the budget exactly are all kept.
func TestBuildExcerpt_MessagesThatFillTheBudgetExactlyAreKept(t *testing.T) {
	// "[user] " + text + "\n\n" is 9 bytes more than the text, so twelve
	// messages of excerptRecentBytes/12-9 bytes fill the budget exactly.
	const perMessage = excerptRecentBytes/12 - 9
	var ls []string
	for i := 0; i < 13; i++ {
		ls = append(ls, userLine(fmt.Sprintf("%02d", i)+strings.Repeat("x", perMessage-2)))
	}
	excerpt, ok := buildExcerpt(transcriptData{Head: lines(ls...), Whole: true})
	require.True(t, ok)
	assert.Contains(t, excerpt, excerptRecentHeading+"\n[user] 01x", "the twelfth newest message is kept")
	assert.Contains(t, excerpt, excerptFirstHeading+"\n[user] 00x", "the oldest is the first instruction")
}

// A string that starts with continuation bytes is cut without reading
// before its start.
func TestTruncateUTF8_InvalidLeadingBytes(t *testing.T) {
	assert.Equal(t, "…", truncateUTF8("\x80\x80\x80abc", 2))
}
