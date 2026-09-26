package tasks

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func joinLines(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

func TestParseCollectOutput_ReadsEverySection(t *testing.T) {
	out := joinLines(
		"::panemux-tasks v1",
		"::now 1790346631",
		"::section state",
		"::file 121.json",
		`{"pid":121,"sessionId":"5d0b91c2","cwd":"/workspace/user/project","status":"busy"}`,
		"",
		"::file 200.json",
		"{",
		`  "pid": 200`,
		"}",
		"",
		"::section ps",
		"    1     0 /sbin/init",
		"  121    89 /usr/local/bin/claude --verbose",
		"::section tmux",
		"89 task-7c21",
		"90 name with spaces",
		"::section cwd",
		"300 /workspace/user/sample api",
		"::section transcripts",
		"1790346000\t5d0b91c2.jsonl\t\"cwd\":\"/workspace/user/project\"\t2048",
		"1790340000\told-session.jsonl\t\t17",
		"::end",
	)

	raw, err := parseCollectOutput(out)
	require.NoError(t, err)

	assert.Equal(t, int64(1790346631), raw.Now)
	require.Len(t, raw.StateFiles, 2)
	assert.Equal(t, "121.json", raw.StateFiles[0].Name)
	assert.JSONEq(t, `{"pid":121,"sessionId":"5d0b91c2","cwd":"/workspace/user/project","status":"busy"}`,
		string(raw.StateFiles[0].Data))
	assert.JSONEq(t, `{"pid":200}`, string(raw.StateFiles[1].Data), "a multi-line file is kept whole")
	assert.Equal(t, []process{
		{PID: 1, PPID: 0, Command: "/sbin/init"},
		{PID: 121, PPID: 89, Command: "/usr/local/bin/claude --verbose"},
	}, raw.Processes)
	assert.Equal(t, []tmuxPane{
		{PanePID: 89, Session: "task-7c21"},
		{PanePID: 90, Session: "name with spaces"},
	}, raw.TmuxPanes)
	assert.Equal(t, map[int]string{300: "/workspace/user/sample api"}, raw.ProcessCWDs)
	assert.Equal(t, []transcript{
		{ModTime: 1790346000, SessionID: "5d0b91c2", CWD: "/workspace/user/project", Size: 2048},
		{ModTime: 1790340000, SessionID: "old-session", CWD: "", Size: 17},
	}, raw.Transcripts)
}

func TestParseCollectOutput_BlankLinesBetweenStateFilesAreIgnored(t *testing.T) {
	raw, err := parseCollectOutput(joinLines(
		"::panemux-tasks v1",
		"::now 5",
		"::section state",
		"",
		"::file 1.json",
		`{"pid":1}`,
		"",
		"::end",
	))
	require.NoError(t, err)
	require.Len(t, raw.StateFiles, 1)
	assert.JSONEq(t, `{"pid":1}`, string(raw.StateFiles[0].Data))
}

func TestParseCollectOutput_EmptySectionsAreFine(t *testing.T) {
	raw, err := parseCollectOutput(joinLines(
		"::panemux-tasks v1",
		"::now 5",
		"::section state",
		"::section ps",
		"::section tmux",
		"::section cwd",
		"::section transcripts",
		"::end",
	))
	require.NoError(t, err)
	assert.Equal(t, int64(5), raw.Now)
	assert.Empty(t, raw.StateFiles)
	assert.Empty(t, raw.Processes)
	assert.Empty(t, raw.TmuxPanes)
	assert.Empty(t, raw.ProcessCWDs)
	assert.Empty(t, raw.Transcripts)
}

func TestParseCollectOutput_SkipsMalformedRows(t *testing.T) {
	raw, err := parseCollectOutput(joinLines(
		"::panemux-tasks v1",
		"::now 5",
		"::section ps",
		"not-a-pid 1 cmd",
		"12 not-a-ppid cmd",
		"13",
		"0 0 kernel_task",
		"14 1 ok",
		"::section tmux",
		"abc session",
		"15",
		"0 zero",
		"16 good",
		"::section cwd",
		"x /path",
		"17",
		"18 /ok",
		"::section transcripts",
		"no tab at all",
		"bad\tname.jsonl\t",
		"1\tno-suffix\t",
		"2\tbad id!.jsonl\t",
		"3\tonly-two-fields.jsonl",
		"4\tquoted.jsonl\t\"cwd\":\"/a\\\"b\"",
		"5\tbroken.jsonl\t\"cwd\":\"/unterminated",
		"6\tbad-size.jsonl\t\"cwd\":\"/s\"\tlots",
		"7\tnegative-size.jsonl\t\t-1",
		"::end",
	))
	require.NoError(t, err)
	assert.Equal(t, []process{{PID: 14, PPID: 1, Command: "ok"}}, raw.Processes)
	assert.Equal(t, []tmuxPane{{PanePID: 16, Session: "good"}}, raw.TmuxPanes)
	assert.Equal(t, map[int]string{18: "/ok"}, raw.ProcessCWDs)
	assert.Equal(t, []transcript{
		{ModTime: 3, SessionID: "only-two-fields"},
		{ModTime: 4, SessionID: "quoted", CWD: `/a"b`},
		{ModTime: 5, SessionID: "broken"},
		// A size that is not a count is unknown (0); the session is still listed.
		{ModTime: 6, SessionID: "bad-size", CWD: "/s"},
		{ModTime: 7, SessionID: "negative-size"},
	}, raw.Transcripts)
}

func TestParseCollectOutput_RejectsIncompleteOutput(t *testing.T) {
	cases := map[string][]byte{
		"empty":             nil,
		"no header":         joinLines("::now 5", "::end"),
		"wrong version":     joinLines("::panemux-tasks v2", "::now 5", "::end"),
		"truncated":         joinLines("::panemux-tasks v1", "::now 5", "::section ps", "1 0 init"),
		"missing clock":     joinLines("::panemux-tasks v1", "::end"),
		"unparseable clock": joinLines("::panemux-tasks v1", "::now soon", "::end"),
		"unknown section":   joinLines("::panemux-tasks v1", "::now 5", "::section other", "::end"),
		"file outside state": joinLines(
			"::panemux-tasks v1", "::now 5", "::section ps", "::file 1.json", "::end"),
		"unknown directive": joinLines("::panemux-tasks v1", "::now 5", "::what", "::end"),
		"row before section": joinLines(
			"::panemux-tasks v1", "::now 5", "1 0 init", "::end"),
		"state row before file": joinLines(
			"::panemux-tasks v1", "::now 5", "::section state", "{}", "::end"),
	}
	for name, out := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseCollectOutput(out)
			assert.Error(t, err)
		})
	}
}

// A remote login shell can print before the script's own output starts: bash
// sources ~/.bashrc even for a non-interactive ssh command, and a greeting
// there lands on stdout ahead of the header.
func TestParseCollectOutput_SkipsTextBeforeTheHeader(t *testing.T) {
	raw, err := parseCollectOutput(joinLines(
		"Welcome to the build host",
		"::now 1",
		"::panemux-tasks v1",
		"::now 9",
		"::end",
	))
	require.NoError(t, err)
	assert.Equal(t, int64(9), raw.Now)
}

func TestParseCollectOutput_IgnoresCarriageReturnsAndTrailingText(t *testing.T) {
	raw, err := parseCollectOutput([]byte(
		"::panemux-tasks v1\r\n::now 7\r\n::section ps\r\n1 0 init\r\n::end\r\nignored after end\n"))
	require.NoError(t, err)
	assert.Equal(t, int64(7), raw.Now)
	assert.Equal(t, []process{{PID: 1, PPID: 0, Command: "init"}}, raw.Processes)
}
