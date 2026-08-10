package main

import (
	"context"
	"fmt"
	"io"

	"panemux/internal/boardmcp"
)

const (
	envBoardMCPBaseURL = "PANEMUX_BOARD_BASE_URL"
	envBoardMCPToken   = "PANEMUX_BOARD_TOKEN" //nolint:gosec // G101: an env var name, not a credential value
)

// runBoardMCPServer implements the panemux `__board-mcp-server` hidden
// subcommand: a stdio MCP server exposing the three board tools, backed by
// an HTTP client hitting panemux's own loopback REST API. See
// docs/agent-board.md's Command center section for why this re-invokes the
// panemux binary itself as the command center's MCP server rather than
// starting a second listening process — the subprocess speaks MCP over
// stdio to whichever `claude -p` process spawned it and is never itself
// reachable over the network.
func runBoardMCPServer(ctx context.Context, getenv func(string) string, stdin io.Reader, stdout io.Writer) error {
	baseURL := getenv(envBoardMCPBaseURL)
	if baseURL == "" {
		return fmt.Errorf("%s is not set", envBoardMCPBaseURL)
	}
	token := getenv(envBoardMCPToken)
	if token == "" {
		return fmt.Errorf("%s is not set", envBoardMCPToken)
	}

	client := boardmcp.NewHTTPBoardAPIClient(baseURL, token)
	if err := boardmcp.NewServer(client).Serve(ctx, stdin, stdout); err != nil {
		return fmt.Errorf("serving board mcp: %w", err)
	}
	return nil
}
