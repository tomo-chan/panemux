package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingWriter fails every write, standing in for the stdout pipe closing
// because the `claude -p` process that spawned this MCP server exited.
type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

// The MCP server speaks over the stdio of a subprocess panemux does not own,
// so its transport failing is ordinary rather than exceptional. It has to be
// reported with context: runBoardMCPServer's error is what `panemux
// __board-mcp-server` exits with, and a bare "broken pipe" says nothing
// about which of panemux's own subcommands produced it.
func TestRunBoardMCPServerWrapsAServeFailure(t *testing.T) {
	env := map[string]string{
		envBoardMCPBaseURL: "http://127.0.0.1:8080",
		envBoardMCPToken:   "sample-token",
	}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")

	err := runBoardMCPServer(
		context.Background(),
		func(k string) string { return env[k] },
		in,
		failingWriter{err: io.ErrClosedPipe},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "serving board mcp",
		"the operator sees this as the subcommand's exit error; it has to name what was serving")
	assert.Contains(t, err.Error(), io.ErrClosedPipe.Error())
}
