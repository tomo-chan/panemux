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

	"panemux/internal/commandcenter"
)

const summarySessionID = "0f0e0d0c-0b0a-4908-8706-050403020100"

// The argv is the command center's hardened shape: no settings, hooks or
// MCP servers of the operator's, no slash commands, every acting tool denied
// by name, a session ID panemux minted and not persisted, and the fixed
// instruction as the only prompt, after "--". The conversation goes on stdin.
func TestSummaryArgs(t *testing.T) {
	args := summaryArgs(summarySessionID)

	assert.Equal(t, "-p", args[0])
	assert.Equal(t, []string{"--", summaryInstruction}, args[len(args)-2:], "the instruction is last, after --")
	assert.Contains(t, args, "--no-session-persistence")
	assert.Contains(t, args, "--strict-mcp-config")
	assert.Contains(t, args, "--disable-slash-commands")
	assert.Contains(t, args, "--output-format=json")
	assert.Contains(t, args, "--disallowedTools="+strings.Join(commandcenter.DisallowedTools(), ","))
	assertFollowedBy(t, args, "--setting-sources", "")
	assertFollowedBy(t, args, "--session-id", summarySessionID)
	assertFollowedBy(t, args, "--json-schema", summarySchema)

	for _, arg := range args[:len(args)-2] {
		assert.False(t, strings.HasPrefix(arg, "--allowedTools"), "no tool is allowed: %q", arg)
		assert.NotEqual(t, "--settings", arg, "no settings are passed")
		assert.NotEqual(t, "--resume", arg)
	}
}

func assertFollowedBy(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i, arg := range args[:len(args)-1] {
		if arg == flag {
			assert.Equal(t, value, args[i+1], "value of %s", flag)
			return
		}
	}
	assert.Failf(t, "flag missing", "%s in %q", flag, args)
}

func TestParseSummaryOutput(t *testing.T) {
	out := `{"type":"result","is_error":false,"structured_output":` +
		`{"summary":"  Fixed the race.  ","remaining":[" Run make check ","","Update docs"]}}`
	got, err := parseSummaryOutput([]byte(out))
	require.NoError(t, err)
	assert.Equal(t, Summary{Text: "Fixed the race.", Remaining: []string{"Run make check", "Update docs"}}, got)
}

func TestParseSummaryOutput_NothingRemaining(t *testing.T) {
	got, err := parseSummaryOutput([]byte(`{"is_error":false,"structured_output":{"summary":"Done.","remaining":[]}}`))
	require.NoError(t, err)
	assert.Equal(t, Summary{Text: "Done.", Remaining: []string{}}, got)
}

func TestParseSummaryOutput_IsBounded(t *testing.T) {
	items := make([]string, 0, maxSummaryRemaining+5)
	for range maxSummaryRemaining + 5 {
		items = append(items, `"`+strings.Repeat("r", 2*maxSummaryItemBytes)+`"`)
	}
	out := `{"is_error":false,"structured_output":{"summary":"` + strings.Repeat("s", 2*maxSummaryTextBytes) +
		`","remaining":[` + strings.Join(items, ",") + `]}}`
	got, err := parseSummaryOutput([]byte(out))
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got.Text), maxSummaryTextBytes+len("…"))
	assert.Len(t, got.Remaining, maxSummaryRemaining)
	assert.LessOrEqual(t, len(got.Remaining[0]), maxSummaryItemBytes+len("…"))
}

// An error from claude is reported with a fixed message; what claude
// printed — which can quote the conversation — is not passed on.
func TestParseSummaryOutput_Errors(t *testing.T) {
	for name, out := range map[string]string{
		"not json":     "Invalid API key",
		"is_error":     `{"is_error":true,"subtype":"error_during_execution","result":"quoted secret"}`,
		"no structure": `{"is_error":false,"result":"quoted secret"}`,
		"no summary":   `{"is_error":false,"structured_output":{"summary":"  ","remaining":[]}}`,
		"wrong types":  `{"is_error":false,"structured_output":{"summary":1,"remaining":"x"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseSummaryOutput([]byte(out))
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "secret")
			assert.NotContains(t, err.Error(), "API key")
		})
	}
}

// fakeClaude writes a stand-in for claude that records its argv, working
// directory and stdin, then prints out.
func fakeClaude(t *testing.T, out string, exit int) (bin, record string) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "record")
	bin = filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		"{ pwd; for a in \"$@\"; do printf 'ARG:%s\\n' \"$a\"; done; echo STDIN:; cat; } >'" + record + "'\n" +
		"cat <<'EOF'\n" + out + "\nEOF\n" +
		"exit " + strconv.Itoa(exit) + "\n"
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o700)) //nolint:gosec // test fixture: an executable stand-in
	return bin, record
}

func TestClaudeSummarizer_RunsClaudeWithTheExcerptOnStdin(t *testing.T) {
	bin, record := fakeClaude(t, `{"is_error":false,"structured_output":{"summary":"S","remaining":["a"]}}`, 0)
	summarize := newClaudeSummarizer(bin, strings.NewReader(strings.Repeat("\x01", 16)))

	got, err := summarize(context.Background(), "[user] hello")
	require.NoError(t, err)
	assert.Equal(t, Summary{Text: "S", Remaining: []string{"a"}}, got)

	data, err := os.ReadFile(record)
	require.NoError(t, err)
	text := string(data)
	dir, _, _ := strings.Cut(text, "\n")
	assert.Contains(t, filepath.Base(dir), "panemux-summary-", "claude runs in an empty directory of its own")
	_, statErr := os.Stat(dir) //nolint:gosec // test: the path the stand-in recorded
	assert.True(t, os.IsNotExist(statErr), "the directory is removed afterwards")
	assert.Contains(t, text, "ARG:--session-id\nARG:01010101-0101-4101-8101-010101010101\n", "a minted v4 UUID")
	assert.True(t, strings.HasSuffix(text, "STDIN:\n[user] hello"), "the excerpt is claude's stdin: %q", text)
}

func TestClaudeSummarizer_Failures(t *testing.T) {
	t.Run("exit status", func(t *testing.T) {
		bin, _ := fakeClaude(t, "Not logged in", 1)
		_, err := newClaudeSummarizer(bin, nil)(context.Background(), "x")
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "Not logged in")
	})
	t.Run("missing binary", func(t *testing.T) {
		_, err := newClaudeSummarizer(filepath.Join(t.TempDir(), "claude"), nil)(context.Background(), "x")
		require.Error(t, err)
	})
	t.Run("context ends", func(t *testing.T) {
		dir := t.TempDir()
		bin := filepath.Join(dir, "claude")
		// A child keeps stdout open, as a real claude's own children could.
		script := []byte("#!/bin/sh\nsleep 30 &\nsleep 30\n")
		require.NoError(t, os.WriteFile(bin, script, 0o700)) //nolint:gosec // test fixture: an executable stand-in
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		started := time.Now()
		_, err := newClaudeSummarizer(bin, nil)(ctx, "x")
		require.Error(t, err)
		assert.Less(t, time.Since(started), 5*time.Second)
	})
}
