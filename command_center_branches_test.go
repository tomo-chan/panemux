package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/commandcenter"
	"panemux/internal/config"
)

// setupCommandCenter is additive, never load-bearing: every failure in it
// disables the command center and lets panemux start anyway. That is the
// right call — a palette nobody can open is better than a server that will
// not boot — but it means each failure is visible only as a log line, and
// these tests cover what those lines say.

func commandCenterConfig() *config.Config {
	cfg := &config.Config{}
	cfg.CommandCenter.Enabled = true
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 8080
	cfg.Server.AuthToken = "sample-token"
	return cfg
}

// Every path setupCommandCenter resolves goes through the home directory, so
// an environment without one disables the feature at the first of them. The
// log line has to name the command center: panemux keeps starting, and this
// is the operator's only sign that the palette will not be there.
func TestSetupCommandCenterReportsAnUnresolvableHomeDirectory(t *testing.T) {
	logs := captureBootstrapLog(t)
	t.Setenv("HOME", "")

	runner := setupCommandCenter(commandCenterConfig())

	assert.Nil(t, runner, "no runner means server.New skips the /ws/board-command route entirely")
	assert.Contains(t, logs.String(), "command center:",
		"the operator has to be able to tell which feature turned itself off")
	assert.Contains(t, logs.String(), "session file path")
}

// The MCP config is built per query rather than at setup, because it embeds
// the bearer token in a temp file that must not outlive the subprocess that
// reads it (docs/security.md). Nothing had exercised that closure, so this
// runs one query far enough to prove it succeeds — the query then fails at
// the missing claude binary, which is the point: the failure reported must
// be the exec, not the config.
func TestSetupCommandCenterBuildsItsMCPConfigPerQuery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// An empty PATH so the `claude` the runner tries to exec is certainly
	// absent, whatever the host has installed.
	t.Setenv("PATH", t.TempDir())

	runner := setupCommandCenter(commandCenterConfig())
	require.NotNil(t, runner)

	events, err := runner.Query(context.Background(), "how is the board")
	require.NoError(t, err)

	var errs []string
	for ev := range events {
		if ev.Type == commandcenter.EventError {
			errs = append(errs, ev.Err)
		}
	}

	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "starting claude",
		"reaching the exec is what proves the mcp config closure ran and succeeded")
	assert.NotContains(t, errs[0], "building mcp config")
	assert.NotContains(t, errs[0], "resolving panemux executable path")

	// Nothing was persisted: the session id is written only after a turn
	// succeeds, so a failed exec must leave the isolated home untouched
	// rather than recording a conversation that never happened.
	assert.NoFileExists(t, filepath.Join(home, ".config", "panemux", "command-center-session.json"))
}
