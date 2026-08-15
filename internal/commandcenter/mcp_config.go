package commandcenter

import (
	"encoding/json"
	"fmt"
	"os"
)

// BoardMCPServerSubcommand is the hidden panemux subcommand
// (`panemux __board-mcp-server`) that main.go recognizes before ordinary
// flag parsing. Re-invoking the panemux binary itself as the MCP server's
// command means no second listening process is ever started — the
// subprocess speaks MCP over stdio to the claude -p process that spawned
// it, exits with it, and is never itself reachable over the network. See
// docs/agent-board.md's "No new daemon, no new listening port" design
// principle.
const BoardMCPServerSubcommand = "__board-mcp-server"

// mcpServerName is this MCP server's name as it appears in the mcp-config
// file's "mcpServers" map and, per Claude Code's own naming convention, as
// the middle segment of every mcp__<server>__<tool> allowed-tool name.
const mcpServerName = "panemux-board"

// The three tool names the board MCP server exposes — thin wrappers around
// GET /api/board/status, GET /api/board/messages, and POST
// /api/board/broadcast respectively. See docs/agent-board.md's Command
// center section.
const (
	MCPToolBoardStatus    = "board_status"
	MCPToolBoardMessages  = "board_messages"
	MCPToolBoardBroadcast = "board_broadcast"
)

type mcpServerConfig struct {
	Env     map[string]string `json:"env"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
}

type mcpConfigFile struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

// BuildMCPConfig writes a temporary MCP config file (mode 0600, since its
// env block embeds a bearer token in plaintext) wiring the command center's
// `claude -p` subprocess to panemux's own narrow MCP server: a
// re-invocation of execPath with BoardMCPServerSubcommand, reading baseURL
// and token from its environment. The returned cleanup func removes the
// file; callers must call it once the `claude -p` subprocess this config
// was built for has exited, never before — the subprocess reads the file
// path only at MCP server startup, but that startup can happen any time
// during the subprocess's life.
func BuildMCPConfig(execPath, baseURL, token string) (path string, cleanup func(), err error) {
	cfg := mcpConfigFile{
		MCPServers: map[string]mcpServerConfig{
			mcpServerName: {
				Command: execPath,
				Args:    []string{BoardMCPServerSubcommand},
				Env: map[string]string{
					"PANEMUX_BOARD_BASE_URL": baseURL,
					"PANEMUX_BOARD_TOKEN":    token,
				},
			},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("encoding mcp config: %w", err)
	}

	f, err := os.CreateTemp("", "panemux-board-mcp-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp mcp config: %w", err)
	}
	tmpPath := f.Name()
	cleanup = func() { _ = os.Remove(tmpPath) }

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("writing temp mcp config: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("closing temp mcp config: %w", err)
	}
	if err := os.Chmod(tmpPath, 0600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("setting mcp config mode: %w", err)
	}
	return tmpPath, cleanup, nil
}

// AllowedTools returns the --allowedTools value scoping the command
// center's `claude -p` subprocess to exactly the three board MCP tools. The
// subprocess never receives --dangerously-skip-permissions, and this narrow
// allowlist stands in for the interactive approval prompt it has no PTY to
// surface — see docs/agent-board.md's "Permissions" subsection.
//
// Note what this does *not* mean, since an earlier revision of this comment
// claimed it: the other built-in tools are not absent. They remain in the
// subprocess's tool list and are refused at call time (a real Bash attempt
// lands in permission_denials). "Refused" is weaker than "absent", which is
// why DisallowedTools below exists.
func AllowedTools() []string {
	return []string{
		"mcp__" + mcpServerName + "__" + MCPToolBoardStatus,
		"mcp__" + mcpServerName + "__" + MCPToolBoardMessages,
		"mcp__" + mcpServerName + "__" + MCPToolBoardBroadcast,
	}
}

// DisallowedTools names the built-in tools the command center subprocess is
// explicitly denied, over and above --allowedTools listing only the three
// board tools.
//
// This second list is not belt-and-braces — it is the layer that actually
// holds. Verified against the real CLI (v2.1.233), with --allowedTools
// scoped to a single board tool:
//
//	--allowedTools alone                              -> Bash blocked
//	+ {"permissions":{"defaultMode":"acceptEdits"}}   -> Bash EXECUTED
//	+ --disallowedTools=Bash                          -> Bash blocked
//
// --allowedTools is a permission policy another policy layer can override;
// --disallowedTools survives that override. panemux does not send a
// permissions key today (see SubprocessSettings), so the middle row is not
// the shipped configuration — but it is one settings key away, and this
// list is what keeps that from being a full escape.
//
// A wildcard was tried first and rejected on evidence: --disallowedTools="*"
// removes the board MCP tools too, leaving the command center with nothing
// to call, while the model still reported file tools as available.
//
// The list is an enumeration and will drift as the CLI gains tools — a known
// weakness, accepted because it is the only argv-level denial that holds.
// Its members are the tools that can act: execute, write, reach the network,
// spawn more agents, persist work, or contact anything outside this process.
func DisallowedTools() []string {
	return []string{
		// Execution.
		"Bash", "BashOutput", "KillBash", "Monitor",
		// Filesystem.
		"Read", "Write", "Edit", "MultiEdit", "NotebookEdit", "Glob", "Grep",
		// Spawning more agents, which would not inherit this scoping.
		"Agent", "Task", "TaskCreate", "TaskStop", "TaskUpdate", "TaskOutput",
		// Network egress.
		"WebFetch", "WebSearch",
		// Reaching anything outside this process.
		"SendMessage", "SendUserFile", "PushNotification", "Artifact",
		// Persistence and scheduling that outlives the query.
		"CronCreate", "CronDelete", "CronList", "ScheduleWakeup",
		// Arbitrary packaged behavior.
		"Skill", "Workflow", "DesignSync", "EnterWorktree", "ExitWorktree",
	}
}
