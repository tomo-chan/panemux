package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultAgentBoardTeam = "panemux"
	defaultAgmsgPath      = "~/.agents/skills/agmsg"

	agentBoardModeMonitor = "monitor"
	agentBoardModeTurn    = "turn"
	agentBoardModeBoth    = "both"
	agentBoardModeOff     = "off"

	// reservedSystemID is the sentinel identity panemux's own relay and
	// command center use as their agmsg from/to. It is deliberately not
	// derived from the product name, since the application could be
	// renamed without this reserved identity needing to change.
	reservedSystemID = "_system"

	authTokenByteLen = 32
)

const authTokenFileMode os.FileMode = 0600

// AgentBoardConfig holds the top-level agent_board settings shared by every
// board-enabled pane.
type AgentBoardConfig struct {
	Team      string `yaml:"team,omitempty"       json:"team,omitempty"`
	AgmsgPath string `yaml:"agmsg_path,omitempty" json:"agmsg_path,omitempty"`
}

// CommandCenterConfig holds the top-level command_center settings.
type CommandCenterConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// PaneAgentBoardConfig holds per-pane agent_board overrides.
type PaneAgentBoardConfig struct {
	Enabled *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Mode    string `yaml:"mode,omitempty"    json:"mode,omitempty"`
}

func (c *Config) normalizeAgentBoard() {
	if c.AgentBoard.Team == "" {
		c.AgentBoard.Team = defaultAgentBoardTeam
	}
	if c.AgentBoard.AgmsgPath == "" {
		c.AgentBoard.AgmsgPath = defaultAgmsgPath
	}
}

// EnsureAuthToken fills in Server.AuthToken when it is not already set,
// either by reading a previously persisted token file or by generating and
// persisting a new random token. Failure to resolve, read, or persist a
// token is non-fatal: it is logged as a warning and AuthToken is left
// empty, so Validate's non-loopback-requires-token rule remains the actual
// enforcement point rather than this method hard-failing for the common
// loopback case.
//
// Deliberately not called by Load/Default/finishLoad — see finishLoad's own
// doc comment for why. Callers that want the "auto-generate on first run"
// behavior (currently only main.go's real startup path) must call this
// explicitly after Load/LoadOrDefault has already succeeded.
func (c *Config) EnsureAuthToken() {
	if c.Server.AuthToken != "" {
		return
	}

	path, err := c.resolveAuthTokenPath()
	if err != nil {
		log.Printf("Warning: failed to resolve auth token path: %v", err)
		return
	}

	if data, readErr := os.ReadFile(path); readErr == nil {
		if token := strings.TrimSpace(string(data)); token != "" {
			c.Server.AuthToken = token
			c.authTokenFromFile = true
			return
		}
	}

	token, err := generateAuthToken()
	if err != nil {
		log.Printf("Warning: failed to generate auth token: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		log.Printf("Warning: failed to create auth token directory: %v", err)
		return
	}
	if err := os.WriteFile(path, []byte(token), authTokenFileMode); err != nil {
		log.Printf("Warning: failed to persist auth token: %v", err)
		return
	}

	c.Server.AuthToken = token
	c.authTokenFromFile = true
}

func (c *Config) resolveAuthTokenPath() (string, error) {
	if c.authTokenPath != "" {
		return c.authTokenPath, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".config", "panemux", "token"), nil
}

func generateAuthToken() (string, error) {
	buf := make([]byte, authTokenByteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating auth token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
