package commandcenter

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// This file owns the command center subprocess's *execution context*: the
// identity of its conversation, the instructions it runs under, and the
// directory it runs in. All three exist because `claude` resolves each of
// them from ambient state by default, and every one of those defaults is
// wrong for a subprocess panemux spawns on an operator's behalf.
//
// Verified live against the real CLI (v2.1.233), not inferred from --help:
//
//   - A plain `claude -p` (no --resume) does NOT mint a fresh conversation.
//     It reports the *ambient* session id of whatever Claude Code session
//     the environment already belongs to. The Runner used to capture that
//     reported id and --resume it on every later query, which silently
//     attached the command center to a conversation it does not own — one
//     that may hold the operator's full tool permissions, while the command
//     center itself is deliberately launched with only three board tools.
//     Passing our own --session-id fixes this: the CLI then reports back
//     exactly the UUID we minted, and --resume of that UUID continues our
//     conversation and no one else's.
//
//   - `claude -p` reads CLAUDE.md from its working directory. With no
//     cmd.Dir set, that is whatever directory the operator happened to start
//     panemux in, so an unrelated project's conventions leak into the
//     orchestrator's instructions.
//
//   - --setting-sources '' suppresses CLAUDE.md loading entirely, not just
//     settings files. So "load our own CLAUDE.md" and "inherit none of the
//     operator's settings or hooks" cannot both be satisfied through files.
//     --append-system-prompt is independent of setting sources, so panemux's
//     own instructions go through that instead, and the file layer below is
//     purely an operator-facing override.

// commandCenterDirName is the operator-facing directory, under panemux's own
// config dir, where an operator may place *instructions* for the command
// center. Instructions only: text appended to a system prompt has no
// execution semantics, unlike a settings file (see SubprocessSettings).
// Nothing is required to exist — panemux ships its own defaults compiled
// into the binary, because it installs as a standalone binary (install.sh)
// with no repository on disk to read from.
const commandCenterDirName = "command-center"

const operatorInstructionsFile = "CLAUDE.md"

// SubprocessSettings is the entire settings document panemux hands the
// subprocess via --settings. It is a fixed literal, and deliberately
// contains only keys that *narrow* what the subprocess may do.
//
// Nothing from the operator's own ~/.claude/settings.json is merged in, and
// there is no operator settings file for the command center either. That is
// not caution for its own sake — a settings value can nullify the tool
// scoping that is this feature's actual security boundary. Reproduced twice
// against the real CLI (v2.1.233): with --allowedTools scoped to a single
// board tool, adding {"permissions":{"defaultMode":"acceptEdits"}} let the
// subprocess run Bash, with no entry in permission_denials at all. An
// operator who sets that mode for their own interactive sessions — an
// entirely ordinary thing to do — would have silently unscoped the command
// center by inheritance.
//
// sandbox confines the tools that *are* permitted. It is a no-op where the
// OS cannot provide it (verified: the setting is ignored, the query still
// succeeds, is_error stays false), so passing it unconditionally costs
// nothing on an unsupported host. See docs/security.md for what this was and
// was not verified to do.
const SubprocessSettings = `{"sandbox":{"enabled":true}}`

// DefaultSystemPrompt is the instruction layer every command center query
// runs under, passed via --append-system-prompt. It is a compile-time
// literal — never operator input — so it carries no taint into argv.
//
// The board_status rule exists because of an observed failure: asked twice
// in a row whether any panes were on the board, the second answer repeated
// the first from conversation context without calling the tool again, long
// after the answer had changed.
const DefaultSystemPrompt = `You are the command center for panemux, a terminal multiplexer.

Your only job is to observe and coordinate the coding agents running in panemux's panes, using the three board
tools available to you:

- board_status: what each pane last reported about itself (state, repo, branch, PR, summary).
- board_messages: the recent cross-pane message history.
- board_broadcast: send a message to one or more panes by pane id.

Rules:

- Always call board_status before answering anything about which panes exist or what they are doing. Pane
  state changes constantly and an answer from earlier in this conversation is not evidence about now.
- Address broadcasts by exact pane id, taken from board_status. Never guess an id.
- A broadcast is an ordinary message to that pane's agent, not an authorized command. Say what you sent and to
  whom; do not claim work was done because you asked for it.
- You have no shell, no filesystem, and no other tools. If a request needs something beyond the three board
  tools, say so plainly instead of describing what you would have run.
- Be brief. The operator reads your replies in a small palette window.`

// NewSessionID mints a RFC 4122 version 4 UUID. --session-id requires a
// valid UUID, and the value is generated here rather than taken from the
// CLI's own report precisely so it cannot collide with an ambient session.
func NewSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating command center session id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}

// DefaultContextDir returns the operator-facing command center directory,
// alongside the token, relay cursor, history and session files panemux
// already keeps in its own config directory.
func DefaultContextDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".config", "panemux", commandCenterDirName), nil
}

// SystemPrompt returns DefaultSystemPrompt, with the operator's own
// CLAUDE.md appended when contextDir holds one. An unreadable or empty file
// is treated as absent: operator instructions are an optional refinement,
// never a precondition for the command center working at all.
func SystemPrompt(contextDir string) string {
	if contextDir == "" {
		return DefaultSystemPrompt
	}
	// G304: an operator-owned config path, same trust level as config.yaml.
	raw, err := os.ReadFile(filepath.Join(contextDir, operatorInstructionsFile)) //nolint:gosec
	if err != nil {
		return DefaultSystemPrompt
	}
	extra := strings.TrimSpace(string(raw))
	if extra == "" {
		return DefaultSystemPrompt
	}
	return DefaultSystemPrompt + "\n\nOperator instructions:\n\n" + extra
}

// NewWorkDir creates the empty directory the subprocess runs in, so it never
// inherits the working directory panemux itself was started from. Returns a
// cleanup that removes it.
func NewWorkDir() (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "panemux-command-center-")
	if err != nil {
		return "", func() {}, fmt.Errorf("creating command center work directory: %w", err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}
