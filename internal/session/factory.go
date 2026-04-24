// Package session manages terminal sessions (local PTY, SSH, tmux).
package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"panemux/internal/config"
	"panemux/internal/sshconfig"
)

// CreateFromConfig creates a Session from a PaneConfig and SSH connections map.
// For SSH types, if the connection is not found in sshConns, it falls back to ~/.ssh/config.
func CreateFromConfig(pane *config.PaneConfig, sshConns map[string]config.SSHConnection) (Session, error) {
	return createSession(pane, sshConns, sshconfig.DefaultPath())
}

// createSession is the internal, testable version of CreateFromConfig that accepts
// an explicit SSH config path instead of always using the default.
func createSession(
	pane *config.PaneConfig,
	sshConns map[string]config.SSHConnection,
	sshConfigPath string,
) (Session, error) {
	switch Type(pane.Type) {
	case TypeLocal:
		return NewLocal(pane.ID, pane.Shell, pane.Cwd, pane.Title)

	case TypeSSH:
		cfg, err := resolveSSHConfig(pane.Connection, sshConns, sshConfigPath)
		if err != nil {
			return nil, err
		}
		cfg.Cwd = pane.Cwd
		cfg.Shell = pane.Shell
		cfg.ConnectionName = pane.Connection
		return NewSSH(pane.ID, pane.Title, cfg)

	case TypeTmux:
		return NewTmuxLocal(pane.ID, pane.Title, pane.TmuxSession)

	case TypeSSHTmux:
		cfg, err := resolveSSHConfig(pane.Connection, sshConns, sshConfigPath)
		if err != nil {
			return nil, err
		}
		cfg.Cwd = pane.Cwd
		cfg.Shell = pane.Shell
		cfg.ConnectionName = pane.Connection
		return NewTmuxSSH(pane.ID, pane.Title, pane.TmuxSession, cfg)

	default:
		return nil, fmt.Errorf("unknown session type: %s", pane.Type)
	}
}

// ResolveSSHConfig is the exported version of resolveSSHConfig for use by the API handler.
func ResolveSSHConfig(name string, sshConns map[string]config.SSHConnection, sshConfigPath string) (SSHConfig, error) {
	return resolveSSHConfig(name, sshConns, sshConfigPath)
}

// resolveSSHConfig looks up the SSH connection by name, first in the sshConns map,
// then falling back to the SSH config file at sshConfigPath.
func resolveSSHConfig(name string, sshConns map[string]config.SSHConnection, sshConfigPath string) (SSHConfig, error) {
	if conn, ok := sshConns[name]; ok {
		return SSHConfig{
			Host:           conn.Host,
			Port:           conn.Port,
			User:           conn.User,
			KeyFile:        conn.KeyFile,
			Password:       conn.Password,
			KnownHostsFile: conn.KnownHostsFile,
		}, nil
	}

	// Fall back to ~/.ssh/config
	hosts, err := sshconfig.ParseHosts(sshConfigPath)
	if err != nil {
		return SSHConfig{}, fmt.Errorf("ssh connection %q not found", name)
	}

	for _, h := range hosts {
		if h.Name == name {
			port := h.Port
			if port == 0 {
				port = 22
			}
			keyFile := h.IdentityFile
			if keyFile != "" {
				home, _ := os.UserHomeDir()
				if strings.HasPrefix(keyFile, "~/") {
					keyFile = filepath.Join(home, keyFile[2:])
				} else if !filepath.IsAbs(keyFile) {
					// Relative paths (e.g. ".ssh/id_ed25519") are relative to HOME,
					// matching OpenSSH behaviour.
					keyFile = filepath.Join(home, keyFile)
				}
			}
			cfg := SSHConfig{
				Host:         h.Hostname,
				Port:         port,
				User:         h.User,
				KeyFile:      keyFile,
				ProxyCommand: h.ProxyCommand,
			}
			if h.ProxyJump != "" {
				jumpCfg, err := resolveSSHConfig(h.ProxyJump, sshConns, sshConfigPath)
				if err != nil {
					return SSHConfig{}, fmt.Errorf("resolving proxy jump %q: %w", h.ProxyJump, err)
				}
				cfg.JumpHost = &jumpCfg
			}
			return cfg, nil
		}
	}

	return SSHConfig{}, fmt.Errorf("ssh connection %q not found", name)
}
