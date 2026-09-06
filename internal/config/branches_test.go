package config

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two groups here, and they fail in opposite directions.
//
// EnsureAuthToken is best-effort by design: every failure leaves the token
// empty and lets panemux start, because a server that will not boot is worse
// than one whose board routes are unreachable. That makes the log line the
// operator's only signal, so each test asserts what it says.
//
// Validate is the opposite — it exists to stop a config from being accepted,
// and a rule that never fires is a rule that is not there.

func captureConfigLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevWriter, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})
	return &buf
}

// ── EnsureAuthToken ──────────────────────────────────────────────────────────

// With no override and no home directory there is nowhere to keep the token,
// so none is minted. Leaving Server.AuthToken empty is the important half:
// a token generated but not persisted would change on every restart, silently
// invalidating the one the dashboard already holds.
func TestEnsureAuthTokenReportsAnUnresolvablePath(t *testing.T) {
	logs := captureConfigLog(t)
	t.Setenv("HOME", "")
	cfg := &Config{}

	cfg.EnsureAuthToken()

	assert.Empty(t, cfg.Server.AuthToken, "no path means no token, not a token that cannot be kept")
	assert.Contains(t, logs.String(), "failed to resolve auth token path")
}

// The directory is created first, so a write failing after that is its own
// arm and its own message. Pointing the token path at an existing directory
// reaches it: MkdirAll on the parent succeeds, the write does not.
func TestEnsureAuthTokenReportsAFailedWrite(t *testing.T) {
	logs := captureConfigLog(t)
	dir := filepath.Join(t.TempDir(), "token-is-a-directory")
	require.NoError(t, os.Mkdir(dir, 0750))
	cfg := &Config{authTokenPath: dir}

	cfg.EnsureAuthToken()

	assert.Empty(t, cfg.Server.AuthToken,
		"a token that could not be persisted must not be used for this run either")
	assert.Contains(t, logs.String(), "failed to persist auth token")
	assert.NotContains(t, logs.String(), "failed to create auth token directory",
		"the parent existed; only the write failed")
}

// The complement, so the failures above are not passing for want of a working
// path: with a usable one a token is minted, persisted at 0600, and read back
// on the next call rather than regenerated.
func TestEnsureAuthTokenMintsPersistsAndReuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "token")
	cfg := &Config{authTokenPath: path}

	cfg.EnsureAuthToken()
	minted := cfg.Server.AuthToken
	require.NotEmpty(t, minted)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(),
		"the token is a credential on disk; docs/security.md requires 0600")

	next := &Config{authTokenPath: path}
	next.EnsureAuthToken()
	assert.Equal(t, minted, next.Server.AuthToken,
		"a restart must reuse the token the dashboard already holds, not mint a new one")
}

// ── Validate ─────────────────────────────────────────────────────────────────

// Each of these rules rejects a config an operator can plausibly write by
// hand, and each is checked together with a value that must pass, so a rule
// that rejects everything is as visible as one that rejects nothing.

// Both bounds, and both of their neighbours. Only the lower one had anything
// protecting it: with `width > maxWorkspaceVerticalBarWidth` deleted the whole
// repository suite stayed green, so an arbitrarily wide sidebar was one
// condition away from being accepted. Verified before writing this, which is
// why the table is a table rather than the one rejecting value it started as.
func TestValidateRejectsAnOutOfRangeVerticalBarWidth(t *testing.T) {
	for _, tc := range []struct {
		name    string
		width   int
		wantErr bool
	}{
		{name: "far below the minimum", width: 1, wantErr: true},
		{name: "just below the minimum", width: minWorkspaceVerticalBarWidth - 1, wantErr: true},
		{name: "at the minimum", width: minWorkspaceVerticalBarWidth},
		{name: "at the maximum", width: maxWorkspaceVerticalBarWidth},
		{name: "just above the maximum", width: maxWorkspaceVerticalBarWidth + 1, wantErr: true},
		{name: "far above the maximum", width: 10000, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validatableConfig()
			cfg.Workspaces.VerticalBarWidth = tc.width

			err := cfg.Validate()

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid vertical_bar_width")
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateRejectsAnInvalidChildDirection(t *testing.T) {
	cfg := validatableConfig()
	cfg.Workspaces.Items[0].Layout.Children[0].Direction = "diagonal"

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid direction")
	assert.Contains(t, err.Error(), "diagonal", "the offending value has to be in the message")

	cfg.Workspaces.Items[0].Layout.Children[0].Direction = "vertical"
	assert.NoError(t, cfg.Validate())
}

// An ssh pane with no connection name is not a pane that connects nowhere —
// it is one that cannot be started at all, so it has to be caught at load
// rather than at first use.
func TestValidateRejectsAnSSHPaneWithNoConnection(t *testing.T) {
	cfg := validatableConfig()
	cfg.Workspaces.Items[0].Layout.Children[0].Pane.Type = "ssh"
	cfg.Workspaces.Items[0].Layout.Children[0].Pane.Connection = ""

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ssh connection name must not be empty")
}

// isLoopbackHost decides whether server.auth_token is required. A hostname
// that is not an IP at all is emphatically not loopback: treating an
// unparsable value as local would let a non-loopback bind skip the token
// requirement that docs/security.md makes the whole board's gate.
func TestIsLoopbackHostTreatsANonIPHostAsRemote(t *testing.T) {
	assert.False(t, isLoopbackHost("panemux.example.com"))
	assert.False(t, isLoopbackHost("0.0.0.0"), "a wildcard bind reaches every interface")

	assert.True(t, isLoopbackHost(""), "an omitted host defaults to loopback")
	assert.True(t, isLoopbackHost("localhost"))
	assert.True(t, isLoopbackHost("127.0.0.1"))
	assert.True(t, isLoopbackHost("::1"))
}

// validatableConfig returns the smallest config Validate accepts, so each
// test above can invalidate exactly one thing.
func validatableConfig() *Config {
	return &Config{
		Server: ServerConfig{Host: "127.0.0.1", Port: 8080},
		Workspaces: WorkspacesConfig{
			Active:      "default",
			TabPosition: "top",
			Items: []WorkspaceConfig{{
				ID:    "default",
				Title: "Default",
				Layout: LayoutNode{
					Direction: "horizontal",
					Children: []LayoutChild{
						{Size: 100, Pane: &PaneConfig{ID: "main", Type: "local"}},
					},
				},
			}},
		},
	}
}
