package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const validConfigYAML = `
server:
  port: 8080
  host: "127.0.0.1"
layout:
  direction: horizontal
  children:
    - size: 100
      pane:
        id: main
        type: local
`

// loadWithAuthTokenPath loads content the same way Load(path) would, but
// lets the caller pre-set authTokenPath (impossible through the real Load
// function, which always constructs a fresh, override-less Config
// internally). It also calls EnsureAuthToken explicitly, mirroring what
// main.go's real startup path does after Load succeeds — finishLoad itself
// deliberately never calls EnsureAuthToken; see finishLoad's own comment.
func loadWithAuthTokenPath(t *testing.T, content, tokenPath string) (*Config, error) {
	t.Helper()
	f := writeTempFile(t, content)
	data, err := os.ReadFile(f)
	require.NoError(t, err)

	var cfg Config
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	cfg.filePath = f
	cfg.authTokenPath = tokenPath

	if err := cfg.finishLoad(); err != nil {
		return nil, err
	}
	cfg.EnsureAuthToken()
	return &cfg, nil
}

func TestEnsureAuthToken_GeneratesAndPersists_WhenAbsent(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "nested", "token")

	cfg, err := loadWithAuthTokenPath(t, validConfigYAML, tokenPath)
	require.NoError(t, err)

	require.NotEmpty(t, cfg.Server.AuthToken)
	_, decodeErr := hex.DecodeString(cfg.Server.AuthToken)
	assert.NoError(t, decodeErr, "generated token should be hex-encoded")

	info, statErr := os.Stat(tokenPath)
	require.NoError(t, statErr, "token file should have been created")
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	persisted, readErr := os.ReadFile(tokenPath)
	require.NoError(t, readErr)
	assert.Equal(t, cfg.Server.AuthToken, strings.TrimSpace(string(persisted)))
}

func TestEnsureAuthToken_ReadsExisting_WhenPresent(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("existing-token-value\n"), 0600))

	cfg, err := loadWithAuthTokenPath(t, validConfigYAML, tokenPath)
	require.NoError(t, err)
	assert.Equal(t, "existing-token-value", cfg.Server.AuthToken)

	// Loading again must not regenerate/overwrite the existing token.
	cfg2, err := loadWithAuthTokenPath(t, validConfigYAML, tokenPath)
	require.NoError(t, err)
	assert.Equal(t, "existing-token-value", cfg2.Server.AuthToken)
}

func TestEnsureAuthToken_DoesNotOverride_ExplicitYAMLToken(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("token-file-value"), 0600))

	content := `
server:
  port: 8080
  host: "127.0.0.1"
  auth_token: "explicit-yaml-token"
layout:
  direction: horizontal
  children:
    - size: 100
      pane:
        id: main
        type: local
`
	cfg, err := loadWithAuthTokenPath(t, content, tokenPath)
	require.NoError(t, err)
	assert.Equal(t, "explicit-yaml-token", cfg.Server.AuthToken)
	assert.False(t, cfg.authTokenFromFile)

	// The pre-existing token file must be left untouched.
	data, readErr := os.ReadFile(tokenPath)
	require.NoError(t, readErr)
	assert.Equal(t, "token-file-value", string(data))
}

func TestEnsureAuthToken_WriteFailure_NonFatal_LoopbackHost(t *testing.T) {
	// Point the token path's parent at a regular file, so MkdirAll fails.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0600))
	tokenPath := filepath.Join(blocker, "token")

	cfg, err := loadWithAuthTokenPath(t, validConfigYAML, tokenPath)
	require.NoError(t, err, "loopback host must load successfully even if token persistence fails")
	assert.Empty(t, cfg.Server.AuthToken,
		"EnsureAuthToken must leave AuthToken empty, never panic or fatally error, on persistence failure")
}

func TestFinishLoad_NeverCallsEnsureAuthToken(t *testing.T) {
	// Regression test: finishLoad (and therefore the real Load) must never
	// touch the token file itself — only an explicit EnsureAuthToken call
	// does. Point authTokenPath at a location finishLoad could plausibly
	// have written to and confirm nothing appears there.
	tokenPath := filepath.Join(t.TempDir(), "token")

	f := writeTempFile(t, validConfigYAML)
	data, err := os.ReadFile(f)
	require.NoError(t, err)

	var cfg Config
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	cfg.filePath = f
	cfg.authTokenPath = tokenPath

	require.NoError(t, cfg.finishLoad())
	assert.Empty(t, cfg.Server.AuthToken, "finishLoad alone must never populate AuthToken")

	_, statErr := os.Stat(tokenPath)
	assert.True(t, os.IsNotExist(statErr), "finishLoad alone must never create the token file")
}

func TestValidate_NonLoopbackHost_EmptyToken_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.AuthToken = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth_token")
}

func TestValidate_NonLoopbackHost_WithToken_NoError(t *testing.T) {
	cfg := validConfig()
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.AuthToken = "some-token"
	assert.NoError(t, cfg.Validate())
}

func TestValidate_LoopbackHost_EmptyToken_NoError(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "::1", ""} {
		t.Run(host, func(t *testing.T) {
			cfg := validConfig()
			cfg.Server.Host = host
			cfg.Server.AuthToken = ""
			assert.NoError(t, cfg.Validate())
		})
	}
}

func TestValidate_ReservedSystemPaneID_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children[0].Pane.ID = "_system"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "_system")
	assert.Contains(t, err.Error(), "reserved")
}

func TestValidate_ReservedSystemPaneID_AlongsideValidPanes_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children = []LayoutChild{
		{Size: 50.0, Pane: &PaneConfig{ID: "_system", Type: "local"}},
		{Size: 50.0, Pane: &PaneConfig{ID: "ok-pane", Type: "local"}},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "_system")
}

func TestValidate_AgentBoardMode_InvalidValue_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children[0].Pane.AgentBoard.Mode = "bogus"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent_board.mode")
}

func TestValidate_AgentBoardMode_ValidValues_NoError(t *testing.T) {
	for _, mode := range []string{"monitor", "turn", "both", "off", ""} {
		t.Run(mode, func(t *testing.T) {
			cfg := validConfig()
			cfg.Layout.Children[0].Pane.AgentBoard.Mode = mode
			assert.NoError(t, cfg.Validate())
		})
	}
}

func TestValidate_AgentBoardTeam_ReservedSystemID_Error(t *testing.T) {
	cfg := validConfig()
	cfg.AgentBoard.Team = "_system"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent_board.team")
	assert.Contains(t, err.Error(), "_system")
}

func TestValidate_AgentBoardTeam_Empty_NoError(t *testing.T) {
	cfg := validConfig()
	cfg.AgentBoard.Team = ""
	assert.NoError(t, cfg.Validate())
}

func TestValidate_AgentBoardTeam_NonReserved_NoError(t *testing.T) {
	cfg := validConfig()
	cfg.AgentBoard.Team = "my-team"
	assert.NoError(t, cfg.Validate())
}

func TestValidate_AgmsgPath_RelativePath_Error(t *testing.T) {
	cfg := validConfig()
	cfg.AgentBoard.AgmsgPath = "relative/path"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent_board.agmsg_path")
}

func TestValidate_AgmsgPath_Empty_NoError(t *testing.T) {
	cfg := validConfig()
	cfg.AgentBoard.AgmsgPath = ""
	assert.NoError(t, cfg.Validate())
}

func TestValidate_AgmsgPath_AbsolutePath_NoError(t *testing.T) {
	cfg := validConfig()
	cfg.AgentBoard.AgmsgPath = "/opt/agmsg"
	assert.NoError(t, cfg.Validate())
}

func TestValidate_AgmsgPath_TildePrefixed_NoError(t *testing.T) {
	cfg := validConfig()
	cfg.AgentBoard.AgmsgPath = "~/.agents/skills/agmsg"
	assert.NoError(t, cfg.Validate())
}

func TestNormalizeAgentBoard_DefaultsTeamAndAgmsgPath(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token")
	cfg, err := loadWithAuthTokenPath(t, validConfigYAML, tokenPath)
	require.NoError(t, err)

	assert.Equal(t, "panemux", cfg.AgentBoard.Team)
	// AgmsgPath is deliberately left un-expanded here: it names a path on
	// whichever host (local, or any number of remote SSH hosts) it ends up
	// used against, so there is no single home directory to expand a
	// leading ~/ against at config-load time. Expansion happens per host at
	// AgmsgClient construction time in board.go instead — see
	// docs/agent-board.md's "~ in agmsg_path is expanded by panemux" section.
	assert.Equal(t, "~/.agents/skills/agmsg", cfg.AgentBoard.AgmsgPath)
}

func TestNormalizeAgentBoard_PreservesExplicitTeam(t *testing.T) {
	content := `
server:
  port: 8080
  host: "127.0.0.1"
agent_board:
  team: "custom-team"
  agmsg_path: "/opt/agmsg"
layout:
  direction: horizontal
  children:
    - size: 100
      pane:
        id: main
        type: local
`
	tokenPath := filepath.Join(t.TempDir(), "token")
	cfg, err := loadWithAuthTokenPath(t, content, tokenPath)
	require.NoError(t, err)
	assert.Equal(t, "custom-team", cfg.AgentBoard.Team)
	assert.Equal(t, "/opt/agmsg", cfg.AgentBoard.AgmsgPath)
}

func TestDefault_SetsAgentBoardDefaultsAndLeavesTokenEmpty(t *testing.T) {
	cfg := Default()
	assert.Equal(t, "panemux", cfg.AgentBoard.Team)
	// Default() must not expand AgmsgPath either, for the same reason
	// finishLoad's expandPaths doesn't — see
	// TestNormalizeAgentBoard_DefaultsTeamAndAgmsgPath's comment.
	assert.Equal(t, "~/.agents/skills/agmsg", cfg.AgentBoard.AgmsgPath)
	// Default() must never touch the real filesystem for a token — see
	// finishLoad's doc comment. AuthToken is only ever populated by an
	// explicit EnsureAuthToken call, which the real startup path (main.go)
	// makes but Default() itself never does.
	assert.Empty(t, cfg.Server.AuthToken)
}

func TestDefault_EnsureAuthTokenExplicitly_Populates(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token")

	cfg := Default()
	cfg.authTokenPath = tokenPath
	cfg.EnsureAuthToken()

	assert.NotEmpty(t, cfg.Server.AuthToken)
}

func TestSaveConfig_AutoGeneratedToken_NotWrittenBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	tokenPath := filepath.Join(t.TempDir(), "token")

	cfg := Default()
	cfg.authTokenPath = tokenPath
	cfg.filePath = path
	cfg.Server.AuthToken = ""
	cfg.authTokenFromFile = false
	cfg.EnsureAuthToken()
	require.NotEmpty(t, cfg.Server.AuthToken)
	require.True(t, cfg.authTokenFromFile)

	require.NoError(t, cfg.SaveLayout(cfg.Layout))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), cfg.Server.AuthToken)
}

func TestSaveConfig_ExplicitToken_WrittenBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	cfg := Default()
	cfg.filePath = path
	cfg.Server.AuthToken = "explicit-token"
	cfg.authTokenFromFile = false

	require.NoError(t, cfg.SaveLayout(cfg.Layout))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "explicit-token")
}

func TestPaneAgentBoardConfig_YAMLRoundTrip(t *testing.T) {
	content := `
server:
  port: 8080
  host: "127.0.0.1"
layout:
  direction: horizontal
  children:
    - size: 100
      pane:
        id: main
        type: local
        agent_board:
          enabled: true
          mode: monitor
`
	tokenPath := filepath.Join(t.TempDir(), "token")
	cfg, err := loadWithAuthTokenPath(t, content, tokenPath)
	require.NoError(t, err)

	pane := cfg.Layout.Children[0].Pane
	require.NotNil(t, pane.AgentBoard.Enabled)
	assert.True(t, *pane.AgentBoard.Enabled)
	assert.Equal(t, "monitor", pane.AgentBoard.Mode)
}

func TestPaneAgentBoardConfig_UnsetByDefault(t *testing.T) {
	cfg := validConfig()
	pane := cfg.Layout.Children[0].Pane
	assert.Nil(t, pane.AgentBoard.Enabled)
	assert.Empty(t, pane.AgentBoard.Mode)
}
