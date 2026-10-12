package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessRuleMatch(t *testing.T) {
	rule := ProcessRule{Patterns: []string{"claude", "claude-*"}, ExcludeTokens: []string{"-p", "--print"}}
	cases := map[string]struct{ binary, match bool }{
		"":                                  {false, false},
		"   ":                               {false, false},
		"claude":                            {true, true},
		"/usr/local/bin/CLAUDE --resume x":  {true, true},
		"claude-nightly":                    {true, true},
		"claude -p hi":                      {true, false},
		"claude chat --print":               {true, false},
		"claude hello-p":                    {true, true},
		"claudex":                           {false, false},
		"xclaude":                           {false, false},
		"node /opt/claude/cli.js":           {false, false},
		"bash -c claude":                    {false, false},
		"/home/u/.local/bin/claude-code -p": {true, false},
	}
	for command, want := range cases {
		assert.Equal(t, want.binary, rule.MatchBinary(command), "MatchBinary(%q)", command)
		assert.Equal(t, want.match, rule.Match(command), "Match(%q)", command)
	}
}

func TestMatchBoardProcess(t *testing.T) {
	cases := map[string]string{
		"claude":                  "claude-code",
		"claude-code":             "claude-code",
		"claude-beta --model x":   "claude-code",
		"codex":                   "codex",
		"codex-nightly":           "codex",
		"cursor-agent":            "cursor",
		"cursor-agent-1":          "cursor",
		"gemini":                  "gemini",
		"gemini-2":                "gemini",
		"/usr/bin/grok":           "grok-build",
		"grok-build":              "grok-build",
		"opencode":                "opencode",
		"opencode-dev":            "opencode",
		"claude -p hi":            "",
		"claude --print":          "",
		"codex exec ls":           "",
		"cursor":                  "",
		"python worker.py":        "",
		"":                        "",
		"node /opt/claude/cli.js": "",
		"devin":                   "",
	}
	for command, want := range cases {
		d, ok := MatchBoardProcess(command)
		assert.Equal(t, want != "", ok, command)
		assert.Equal(t, want, d.AgmsgType, command)
	}
}

func TestMatchWorkdirProcess(t *testing.T) {
	cases := map[string]ID{
		"codex":                           Codex,
		"/usr/local/bin/codex --model g5": Codex,
		"claude":                          Claude,
		"codex exec":                      "",
		"claude -p":                       "",
		"claude --print":                  "",
		"claude-code":                     "",
		"codex-nightly":                   "",
		"gemini":                          "",
		"python worker.py":                "",
		"":                                "",
	}
	for command, want := range cases {
		d, ok := MatchWorkdirProcess(command)
		assert.Equal(t, want != "", ok, command)
		assert.Equal(t, want, d.ID, command)
	}
}

func TestIsClaudeProcess(t *testing.T) {
	cases := map[string]bool{
		"claude": true,
		"/home/u/.local/share/claude/versions/2.1.3":                  true,
		"node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js": true,
		"BUN /x/claude-code/cli.js":                                   true,
		"deno run claude":                                             false,
		"deno claude":                                                 true,
		"python claude":                                               false,
		"vim /home/u/.claude/settings.json":                           false,
		"tail -f /x/claude":                                           false,
		"node":                                                        false,
		"":                                                            false,
		"/opt/Claude-Code/bin/x":                                      true,
	}
	for command, want := range cases {
		assert.Equal(t, want, IsClaudeProcess(command), command)
	}
}

func TestIsInteractiveClaude(t *testing.T) {
	cases := map[string]bool{
		"claude":                       true,
		"claude --resume abc":          true,
		"claude -p hi":                 false,
		"claude --print":               false,
		"node x/claude-code/cli.js -p": false,
		"vim -p":                       false,
		"":                             false,
	}
	for command, want := range cases {
		assert.Equal(t, want, IsInteractiveClaude(command), command)
	}
}

func TestCodexCommands(t *testing.T) {
	cases := []struct {
		command, subcommand  string
		program, interactive bool
	}{
		{"codex", "", true, true},
		{"/usr/local/bin/Codex --model gpt-5", "", true, true},
		{"codex fix the exec bug", "fix", true, true},
		{"codex -m exec", "", true, true},
		{"codex --profile review resume", "resume", true, true},
		{"codex resume abc", "resume", true, true},
		{"codex fork abc", "fork", true, true},
		{"codex exec ls", "exec", true, false},
		{"codex e ls", "e", true, false},
		{"codex -c k=v app-server", "app-server", true, false},
		{"codex mcp-server", "mcp-server", true, false},
		{"codex --full-auto exec", "exec", true, false},
		{"codex -- exec", "", true, true},
		{"codex brand-new-subcommand", "brand-new-subcommand", true, true},
		{"codex-nightly", "", false, false},
		{"python codex", "", false, false},
		{"", "", false, false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.program, IsCodexProgram(tc.command), "program %q", tc.command)
		assert.Equal(t, tc.interactive, IsInteractiveCodex(tc.command), "interactive %q", tc.command)
		if tc.program {
			assert.Equal(t, tc.subcommand, CodexSubcommand(tc.command), "subcommand %q", tc.command)
		}
	}
}

// The task rules registered for claude and codex are the predicates above,
// so the task board and the registry cannot disagree.
func TestTaskRules(t *testing.T) {
	claude, ok := Lookup(Claude)
	require.True(t, ok)
	require.NotNil(t, claude.Detection.Task)
	assert.True(t, claude.Detection.Task.Process("claude -p"))
	assert.False(t, claude.Detection.Task.Interactive("claude -p"))
	assert.True(t, claude.Detection.Task.Interactive("claude"))

	codex, ok := Lookup(Codex)
	require.True(t, ok)
	require.NotNil(t, codex.Detection.Task)
	assert.True(t, codex.Detection.Task.Process("codex exec"))
	assert.False(t, codex.Detection.Task.Interactive("codex exec"))
	assert.True(t, codex.Detection.Task.Interactive("codex"))

	for _, id := range []ID{Cursor, Gemini, GrokBuild, OpenCode} {
		d, _ := Lookup(id)
		assert.Nil(t, d.Detection.Task, id)
		assert.Nil(t, d.Detection.Workdir, id)
	}
}

// Every rule records what it does with arguments it does not know, so the
// forward-compatibility stance is declared rather than implied.
func TestRegisteredUnknownArgsPolicies(t *testing.T) {
	for _, d := range All() {
		for _, r := range []*ProcessRule{d.Detection.Board, d.Detection.Workdir} {
			if r != nil {
				assert.Equal(t, Accept, r.UnknownArgs, d.ID)
			}
		}
		if d.Detection.Task != nil {
			assert.Equal(t, Accept, d.Detection.Task.UnknownArgs, d.ID)
		}
		if d.Detection.Board != nil {
			assert.Equal(t, "agmsg v1.1.13", d.Detection.Board.VerifiedWith, d.ID)
		}
	}
	codex, _ := Lookup(Codex)
	assert.Equal(t, "codex-cli 0.157.0", codex.Detection.Task.VerifiedWith)
}

func TestSessionKey(t *testing.T) {
	cases := []struct {
		key   SessionKey
		valid bool
	}{
		{SessionKey{Agent: Claude, SessionID: "abc"}, true},
		{SessionKey{Host: "dev", Agent: Codex, SessionID: "abc"}, true},
		{SessionKey{Host: "dev", Agent: "devin", SessionID: "abc"}, true},
		{SessionKey{Agent: Claude}, false},
		{SessionKey{SessionID: "abc"}, false},
		{SessionKey{Agent: "Claude", SessionID: "abc"}, false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.valid, tc.key.Valid(), "%+v", tc.key)
	}

	local := SessionKey{Agent: Claude, SessionID: "s"}
	remote := SessionKey{Host: "dev", Agent: Claude, SessionID: "s"}
	other := SessionKey{Agent: Codex, SessionID: "s"}
	seen := map[SessionKey]bool{local: true}
	assert.True(t, seen[SessionKey{Agent: Claude, SessionID: "s"}])
	assert.False(t, seen[remote], "the same session ID on another host is another session")
	assert.False(t, seen[other], "the same session ID under another agent is another session")
}
