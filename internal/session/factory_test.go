package session

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/homedir"

	"panemux/internal/testcap"
)

func TestCreateFromConfig_Local(t *testing.T) {
	testcap.RequirePTY(t)
	pane := &config.PaneConfig{
		ID:    "test-local",
		Type:  "local",
		Shell: "/bin/sh",
		Title: "Test",
	}
	sess, err := CreateFromConfig(pane, nil)
	require.NoError(t, err)
	defer sess.Close()

	assert.Equal(t, "test-local", sess.ID())
	assert.Equal(t, TypeLocal, sess.Type())
}

// TestCreateSession_Tmux_PassesCwdThrough verifies that pane.Cwd flows from
// PaneConfig through createSession's TypeTmux branch into the constructor
// used to build the local tmux session. This targets the wiring itself
// (factory.go forgetting to forward pane.Cwd), not the argument-building
// logic already covered by TestTmuxLocalArgs_*, using an injected stub so no
// real tmux process is spawned.
func TestCreateSession_Tmux_PassesCwdThrough(t *testing.T) {
	prev := newTmuxLocalFn
	defer func() { newTmuxLocalFn = prev }()

	var gotID, gotTitle, gotTmuxSession, gotCwd string
	newTmuxLocalFn = func(id, title, tmuxSession, cwd string) (*TmuxLocalSession, error) {
		gotID, gotTitle, gotTmuxSession, gotCwd = id, title, tmuxSession, cwd
		return &TmuxLocalSession{id: id, title: title, tmuxSession: tmuxSession, state: StateConnected}, nil
	}

	pane := &config.PaneConfig{
		ID:          "test-tmux",
		Type:        "tmux",
		Title:       "Test Tmux",
		TmuxSession: "mysession",
		Cwd:         "/workspace/user/project",
	}
	sess, err := createSession(pane, nil, "")
	require.NoError(t, err)

	assert.Equal(t, "test-tmux", gotID)
	assert.Equal(t, "Test Tmux", gotTitle)
	assert.Equal(t, "mysession", gotTmuxSession)
	assert.Equal(t, "/workspace/user/project", gotCwd)
	assert.Equal(t, TypeTmux, sess.Type())
}

// TestCreateSession_Tmux_EmptyCwd_PassesEmptyString verifies the empty-cwd
// case is also forwarded as-is (not substituted with a default), matching
// NewTmuxLocal's own empty-cwd handling in tmuxLocalArgs.
func TestCreateSession_Tmux_EmptyCwd_PassesEmptyString(t *testing.T) {
	prev := newTmuxLocalFn
	defer func() { newTmuxLocalFn = prev }()

	var gotCwd string
	cwdSeen := false
	newTmuxLocalFn = func(id, title, tmuxSession, cwd string) (*TmuxLocalSession, error) {
		gotCwd = cwd
		cwdSeen = true
		return &TmuxLocalSession{id: id, title: title, tmuxSession: tmuxSession, state: StateConnected}, nil
	}

	pane := &config.PaneConfig{
		ID:          "test-tmux-no-cwd",
		Type:        "tmux",
		TmuxSession: "mysession2",
	}
	_, err := createSession(pane, nil, "")
	require.NoError(t, err)

	require.True(t, cwdSeen)
	assert.Empty(t, gotCwd)
}

func TestCreateFromConfig_UnknownType(t *testing.T) {
	pane := &config.PaneConfig{
		ID:   "test",
		Type: "unknown",
	}
	_, err := CreateFromConfig(pane, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
}

func TestCreateFromConfig_SSHMissing(t *testing.T) {
	pane := &config.PaneConfig{
		ID:         "test-ssh",
		Type:       "ssh",
		Connection: "nonexistent",
	}
	_, err := CreateFromConfig(pane, map[string]config.SSHConnection{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestResolveSSHConfig_ProxyJump verifies that a ProxyJump directive in ~/.ssh/config
// causes the returned SSHConfig to have JumpHost populated with the jump host's details.
func TestResolveSSHConfig_ProxyJump(t *testing.T) {
	dir := t.TempDir()
	sshCfgPath := filepath.Join(dir, "config")
	content := `Host jump-host
    HostName jump.example.com
    User jumpuser
    Port 22

Host target-host
    HostName target.internal
    User admin
    ProxyJump jump-host
`
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(content), 0600))

	cfg, err := resolveSSHConfig("target-host", nil, sshCfgPath)
	require.NoError(t, err)

	assert.Equal(t, "target.internal", cfg.Host)
	assert.Equal(t, "admin", cfg.User)
	require.NotNil(t, cfg.JumpHost, "JumpHost should be populated when ProxyJump is set")
	assert.Equal(t, "jump.example.com", cfg.JumpHost.Host)
	assert.Equal(t, "jumpuser", cfg.JumpHost.User)
	assert.Equal(t, 22, cfg.JumpHost.Port)
	assert.Nil(t, cfg.JumpHost.JumpHost, "jump host itself should have no further jump")
}

// TestResolveSSHConfig_NoProxyJump_JumpHostNil verifies that hosts without ProxyJump
// return SSHConfig.JumpHost == nil.
func TestResolveSSHConfig_NoProxyJump_JumpHostNil(t *testing.T) {
	sshCfgPath := writeTempSSHConfig(t, "direct-host", "direct.example.com", "user", 22)
	cfg, err := resolveSSHConfig("direct-host", nil, sshCfgPath)
	require.NoError(t, err)
	assert.Nil(t, cfg.JumpHost)
}

// TestResolveSSHConfig_ProxyJump_JumpHostNotFound verifies that an error is returned
// when the ProxyJump alias cannot be resolved.
func TestResolveSSHConfig_ProxyJump_JumpHostNotFound(t *testing.T) {
	dir := t.TempDir()
	sshCfgPath := filepath.Join(dir, "config")
	content := `Host target-host
    HostName target.internal
    User admin
    ProxyJump nonexistent-jump
`
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(content), 0600))

	_, err := resolveSSHConfig("target-host", nil, sshCfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving proxy jump")
}

// TestResolveSSHConfig_ProxyCommand verifies that a ProxyCommand directive in
// ~/.ssh/config causes the returned SSHConfig.ProxyCommand to be populated.
func TestResolveSSHConfig_ProxyCommand(t *testing.T) {
	dir := t.TempDir()
	sshCfgPath := filepath.Join(dir, "config")
	content := `Host bastion
    HostName bastion.example.com
    User admin
    ProxyCommand gcloud compute start-iap-tunnel bastion %p --listen-on-stdin --project=my-proj
`
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(content), 0600))

	cfg, err := resolveSSHConfig("bastion", nil, sshCfgPath)
	require.NoError(t, err)
	assert.Equal(t, "bastion.example.com", cfg.Host)
	assert.Equal(t, "gcloud compute start-iap-tunnel bastion %p --listen-on-stdin --project=my-proj", cfg.ProxyCommand)
	assert.Nil(t, cfg.JumpHost)
}

// TestResolveSSHConfig_RelativeIdentityFile verifies that a relative IdentityFile path
// (e.g. ".ssh/id_ed25519" without "~/") is expanded relative to HOME.
func TestResolveSSHConfig_RelativeIdentityFile(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)

	dir := t.TempDir()
	sshCfgPath := filepath.Join(dir, "config")
	content := `Host myhost
    HostName myhost.example.com
    User admin
    IdentityFile .ssh/id_ed25519
`
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(content), 0600))

	cfg, err := resolveSSHConfig("myhost", nil, sshCfgPath)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".ssh", "id_ed25519"), cfg.KeyFile)
}

// writeTempSSHConfig writes a minimal SSH config file with a Host block and returns the path.
func writeTempSSHConfig(t *testing.T, name, hostname, user string, port int) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "config")
	content := "Host " + name + "\n"
	content += "    HostName " + hostname + "\n"
	content += "    User " + user + "\n"
	if port != 0 {
		content += "    Port " + itoa(port) + "\n"
	}
	require.NoError(t, os.WriteFile(f, []byte(content), 0600))
	return f
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

// TestResolveSSHConfig_FromSSHConfigMap verifies that resolveSSHConfig returns
// the correct values when the connection is found in the yaml sshConns map.
func TestResolveSSHConfig_FromSSHConfigMap(t *testing.T) {
	conns := map[string]config.SSHConnection{
		"prod": {Host: "prod.example.com", Port: 2222, User: "deploy", KeyFile: "/home/user/.ssh/id_rsa"},
	}
	cfg, err := resolveSSHConfig("prod", conns, filepath.Join(t.TempDir(), "no-config"))
	require.NoError(t, err)
	assert.Equal(t, "prod.example.com", cfg.Host)
	assert.Equal(t, 2222, cfg.Port)
	assert.Equal(t, "deploy", cfg.User)
	assert.Equal(t, "/home/user/.ssh/id_rsa", cfg.KeyFile)
}

// TestResolveSSHConfig_FallbackToSSHConfig verifies the ~/.ssh/config fallback path:
// host is resolved, port defaults to 22, and IdentityFile with ~/ is expanded.
func TestResolveSSHConfig_FallbackToSSHConfig(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(sshDir, 0700))

	sshCfgPath := filepath.Join(sshDir, "config")
	content := "Host myserver\n    HostName 10.0.0.1\n    User admin\n    IdentityFile ~/.ssh/id_ed25519\n"
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(content), 0600))

	// Point the home-directory seam at our temp dir so ~/ expansion uses it
	homedir.SetForTest(t, home)

	cfg, err := resolveSSHConfig("myserver", nil, sshCfgPath)
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", cfg.Host)
	assert.Equal(t, 22, cfg.Port) // default when not set
	assert.Equal(t, "admin", cfg.User)
	// IdentityFile should have ~/ expanded to home
	assert.Equal(t, filepath.Join(home, ".ssh", "id_ed25519"), cfg.KeyFile)
}

// TestResolveSSHConfig_PortFromSSHConfig verifies that an explicit Port in the
// ssh config file is used as-is.
func TestResolveSSHConfig_PortFromSSHConfig(t *testing.T) {
	sshCfgPath := writeTempSSHConfig(t, "myhost", "myhost.example.com", "myuser", 2222)
	cfg, err := resolveSSHConfig("myhost", nil, sshCfgPath)
	require.NoError(t, err)
	assert.Equal(t, 2222, cfg.Port)
}

// TestResolveSSHConfig_NotFound verifies that a missing connection returns an error.
func TestResolveSSHConfig_NotFound(t *testing.T) {
	sshCfgPath := writeTempSSHConfig(t, "other", "other.example.com", "user", 0)
	_, err := resolveSSHConfig("does-not-exist", nil, sshCfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCreateSession_SSHFallbackToSSHConfig(t *testing.T) {
	// pane.Connection "ssh-alias" not in sshConns map → should be found in ~/.ssh/config
	sshConfigPath := writeTempSSHConfig(t, "ssh-alias", "ssh.example.com", "alice", 0)

	pane := &config.PaneConfig{
		ID:         "test-ssh-fallback",
		Type:       "ssh",
		Connection: "ssh-alias",
	}

	// This should not fail — the SSH connection is resolved from ssh config
	// (it will fail to connect, but that's a network issue, not a config one)
	// We only test that the error is NOT "not found"
	_, err := createSession(pane, map[string]config.SSHConnection{}, sshConfigPath)
	// The error (if any) should be a connection error, not "not found"
	if err != nil {
		assert.NotContains(t, err.Error(), "not found")
	}
}

func TestCreateSession_SSHFallback_NotInSSHConfig_Errors(t *testing.T) {
	sshConfigPath := writeTempSSHConfig(t, "other-host", "other.example.com", "bob", 0)

	pane := &config.PaneConfig{
		ID:         "test-ssh-missing",
		Type:       "ssh",
		Connection: "does-not-exist",
	}
	_, err := createSession(pane, map[string]config.SSHConnection{}, sshConfigPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCreateSession_SSHTmuxFallbackToSSHConfig(t *testing.T) {
	sshConfigPath := writeTempSSHConfig(t, "tmux-host", "tmux.example.com", "carol", 0)

	pane := &config.PaneConfig{
		ID:          "test-ssh-tmux-fallback",
		Type:        "ssh_tmux",
		Connection:  "tmux-host",
		TmuxSession: "main",
	}
	_, err := createSession(pane, map[string]config.SSHConnection{}, sshConfigPath)
	// Expect connection error, not "not found"
	if err != nil {
		assert.NotContains(t, err.Error(), "not found")
	}
}

// TestCreateSession_SSH_ShellPassedToConfig verifies Shell from PaneConfig flows to SSHConfig.
func TestCreateSession_SSH_ShellPassedToConfig(t *testing.T) {
	sshConfigPath := writeTempSSHConfig(t, "myhost", "myhost.example.com", "user", 22)
	pane := &config.PaneConfig{
		ID:         "test-shell",
		Type:       "ssh",
		Connection: "myhost",
		Shell:      "/usr/bin/zsh",
	}
	// This will fail to connect (no real SSH server) but we only care it's not "not found"
	_, err := createSession(pane, map[string]config.SSHConnection{}, sshConfigPath)
	if err != nil {
		assert.NotContains(t, err.Error(), "not found")
	}
}

func TestCreateSession_SSHConfigPort_UsedWhenSet(t *testing.T) {
	// Port 2 in test — won't actually connect, but let's verify it's read from config
	dir := t.TempDir()
	f := filepath.Join(dir, "config")
	content := "Host myhost\n    HostName myhost.example.com\n    User myuser\n    Port 2222\n"
	require.NoError(t, os.WriteFile(f, []byte(content), 0600))

	pane := &config.PaneConfig{
		ID:         "test-port",
		Type:       "ssh",
		Connection: "myhost",
	}
	_, err := createSession(pane, map[string]config.SSHConnection{}, f)
	// We only care that "not found" is NOT returned
	if err != nil {
		assert.NotContains(t, err.Error(), "not found")
	}
}

// resolveSSHConfig expands an IdentityFile against the home directory and does
// not report a failure to resolve it — the ssh config's other fields are still
// usable. Issue #212 asked for that swallow to be decided rather than
// inherited, and what it must not do is join against an empty home: that turned
// "~/.ssh/id_ed25519" into ".ssh/id_ed25519", inventing a path that names a
// real, ordinary directory relative to wherever panemux was started.
//
// Leaving the value as the ssh config wrote it is only half the answer, and on
// its own it is not a safety property: an unexpanded "~/.ssh/id_ed25519" and an
// already-relative ".ssh/id_ed25519" are both still relative, and os.ReadFile
// resolves either against the working directory. What makes the failure safe is
// buildAuthMethods' requireAbsolutePath guard refusing to read a path that is
// not absolute — see
// TestBuildAuthMethods_NonAbsoluteKeyFile_IsRefusedRatherThanReadFromTheWorkingDirectory,
// which plants a readable key at both paths to show the read would otherwise
// succeed. This test pins the half that belongs here: no invented path.
func TestResolveSSHConfig_UnresolvableHomeDirectory_LeavesTheIdentityFileAlone(t *testing.T) {
	for name, identityFile := range map[string]string{
		"tilde prefixed": "~/.ssh/id_ed25519",
		"relative":       ".ssh/id_ed25519",
	} {
		t.Run(name, func(t *testing.T) {
			homedir.SetFailingForTest(t, errNoHomeDir)

			sshCfgPath := filepath.Join(t.TempDir(), "config")
			content := "Host myhost\n    HostName myhost.example.com\n    User admin\n    IdentityFile " +
				identityFile + "\n"
			require.NoError(t, os.WriteFile(sshCfgPath, []byte(content), 0600))

			cfg, err := resolveSSHConfig("myhost", nil, sshCfgPath)

			require.NoError(t, err, "one unexpandable path must not fail the whole lookup")
			assert.Equal(t, identityFile, cfg.KeyFile,
				"the path must be left as the ssh config wrote it, not rebuilt against an empty home")
			require.Error(t, requireAbsolutePath("key file", cfg.KeyFile),
				"and the guard at the read must be what refuses it")
			assert.Equal(t, "myhost.example.com", cfg.Host, "the rest of the entry is still usable")
		})
	}
}

// TestResolveSSHConfig_MergesSSHConnectionsOverSSHConfig covers an
// ssh_connections entry whose name also has a ~/.ssh/config Host block: every
// field the entry leaves empty comes from the block, and every field it sets
// wins over the block's value for the same setting.
func TestResolveSSHConfig_MergesSSHConnectionsOverSSHConfig(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	sshCfgPath := filepath.Join(t.TempDir(), "config")
	content := `Host jump
    HostName jump.example.com
    User jumper

Host box
    HostName box.example.com
    User fileuser
    Port 2200
    IdentityFile ~/.ssh/id_file
    ProxyJump jump
    ProxyCommand nc %h %p
`
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(content), 0600))

	fromFile := SSHConfig{
		Host:         "box.example.com",
		User:         "fileuser",
		Port:         2200,
		KeyFile:      filepath.Join(home, ".ssh", "id_file"),
		ProxyCommand: "nc %h %p",
		JumpHost:     &SSHConfig{Host: "jump.example.com", User: "jumper", Port: 22},
	}
	tests := []struct {
		modify func(*SSHConfig)
		name   string
		conn   config.SSHConnection
	}{
		{func(*SSHConfig) {}, "name only", config.SSHConnection{}},
		{
			func(c *SSHConfig) { c.Host, c.ProxyCommand, c.JumpHost = "yaml.example.com", "", nil },
			"host, which also drops the block's route", config.SSHConnection{Host: "yaml.example.com"},
		},
		{func(c *SSHConfig) { c.User = "yamluser" }, "user", config.SSHConnection{User: "yamluser"}},
		{func(c *SSHConfig) { c.Port = 2222 }, "port", config.SSHConnection{Port: 2222}},
		{func(c *SSHConfig) { c.KeyFile = "/keys/yaml" }, "key file", config.SSHConnection{KeyFile: "/keys/yaml"}},
		{func(c *SSHConfig) { c.Password = "secret" }, "password", config.SSHConnection{Password: "secret"}},
		{
			func(c *SSHConfig) { c.KnownHostsFile = "/kh/yaml" },
			"known hosts file", config.SSHConnection{KnownHostsFile: "/kh/yaml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := fromFile
			jump := *fromFile.JumpHost
			want.JumpHost = &jump
			tt.modify(&want)

			got, err := resolveSSHConfig("box", map[string]config.SSHConnection{"box": tt.conn}, sshCfgPath)

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

// TestResolveSSHConfig_NameOnlyEntryWithoutSSHConfigHost_Errors verifies that
// an entry with no host, and no ~/.ssh/config Host block to take one from, is
// refused by name rather than dialed with an empty address.
func TestResolveSSHConfig_NameOnlyEntryWithoutSSHConfigHost_Errors(t *testing.T) {
	sshCfgPath := writeTempSSHConfig(t, "other", "other.example.com", "user", 0)
	conns := map[string]config.SSHConnection{"lonely": {User: "deploy"}}

	_, err := resolveSSHConfig("lonely", conns, sshCfgPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `ssh connection "lonely" has no host`)
}

// TestResolveSSHConfig_SSHConnectionsEntryWithUnreadableSSHConfig verifies
// that an entry carrying its own host still resolves when ~/.ssh/config cannot
// be read (here, the path is a directory), while a name-only entry reports
// that it has no host.
func TestResolveSSHConfig_SSHConnectionsEntryWithUnreadableSSHConfig(t *testing.T) {
	unreadable := t.TempDir()
	conns := map[string]config.SSHConnection{
		"full":      {Host: "full.example.com", User: "deploy"},
		"name-only": {},
	}

	cfg, err := resolveSSHConfig("full", conns, unreadable)
	require.NoError(t, err)
	assert.Equal(t, SSHConfig{Host: "full.example.com", User: "deploy"}, cfg)

	_, err = resolveSSHConfig("name-only", conns, unreadable)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `ssh connection "name-only" has no host`)
	assert.Contains(t, err.Error(), "is a directory", "the read error must be reported, not taken as a missing block")
}

// TestResolveSSHConfig_NameOnlyEntry_ProxyJumpNotFound verifies that a jump
// host that cannot be resolved fails a name-only entry too.
func TestResolveSSHConfig_NameOnlyEntry_ProxyJumpNotFound(t *testing.T) {
	sshCfgPath := filepath.Join(t.TempDir(), "config")
	content := "Host target\n    HostName target.internal\n    ProxyJump missing-jump\n"
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(content), 0600))

	_, err := resolveSSHConfig("target", map[string]config.SSHConnection{"target": {}}, sshCfgPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving proxy jump")
}

// TestResolveSSHConfig_EntryWithItsOwnHostTakesNoRouteFromTheHostBlock pins
// that an ssh_connections entry setting its own host neither inherits nor
// resolves the ProxyJump and ProxyCommand of a Host block of the same name.
// Resolving a ProxyJump panemux cannot follow (user@host here) used to fail an
// entry that connected on its YAML values alone, and the block's ProxyCommand
// would have run with the YAML host substituted for %h.
func TestResolveSSHConfig_EntryWithItsOwnHostTakesNoRouteFromTheHostBlock(t *testing.T) {
	sshCfgPath := filepath.Join(t.TempDir(), "config")
	content := `Host prod
    HostName prod.internal
    User fileuser
    Port 2200
    ProxyJump ops@bastion.example.com
    ProxyCommand nc %h %p
`
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(content), 0600))
	conns := map[string]config.SSHConnection{"prod": {Host: "10.0.0.5", User: "deploy"}}

	cfg, err := resolveSSHConfig("prod", conns, sshCfgPath)

	require.NoError(t, err)
	assert.Equal(t, SSHConfig{Host: "10.0.0.5", User: "deploy", Port: 2200}, cfg)
}

// TestResolveSSHConfig_ProxyJumpCycle_IsAnError verifies that a ProxyJump
// chain that comes back to a name already on it is refused, rather than
// recursing until the stack overflows and takes the whole server down.
func TestResolveSSHConfig_ProxyJumpCycle_IsAnError(t *testing.T) {
	tests := []struct {
		conns   map[string]config.SSHConnection
		name    string
		content string
		chain   string
	}{
		{
			name:    "self",
			content: "Host a\n    HostName a.example\n    ProxyJump a\n",
			chain:   "proxy jump cycle: a -> a",
		},
		{
			name:    "mutual",
			content: "Host a\n    HostName a.example\n    ProxyJump b\n\nHost b\n    HostName b.example\n    ProxyJump a\n",
			chain:   "proxy jump cycle: a -> b -> a",
		},
		{
			name:    "through name-only entries",
			conns:   map[string]config.SSHConnection{"a": {}, "b": {}},
			content: "Host a\n    HostName a.example\n    ProxyJump b\n\nHost b\n    HostName b.example\n    ProxyJump a\n",
			chain:   "proxy jump cycle: a -> b -> a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sshCfgPath := filepath.Join(t.TempDir(), "config")
			require.NoError(t, os.WriteFile(sshCfgPath, []byte(tt.content), 0600))

			_, err := resolveSSHConfig("a", tt.conns, sshCfgPath)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.chain)
		})
	}
}

// TestResolveSSHConfig_RelativeSSHConfigPath_IsNotRead verifies that an SSH
// config path that is not absolute — what sshconfig.DefaultPath returns when
// the home directory cannot be resolved — is never read. Read, it would be
// whatever .ssh/config the working directory holds, and its ProxyCommand runs
// through /bin/sh.
func TestResolveSSHConfig_RelativeSSHConfigPath_IsNotRead(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".ssh"), 0700))
	content := "Host planted\n    HostName planted.example\n    ProxyCommand touch pwned\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".ssh", "config"), []byte(content), 0600))
	t.Chdir(dir)
	relative := filepath.Join(".ssh", "config")

	_, err := resolveSSHConfig("planted", nil, relative)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `ssh connection "planted" not found`)

	_, err = resolveSSHConfig("planted", map[string]config.SSHConnection{"planted": {}}, relative)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `ssh connection "planted" has no host`)
	assert.Contains(t, err.Error(), "not absolute")
}
