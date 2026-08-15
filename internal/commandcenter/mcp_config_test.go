package commandcenter

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMCPConfigWritesExpectedShape(t *testing.T) {
	path, cleanup, err := BuildMCPConfig("/usr/local/bin/panemux", "http://127.0.0.1:8080", "sekret-token")
	require.NoError(t, err)
	defer cleanup()

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var parsed mcpConfigFile
	require.NoError(t, json.Unmarshal(data, &parsed))

	server, ok := parsed.MCPServers["panemux-board"]
	require.True(t, ok, "expected a panemux-board mcp server entry")
	assert.Equal(t, "/usr/local/bin/panemux", server.Command)
	assert.Equal(t, []string{BoardMCPServerSubcommand}, server.Args)
	assert.Equal(t, "http://127.0.0.1:8080", server.Env["PANEMUX_BOARD_BASE_URL"])
	assert.Equal(t, "sekret-token", server.Env["PANEMUX_BOARD_TOKEN"])
}

func TestBuildMCPConfigCleanupRemovesFile(t *testing.T) {
	path, cleanup, err := BuildMCPConfig("/usr/local/bin/panemux", "http://127.0.0.1:8080", "tok")
	require.NoError(t, err)

	cleanup()

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestBuildMCPConfigEachCallGetsDistinctPath(t *testing.T) {
	path1, cleanup1, err := BuildMCPConfig("/bin/panemux", "http://127.0.0.1:8080", "a")
	require.NoError(t, err)
	defer cleanup1()

	path2, cleanup2, err := BuildMCPConfig("/bin/panemux", "http://127.0.0.1:8080", "b")
	require.NoError(t, err)
	defer cleanup2()

	assert.NotEqual(t, path1, path2)
}

func TestAllowedToolsScopedToBoardToolsOnly(t *testing.T) {
	tools := AllowedTools()

	assert.Equal(t, []string{
		"mcp__panemux-board__board_status",
		"mcp__panemux-board__board_messages",
		"mcp__panemux-board__board_broadcast",
	}, tools)
}

// TestDisallowedToolsCoversActingTools guards the list that is the command
// center's only argv-level denial that survives a permissions override. See
// DisallowedTools for the three-row experiment against the real CLI.
func TestDisallowedToolsCoversActingTools(t *testing.T) {
	deny := DisallowedTools()

	for _, tool := range []string{"Bash", "Write", "Edit", "Read", "Agent", "Task", "WebFetch", "Monitor"} {
		assert.Contains(t, deny, tool, "%s can act outside the board and must be denied by name", tool)
	}

	// A wildcard was tried and rejected on evidence: it removes the board
	// MCP tools too, leaving the command center with nothing to call.
	assert.NotContains(t, deny, "*", "a wildcard denial also removes the board tools")

	for _, allowed := range AllowedTools() {
		assert.NotContains(t, deny, allowed, "the board tools must never be denied")
	}
}
