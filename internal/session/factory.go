// Package session manages terminal sessions (local PTY, SSH, tmux).
package session

import (
	"fmt"
	"path/filepath"
	"slices"
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

// newTmuxLocalAttachFn is an injectable seam over NewTmuxLocalAttach.
var newTmuxLocalAttachFn = NewTmuxLocalAttach

// CreateTmuxAttach creates the board's temporary attach (issue #283) to the
// running tmux session tmuxSession: on the panemux host when connection is
// empty, otherwise on the named ssh_connections entry.
func CreateTmuxAttach(
	id, title, connection, tmuxSession string,
	sshConns map[string]config.SSHConnection,
) (Session, error) {
	return createTmuxAttach(id, title, connection, tmuxSession, sshConns, sshconfig.DefaultPath())
}

func createTmuxAttach(
	id, title, connection, tmuxSession string,
	sshConns map[string]config.SSHConnection,
	sshConfigPath string,
) (Session, error) {
	if connection == "" {
		return newTmuxLocalAttachFn(id, title, tmuxSession)
	}
	cfg, err := resolveSSHConfig(connection, sshConns, sshConfigPath)
	if err != nil {
		return nil, err
	}
	cfg.ConnectionName = connection
	return NewTmuxSSHAttach(id, title, tmuxSession, cfg)
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
//
// An entry that sets its own host takes no route from the Host block: its
// ProxyJump is neither inherited nor resolved, and its ProxyCommand is not
// inherited. The route belongs to the host the block names, and the entry
// has replaced that host.
func resolveSSHConfig(name string, sshConns map[string]config.SSHConnection, sshConfigPath string) (SSHConfig, error) {
	return resolveSSHConfigChain(name, sshConns, sshConfigPath, nil)
}

// resolveSSHConfigChain is resolveSSHConfig with the ProxyJump names already
// being resolved above it, so a chain that comes back to one of them is an
// error rather than recursion without end.
func resolveSSHConfigChain(
	name string, sshConns map[string]config.SSHConnection, sshConfigPath string, chain []string,
) (SSHConfig, error) {
	if slices.Contains(chain, name) {
		return SSHConfig{}, fmt.Errorf("proxy jump cycle: %s", strings.Join(append(chain, name), " -> "))
	}
	chain = append(chain, name)

	conn, inConns := sshConns[name]
	host, inFile, readErr := lookupSSHConfigHost(name, sshConfigPath)
	if !inConns && !inFile {
		return SSHConfig{}, fmt.Errorf("ssh connection %q not found", name)
	}

	var cfg SSHConfig
	if inFile {
		if conn.Host != "" {
			host.ProxyJump, host.ProxyCommand = "", ""
		}
		var err error
		if cfg, err = sshConfigFromHost(host, sshConns, sshConfigPath, chain); err != nil {
			return SSHConfig{}, err
		}
	}
	overlaySSHConnection(&cfg, conn)

	if cfg.Host == "" {
		err := fmt.Errorf(
			"ssh connection %q has no host: set host under ssh_connections or add a Host %s block to ~/.ssh/config",
			name, name)
		if readErr != nil {
			err = fmt.Errorf("%w (the ssh config could not be read: %w)", err, readErr)
		}
		return SSHConfig{}, err
	}
	return cfg, nil
}

// lookupSSHConfigHost returns the non-wildcard Host block named name from the
// SSH config file at sshConfigPath. A file that cannot be read counts as not
// having the block, and the reason is returned for the caller to report: an
// ssh_connections entry that carries its own host must keep working whatever
// state ~/.ssh/config is in.
//
// A path that is not absolute is not read at all. sshconfig.DefaultPath
// returns one when the home directory cannot be resolved, and reading it would
// take a Host block — ProxyCommand included — from whatever directory panemux
// was started in.
func lookupSSHConfigHost(name, sshConfigPath string) (sshconfig.Host, bool, error) {
	if !filepath.IsAbs(sshConfigPath) {
		return sshconfig.Host{}, false, fmt.Errorf("ssh config path %q is not absolute", sshConfigPath)
	}
	hosts, err := sshconfig.ParseHosts(sshConfigPath)
	if err != nil {
		return sshconfig.Host{}, false, err
	}
	for _, h := range hosts {
		if h.Name == name {
			return h, true, nil
		}
	}
	return sshconfig.Host{}, false, nil
}

// sshConfigFromHost converts one ~/.ssh/config Host block, resolving its
// ProxyJump alias through resolveSSHConfigChain.
func sshConfigFromHost(
	h sshconfig.Host, sshConns map[string]config.SSHConnection, sshConfigPath string, chain []string,
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
		jumpCfg, err := resolveSSHConfigChain(h.ProxyJump, sshConns, sshConfigPath, chain)
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
