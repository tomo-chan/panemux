package main

import (
	"fmt"
	"log"
	"os"

	"panemux/internal/commandcenter"
	"panemux/internal/config"
)

// setupCommandCenter builds the command center's Runner when
// command_center.enabled is true, wired with real session/history paths and
// the current executable re-invoked as its own narrow MCP server (see
// docs/agent-board.md's Command center section). It returns nil when
// disabled, or when no auth token is configured — the command center's MCP
// server authenticates every REST call it makes with that token, so running
// without one would mean every query fails at the first tool call. server.New
// treats a nil Runner as "don't register /ws/board-command at all," matching
// agent board's own "additive, never load-bearing" bootstrap philosophy.
func setupCommandCenter(cfg *config.Config) *commandcenter.Runner {
	if !cfg.CommandCenter.Enabled {
		return nil
	}
	if cfg.Server.AuthToken == "" {
		log.Printf("Warning: command center: enabled but no server.auth_token is configured, skipping")
		return nil
	}

	sessionPath, err := commandcenter.DefaultSessionFilePath()
	if err != nil {
		log.Printf("Warning: command center: resolving session file path: %v", err)
		return nil
	}
	historyPath, err := commandcenter.DefaultHistoryFilePath()
	if err != nil {
		log.Printf("Warning: command center: resolving history file path: %v", err)
		return nil
	}

	// The command center's own claude -p subprocess always talks to
	// loopback, regardless of what interface server.host binds to — it runs
	// as a local subprocess on the same host panemux itself runs on, and
	// 127.0.0.1 always reaches a service bound to any local interface,
	// including a non-loopback server.host.
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", cfg.Server.Port)
	token := cfg.Server.AuthToken

	return commandcenter.NewRunner(commandcenter.RunnerConfig{
		SessionPath:  sessionPath,
		HistoryPath:  historyPath,
		AllowedTools: commandcenter.AllowedTools(),
		BuildMCPConfig: func() (string, func(), error) {
			execPath, err := os.Executable()
			if err != nil {
				return "", nil, fmt.Errorf("resolving panemux executable path: %w", err)
			}
			return commandcenter.BuildMCPConfig(execPath, baseURL, token)
		},
	})
}
