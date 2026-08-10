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
// center's `claude -p` subprocess to exactly the three board MCP tools —
// no Bash, no filesystem tools, no other MCP server. See
// docs/agent-board.md's "Permissions" subsection: the subprocess never
// receives --dangerously-skip-permissions, and this narrow allowlist is
// what stands in for the interactive approval prompt it has no PTY to
// surface.
func AllowedTools() []string {
	return []string{
		"mcp__" + mcpServerName + "__" + MCPToolBoardStatus,
		"mcp__" + mcpServerName + "__" + MCPToolBoardMessages,
		"mcp__" + mcpServerName + "__" + MCPToolBoardBroadcast,
	}
}
