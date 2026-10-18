package agent

import (
	"path/filepath"
	"slices"
	"strings"
)

// claudeAliases are the other names of Claude Code: the npm package installs
// it under a claude-code directory.
var claudeAliases = []string{claudeCodeName}

const (
	claudeCodeName = "claude-code"
	printFlag      = "--print"
	codexExec      = "exec"
)

// The arguments that make a process a headless run rather than a session.
var (
	claudeHeadless = []string{"-p", printFlag}
	codexHeadless  = []string{codexExec}
)

// codexVerified is the codex-cli release whose `codex --help` the codex
// subcommand and option tables below were taken from.
const codexVerified = "codex-cli 0.157.0"

// IsClaudeProcess reports whether a `ps` command line is a Claude Code
// process: the program itself — argv[0], or the script a node, bun or deno
// interpreter runs — has a path component named claude or claude-code. How
// Claude Code shows in `ps` depends on how it was installed: a native binary
// under .../claude/versions/<version> or named claude, or node running
// .../@anthropic-ai/claude-code/cli.js. Only the program is looked at, so a
// process that merely has a file under ~/.claude as an argument (an editor,
// `tail`, a hook) does not pass for one.
func IsClaudeProcess(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	if namesClaude(fields[0]) {
		return true
	}
	return len(fields) > 1 && scriptInterpreters[strings.ToLower(filepath.Base(fields[0]))] && namesClaude(fields[1])
}

var scriptInterpreters = map[string]bool{"node": true, "bun": true, "deno": true}

func namesClaude(program string) bool {
	for _, part := range strings.Split(strings.ToLower(program), "/") {
		if part == string(Claude) || slices.Contains(claudeAliases, part) {
			return true
		}
	}
	return false
}

// IsInteractiveClaude is a claude process that is not `claude -p` /
// `claude --print`, which answer one prompt and exit.
func IsInteractiveClaude(command string) bool {
	if !IsClaudeProcess(command) {
		return false
	}
	for _, field := range strings.Fields(command)[1:] {
		if field == "-p" || field == printFlag {
			return false
		}
	}
	return true
}

// codexNonInteractiveCommands are codex-cli's subcommands that do not open
// an interactive session, from `codex --help` of codex-cli 0.157.0, plus
// mcp-server from earlier releases. With no subcommand codex opens the TUI
// (the first positional argument is then a prompt), and `resume` and `fork`
// reopen an interactive session, so neither appears here.
var codexNonInteractiveCommands = map[string]bool{
	"agents": true, codexExec: true, "e": true, "review": true, "login": true, "logout": true,
	"mcp": true, "mcp-server": true, "plugin": true, "app-server": true, "remote-control": true,
	"completion": true, "update": true, "doctor": true, "sandbox": true, "debug": true,
	"apply": true, "a": true, "queue": true, "archive": true, "delete": true,
	"migrate-rollouts": true, "unarchive": true, "cloud": true, "exec-server": true,
	"features": true, "help": true,
}

// codexValueOptions are the top-level options of codex-cli 0.157.0 that take
// a value in the next argument, so the value is not mistaken for a subcommand.
var codexValueOptions = map[string]bool{
	"-c": true, "--config": true, "--enable": true, "--disable": true, "--remote": true,
	"--remote-auth-token-env": true, "-i": true, "--image": true, "-m": true, "--model": true,
	"--local-provider": true, "-p": true, "--profile": true, "-s": true, "--sandbox": true,
	"-C": true, "--cd": true, "--add-dir": true, "-a": true, "--ask-for-approval": true,
}

// IsInteractiveCodex reports whether a `ps` command line is an interactive
// codex session: argv[0]'s base name is codex, and the first positional
// argument is not a non-interactive subcommand. Only that argument is looked
// at, so a prompt that happens to contain "exec" is still a session.
func IsInteractiveCodex(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 || strings.ToLower(filepath.Base(fields[0])) != string(Codex) {
		return false
	}
	return !codexNonInteractiveCommands[firstCodexPositional(fields[1:])]
}

// codexMultiValueOptions take every argument up to the next option as their
// values (`-i, --image <FILE>...`; confirmed against codex-cli 0.160.0 in the
// PR #418 review), so `codex -i a.png review` is a session with an image, not
// a review. An attached value (`--image=a.png`, `-ia.png`) is read as one
// value: whether codex takes more after it is unverified.
var codexMultiValueOptions = map[string]bool{"-i": true, "--image": true}

// firstCodexPositional is codex's first positional argument — its
// subcommand, or a prompt — with the values of options skipped; "" when
// there is none or it follows "--".
func firstCodexPositional(args []string) string {
	skipValue, skipValues := false, false
	for _, arg := range args {
		switch {
		case skipValue:
			skipValue = false
		case arg == "--":
			return ""
		case strings.HasPrefix(arg, "-"):
			skipValue = codexValueOptions[arg]
			skipValues = codexMultiValueOptions[arg]
		case skipValues:
		default:
			return arg
		}
	}
	return ""
}

// IsCodexProgram is any codex process, interactive or not.
func IsCodexProgram(command string) bool {
	fields := strings.Fields(command)
	//mutation:exempt[CONDITIONALS_BOUNDARY] unreachable — parseProcessRow keeps only rows that have a command
	return len(fields) > 0 && strings.ToLower(fieldBase(fields[0])) == string(Codex)
}

func fieldBase(program string) string {
	return program[strings.LastIndex(program, "/")+1:]
}

// CodexSubcommand is the first positional argument of a codex command line —
// its subcommand, or a prompt — with option values skipped; "" when there is
// none.
func CodexSubcommand(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return firstCodexPositional(fields[1:])
}
