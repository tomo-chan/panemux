package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/homedir"
)

func TestValidateSSHConnectionName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "letters digits and punctuation", input: "gpu-box_2.lan"},
		{name: "empty", input: "", wantErr: "name is required"},
		{name: "space", input: "gpu box", wantErr: "name must contain only"},
		{name: "slash", input: "gpu/box", wantErr: "name must contain only"},
		{name: "newline", input: "gpu\nbox", wantErr: "name must contain only"},
		{name: "colon", input: "gpu:box", wantErr: "name must contain only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSSHConnectionName(tt.input)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidateSSHConnection(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
		conn    SSHConnection
	}{
		{name: "no fields at all", conn: SSHConnection{}},
		{name: "every field", conn: SSHConnection{
			Host: "gpu.invalid", User: "demo", Port: 2222,
			KeyFile: "~/.ssh/id_ed25519", KnownHostsFile: "/remote/home/demo/.ssh/known_hosts",
			Password: "p@ss word",
		}},
		{name: "lowest port", conn: SSHConnection{Port: 1}},
		{name: "highest port", conn: SSHConnection{Port: 65535}},
		{name: "negative port", conn: SSHConnection{Port: -1}, wantErr: "port must be between 1 and 65535"},
		{name: "port above range", conn: SSHConnection{Port: 65536}, wantErr: "port must be between 1 and 65535"},
		{name: "control character in host", conn: SSHConnection{Host: "gpu\n.invalid"}, wantErr: "host must not contain"},
		{name: "control character first", conn: SSHConnection{Host: "\tgpu.invalid"}, wantErr: "host must not contain"},
		{name: "control character in user", conn: SSHConnection{User: "de\tmo"}, wantErr: "user must not contain"},
		{name: "control character in key_file", conn: SSHConnection{KeyFile: "/k\x00"}, wantErr: "key_file must not contain"},
		{
			name:    "control character in known_hosts_file",
			conn:    SSHConnection{KnownHostsFile: "/k\r"},
			wantErr: "known_hosts_file must not contain",
		},
		{name: "relative key_file", conn: SSHConnection{KeyFile: ".ssh/id"}, wantErr: "key_file must be an absolute path"},
		{
			name:    "relative known_hosts_file",
			conn:    SSHConnection{KnownHostsFile: "known_hosts"},
			wantErr: "known_hosts_file must be an absolute path",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSSHConnection(tt.conn)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// The error names the field that is wrong, never a value: the value can be a
// password the caller typed into the wrong field.
func TestValidateSSHConnection_ErrorsDoNotEchoValues(t *testing.T) {
	const secret = "s3cret-value"
	for _, conn := range []SSHConnection{
		{Host: secret + "\n"},
		{User: secret + "\n"},
		{KeyFile: secret},
		{KnownHostsFile: secret},
	} {
		err := ValidateSSHConnection(conn)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), secret)
	}
}

func TestExpandSSHConnectionPaths(t *testing.T) {
	homedir.SetForTest(t, "/remote/home/demo")

	got := ExpandSSHConnectionPaths(SSHConnection{
		Host: "~/not-a-path", KeyFile: "~/.ssh/id", KnownHostsFile: "~/.ssh/known_hosts",
	})

	assert.Equal(t, SSHConnection{
		Host:           "~/not-a-path",
		KeyFile:        "/remote/home/demo/.ssh/id",
		KnownHostsFile: "/remote/home/demo/.ssh/known_hosts",
	}, got)
}

func TestPanesUsingConnection(t *testing.T) {
	cfg := &Config{Data: Data{
		Workspaces: WorkspacesConfig{
			Active: "one",
			Items: []WorkspaceConfig{
				{ID: "one", Title: "One", Layout: LayoutNode{Children: []LayoutChild{
					{Size: 50, Pane: &PaneConfig{ID: "b-ssh", Type: PaneTypeSSH, Connection: "gpu"}},
					{Size: 50, Pane: &PaneConfig{ID: "local", Type: PaneTypeLocal, Connection: "gpu"}},
				}}},
				{ID: "two", Title: "Two", Layout: LayoutNode{Children: []LayoutChild{
					{Size: 50, Children: []LayoutChild{
						{Size: 100, Pane: &PaneConfig{ID: "a-tmux", Type: PaneTypeSSHTmux, Connection: "gpu", TmuxSession: "s"}},
					}},
					{Size: 50, Pane: &PaneConfig{ID: "other", Type: PaneTypeSSH, Connection: "build"}},
				}}},
			},
		},
	}}

	assert.Equal(t, []string{"a-tmux", "b-ssh"}, cfg.PanesUsingConnection("gpu"))
	assert.Equal(t, []string{"other"}, cfg.PanesUsingConnection("build"))
	assert.Empty(t, cfg.PanesUsingConnection("missing"))
}

// An ssh_connections change made in memory reaches config.yaml, and a fresh
// Load reads it back: the persistence half of issue #272's "the saved
// contents survive a reload".
func TestSaveSSHConnections_PersistsAndReloads(t *testing.T) {
	homedir.SetForTest(t, t.TempDir())
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8080\n  host: 127.0.0.1\n"), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)

	cfg.SSHConnections = map[string]SSHConnection{
		"gpu":        {Host: "gpu.invalid", User: "demo", Port: 2222, Password: "pw"},
		"name-only":  {},
		"with-files": {KeyFile: "/remote/home/demo/.ssh/id", KnownHostsFile: "/remote/home/demo/.ssh/kh"},
	}
	require.NoError(t, cfg.SaveSSHConnections())

	reloaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, cfg.SSHConnections, reloaded.SSHConnections)
}
