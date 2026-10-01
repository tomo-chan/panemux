package config

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// sshConnectionNameRe is the name rule POST /api/ssh-config/hosts already
// applies to a ~/.ssh/config alias, so a name accepted for one is accepted for
// the other — which is what lets an ssh_connections entry be just the name of
// a Host block.
var sshConnectionNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.\-]+$`)

// ValidateSSHConnectionName checks a name for a new ssh_connections entry.
//
// It is applied to entries created through the API only. A name already in
// config.yaml is not re-checked, so an entry written by hand under a name this
// rule would refuse still loads, and can still be edited and deleted.
func ValidateSSHConnectionName(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if !sshConnectionNameRe.MatchString(name) {
		return errors.New("name must contain only alphanumeric characters, hyphens, underscores, or dots")
	}
	return nil
}

// ValidateSSHConnection checks the fields of an ssh_connections entry written
// through the API (issue #272). Every field is optional, as it is in
// config.yaml: an entry with none takes its details from the ~/.ssh/config
// Host block of the same name.
//
// Errors name the field, never its value, so a password typed into the wrong
// field is not echoed back. The password itself is not checked: it is never
// written anywhere a character in it could change meaning.
func ValidateSSHConnection(conn SSHConnection) error {
	var errs []string
	if conn.Port < 0 || conn.Port > 65535 {
		errs = append(errs, "port must be between 1 and 65535")
	}
	for _, f := range []struct{ field, value string }{
		{"host", conn.Host},
		{"user", conn.User},
		{"key_file", conn.KeyFile},
		{"known_hosts_file", conn.KnownHostsFile},
	} {
		if strings.IndexFunc(f.value, unicode.IsControl) >= 0 {
			errs = append(errs, f.field+" must not contain a control character")
		}
	}
	for _, f := range []struct{ field, value string }{
		{"key_file", conn.KeyFile},
		{"known_hosts_file", conn.KnownHostsFile},
	} {
		if f.value != "" && !strings.HasPrefix(f.value, "/") && !strings.HasPrefix(f.value, "~/") {
			errs = append(errs, f.field+" must be an absolute path or start with ~/")
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// ExpandSSHConnectionPaths expands a leading ~/ in the entry's key_file and
// known_hosts_file, as Load does for the entries in config.yaml.
func ExpandSSHConnectionPaths(conn SSHConnection) SSHConnection {
	conn.KeyFile = expandTilde(conn.KeyFile)
	conn.KnownHostsFile = expandTilde(conn.KnownHostsFile)
	return conn
}

// PanesUsingConnection returns the IDs of the ssh and ssh_tmux panes, in every
// workspace, whose connection is name, sorted.
func (c *Data) PanesUsingConnection(name string) []string {
	var ids []string
	for _, pane := range c.AllPanes() {
		if (pane.Type == PaneTypeSSH || pane.Type == PaneTypeSSHTmux) && pane.Connection == name {
			ids = append(ids, pane.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// SaveSSHConnections writes the config file after a change to
// SSHConnections. It is the same whole-file write the workspace routes use.
func (c *Config) SaveSSHConnections() error {
	c.normalizeWorkspaces()
	if err := c.write(); err != nil {
		return fmt.Errorf("saving ssh_connections: %w", err)
	}
	return nil
}
