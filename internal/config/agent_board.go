package config

// DefaultAgentBoardTeam is the agmsg team every board-enabled pane joins
// unless overridden. See docs/agent-board.md's "Config additions".
const DefaultAgentBoardTeam = "panemux"

// PanemuxReservedPaneID is the sentinel agmsg identity reserved for status
// reports and every message panemux itself originates. A pane may never be
// configured with this ID — see Validate and
// docs/agent-board.md#config-additions.
const PanemuxReservedPaneID = "_panemux"

// Agent Board delivery modes, mirroring agmsg's own
// `/agmsg mode monitor|turn|both|off`. See docs/agent-board.md's Bootstrap
// flow.
const (
	AgentBoardModeMonitor = "monitor"
	AgentBoardModeTurn    = "turn"
	AgentBoardModeBoth    = "both"
	AgentBoardModeOff     = "off"
)

// AgentBoardConfig holds the instance-wide Agent Board settings.
type AgentBoardConfig struct {
	// Team is the agmsg team every board-enabled pane joins unless a pane
	// overrides it. Defaults to "panemux".
	Team string `yaml:"team,omitempty" json:"team,omitempty"`
	// AgmsgPath is where scripts/api.sh is expected to live, e.g.
	// "~/.agents/skills/agmsg". A leading "~" is expanded at load time for
	// the local host; per-host (SSH) expansion happens separately via
	// session.BoardHomeDirer, since it depends on the remote user's home
	// directory, not the local one.
	AgmsgPath string `yaml:"agmsg_path,omitempty" json:"agmsg_path,omitempty"`
}

// normalize fills in defaults left unset by the operator.
func (c *AgentBoardConfig) normalize() {
	if c.Team == "" {
		c.Team = DefaultAgentBoardTeam
	}
}

// PaneAgentBoardConfig holds the per-pane Agent Board overrides.
type PaneAgentBoardConfig struct {
	// Mode mirrors agmsg's own /agmsg mode: "monitor" (default), "turn"
	// (legacy), "both", or "off".
	Mode    string `yaml:"mode,omitempty" json:"mode,omitempty"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

// ValidAgentBoardMode reports whether mode is a recognized Agent Board
// delivery mode, treating "" as valid (defaults to monitor).
func ValidAgentBoardMode(mode string) bool {
	switch mode {
	case "", AgentBoardModeMonitor, AgentBoardModeTurn, AgentBoardModeBoth, AgentBoardModeOff:
		return true
	default:
		return false
	}
}
