// Package session manages terminal sessions (local PTY, SSH, tmux).
package session

import (
	"fmt"
	"path/filepath"
	"strings"

	"panemux/internal/config"
	"panemux/internal/homedir"
	"panemux/internal/sshconfig"
)

// CreateFromConfig creates a Session from a PaneConfig and SSH connections map.
// For SSH types, the connection is resolved from sshConns merged over the
// ~/.ssh/config Host block of the same name (see resolveSSHConfig).
func CreateFromConfig(pane *config.PaneConfig, sshConns map[string]config.SSHConnection) (Session, error) {
	return createSession(pane, sshConns, sshconfig.DefaultPath())
}

// newTmuxLocalFn is an injectable seam over NewTmuxLocal so tests can verify
// the TypeTmux branch below forwards PaneConfig fields (in particular Cwd)
// without spawning a real tmux process.
var newTmuxLocalFn = NewTmuxLocal

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
		return newTmuxLocalFn(pane.ID, pane.Title, pane.TmuxSession, pane.Cwd)

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

// resolveSSHConfig looks up the SSH connection by name in the sshConns map and
// in the SSH config file at sshConfigPath, and merges the two field by field.
//
// A Host block of the same name supplies the starting values (with its
// ProxyJump resolved the same way), and every field the sshConns entry sets
// overrides that block's value for the same setting. An entry written as just
// its name therefore connects exactly as the Host block describes, and a name
// found in only one of the two sources resolves from that source alone.
func resolveSSHConfig(name string, sshConns map[string]config.SSHConnection, sshConfigPath string) (SSHConfig, error) {
	conn, inConns := sshConns[name]
	host, inFile := lookupSSHConfigHost(name, sshConfigPath)
	if !inConns && !inFile {
		return SSHConfig{}, fmt.Errorf("ssh connection %q not found", name)
	}

	var cfg SSHConfig
	if inFile {
		var err error
		if cfg, err = sshConfigFromHost(host, sshConns, sshConfigPath); err != nil {
			return SSHConfig{}, err
		}
	}
	overlaySSHConnection(&cfg, conn)

	if cfg.Host == "" {
		return SSHConfig{}, fmt.Errorf(
			"ssh connection %q has no host: set host under ssh_connections or add a Host %s block to ~/.ssh/config",
			name, name)
	}
	return cfg, nil
}

// lookupSSHConfigHost returns the non-wildcard Host block named name from the
// SSH config file at sshConfigPath. A file that cannot be read counts as not
// having the block: an ssh_connections entry that carries its own host must
// keep working whatever state ~/.ssh/config is in.
func lookupSSHConfigHost(name, sshConfigPath string) (sshconfig.Host, bool) {
	hosts, err := sshconfig.ParseHosts(sshConfigPath)
	if err != nil {
		return sshconfig.Host{}, false
	}
	for _, h := range hosts {
		if h.Name == name {
			return h, true
		}
	}
	return sshconfig.Host{}, false
}

// sshConfigFromHost converts one ~/.ssh/config Host block, resolving its
// ProxyJump alias through resolveSSHConfig.
func sshConfigFromHost(
	h sshconfig.Host, sshConns map[string]config.SSHConnection, sshConfigPath string,
) (SSHConfig, error) {
	port := h.Port
	if port == 0 {
		port = 22
	}
	cfg := SSHConfig{
		Host:         h.Hostname,
		Port:         port,
		User:         h.User,
		KeyFile:      expandIdentityFile(h.IdentityFile),
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

// overlaySSHConnection writes every field conn sets over cfg, leaving the
// fields conn leaves empty as they are.
func overlaySSHConnection(cfg *SSHConfig, conn config.SSHConnection) {
	if conn.Host != "" {
		cfg.Host = conn.Host
	}
	if conn.Port != 0 {
		cfg.Port = conn.Port
	}
	if conn.User != "" {
		cfg.User = conn.User
	}
	if conn.KeyFile != "" {
		cfg.KeyFile = conn.KeyFile
	}
	if conn.Password != "" {
		cfg.Password = conn.Password
	}
	if conn.KnownHostsFile != "" {
		cfg.KnownHostsFile = conn.KnownHostsFile
	}
}

// expandIdentityFile resolves an ssh_config IdentityFile against the user's
// home directory: a leading ~/ is expanded, and a relative path is taken as
// relative to the home directory, matching OpenSSH's own behavior.
//
// A home directory that cannot be resolved leaves the path exactly as the
// ssh config wrote it, rather than joining against an empty home — that turned
// "~/.ssh/id_ed25519" into ".ssh/id_ed25519" and left an already-relative path
// relative, either of which reads a private key out of whatever directory
// panemux happened to be started in. No error is reported because the rest of
// the entry is still usable and buildAuthMethods reports a genuinely
// unreachable key as "no auth methods" a moment later.
func expandIdentityFile(keyFile string) string {
	if keyFile == "" || filepath.IsAbs(keyFile) {
		return keyFile
	}
	home, err := homedir.Dir()
	if err != nil {
		return keyFile
	}
	if strings.HasPrefix(keyFile, "~/") {
		return filepath.Join(home, keyFile[2:])
	}
	return filepath.Join(home, keyFile)
}
