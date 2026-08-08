package config

import (
	"strings"
	"testing"
)

func validBaseConfigForAgentBoardTests() *Config {
	cfg := Default()
	return cfg
}

func TestValidate_LoopbackHostEmptyAuthToken_NoError(t *testing.T) {
	cfg := validBaseConfigForAgentBoardTests()
	cfg.Server.Host = defaultServerHost
	cfg.Server.AuthToken = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for loopback host with empty auth_token, got %v", err)
	}
}

func TestValidate_LoopbackHostWithAuthToken_NoError(t *testing.T) {
	cfg := validBaseConfigForAgentBoardTests()
	cfg.Server.Host = defaultServerHost
	cfg.Server.AuthToken = "some-token"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for loopback host with an explicit auth_token, got %v", err)
	}
}

func TestValidate_NonLoopbackHostEmptyAuthToken_Error(t *testing.T) {
	cfg := validBaseConfigForAgentBoardTests()
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.AuthToken = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an error for a non-loopback host with an empty auth_token")
	}
	if !strings.Contains(err.Error(), "auth_token") {
		t.Fatalf("expected error to mention auth_token, got %v", err)
	}
}

func TestValidate_NonLoopbackHostWithAuthToken_NoError(t *testing.T) {
	cfg := validBaseConfigForAgentBoardTests()
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.AuthToken = "some-token"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for a non-loopback host with an explicit auth_token, got %v", err)
	}
}

func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"", true},
		{defaultServerHost, true},
		{"127.0.0.2", true},
		{"::1", true},
		{"localhost", true},
		{"0.0.0.0", false},
		{"192.168.1.5", false},
		{"example.com", false},
	}
	for _, tt := range tests {
		if got := isLoopbackHost(tt.host); got != tt.want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestValidate_PanemuxReservedPaneID_Error(t *testing.T) {
	cfg := validBaseConfigForAgentBoardTests()
	cfg.Layout = singleLocalPaneLayout(PanemuxReservedPaneID)
	cfg.Workspaces.Items[0].Layout = cfg.Layout

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an error for a pane id of _panemux")
	}
	if !strings.Contains(err.Error(), "_panemux") {
		t.Fatalf("expected error to mention the reserved id, got %v", err)
	}
}

func TestValidate_PanemuxReservedPaneID_ErrorAlongsideOtherValidPanes(t *testing.T) {
	cfg := validBaseConfigForAgentBoardTests()
	cfg.Layout = LayoutNode{
		Direction: "horizontal",
		Children: []LayoutChild{
			{Size: 50, Pane: &PaneConfig{ID: "pane-a", Type: "local"}},
			{Size: 50, Pane: &PaneConfig{ID: PanemuxReservedPaneID, Type: "local"}},
		},
	}
	cfg.Workspaces.Items[0].Layout = cfg.Layout

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an error even when other panes in the same config are valid")
	}
	if !strings.Contains(err.Error(), "_panemux") {
		t.Fatalf("expected error to mention the reserved id, got %v", err)
	}
}

func TestValidate_PaneAgentBoardMode_Valid(t *testing.T) {
	for _, mode := range []string{"", AgentBoardModeMonitor, AgentBoardModeTurn, AgentBoardModeBoth, AgentBoardModeOff} {
		cfg := validBaseConfigForAgentBoardTests()
		cfg.Layout = LayoutNode{
			Direction: "horizontal",
			Children: []LayoutChild{
				{Size: 100, Pane: &PaneConfig{
					ID: "pane-a", Type: "local",
					AgentBoard: &PaneAgentBoardConfig{Enabled: true, Mode: mode},
				}},
			},
		}
		cfg.Workspaces.Items[0].Layout = cfg.Layout
		if err := cfg.Validate(); err != nil {
			t.Fatalf("mode %q: expected no error, got %v", mode, err)
		}
	}
}

func TestValidate_PaneAgentBoardMode_Invalid(t *testing.T) {
	cfg := validBaseConfigForAgentBoardTests()
	cfg.Layout = LayoutNode{
		Direction: "horizontal",
		Children: []LayoutChild{
			{Size: 100, Pane: &PaneConfig{
				ID: "pane-a", Type: "local",
				AgentBoard: &PaneAgentBoardConfig{Enabled: true, Mode: "bogus"},
			}},
		},
	}
	cfg.Workspaces.Items[0].Layout = cfg.Layout
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an error for an invalid agent_board.mode")
	}
}

func TestAgentBoardConfig_DefaultsTeamWhenUnset(t *testing.T) {
	cfg := &AgentBoardConfig{}
	cfg.normalize()
	if cfg.Team != "panemux" {
		t.Fatalf("Team = %q, want %q", cfg.Team, "panemux")
	}
}

func TestAgentBoardConfig_PreservesExplicitTeam(t *testing.T) {
	cfg := &AgentBoardConfig{Team: "custom-team"}
	cfg.normalize()
	if cfg.Team != "custom-team" {
		t.Fatalf("Team = %q, want %q", cfg.Team, "custom-team")
	}
}

func TestDefault_AgentBoardTeamDefaultsToPanemux(t *testing.T) {
	cfg := Default()
	if cfg.AgentBoard.Team != "panemux" {
		t.Fatalf("Default().AgentBoard.Team = %q, want %q", cfg.AgentBoard.Team, "panemux")
	}
}
