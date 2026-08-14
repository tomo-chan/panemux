package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"

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

	baseURL := commandCenterBaseURL(cfg)
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

// commandCenterBaseURL returns the URL the command center's own claude -p
// subprocess should call to reach panemux's own REST API. 127.0.0.1 (or its
// IPv6 equivalent) is substituted only for a wildcard bind (empty,
// "0.0.0.0", or "::") — a specific non-wildcard, non-loopback bind (e.g.
// "192.168.1.50") is NOT reachable via 127.0.0.1, since binding to a
// specific interface restricts the listening socket to that one address;
// the configured host must be used as-is in that case.
// loopbackIPv4 is the literal IPv4 loopback host string substituted for a
// wildcard server.host bind.
const loopbackIPv4 = "127.0.0.1"

func commandCenterBaseURL(cfg *config.Config) string {
	host := cfg.Server.Host
	// net.Listen requires the wildcard IPv6 bind bracketed ("[::]") — a bare
	// "::" is not itself a form it accepts — so a config value may arrive
	// either way. Strip any pre-existing brackets before the wildcard/host
	// checks below so both "::" and "[::]" are recognized identically, and
	// so net.JoinHostPort (which adds its own brackets for any address
	// containing a colon) never receives an already-bracketed host and
	// double-brackets it.
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	switch host {
	case "", "0.0.0.0":
		host = loopbackIPv4
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(cfg.Server.Port))
}
