package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPath_EndsWithSSHConfig(t *testing.T) {
	p := DefaultPath()
	assert.True(t, strings.HasSuffix(p, filepath.Join(".ssh", "config")),
		"expected path to end with .ssh/config, got %s", p)
}

func TestParseHosts_MultipleBlocks(t *testing.T) {
	content := `Host prod-web
    HostName prod.example.com
    User admin
    Port 2222
    IdentityFile ~/.ssh/prod_rsa

Host dev
    HostName dev.example.com
    User developer
`
	f := writeTempSSHConfig(t, content)
	hosts, err := ParseHosts(f)
	require.NoError(t, err)
	require.Len(t, hosts, 2)

	assert.Equal(t, "prod-web", hosts[0].Name)
	assert.Equal(t, "prod.example.com", hosts[0].Hostname)
	assert.Equal(t, "admin", hosts[0].User)
	assert.Equal(t, 2222, hosts[0].Port)
	assert.Equal(t, "~/.ssh/prod_rsa", hosts[0].IdentityFile)

	assert.Equal(t, "dev", hosts[1].Name)
	assert.Equal(t, "dev.example.com", hosts[1].Hostname)
	assert.Equal(t, "developer", hosts[1].User)
	assert.Equal(t, 0, hosts[1].Port)
	assert.Equal(t, "", hosts[1].IdentityFile)
}

func TestParseHosts_WildcardExcluded(t *testing.T) {
	content := `Host *
    ServerAliveInterval 60

Host myserver
    HostName myserver.example.com
    User ubuntu
`
	f := writeTempSSHConfig(t, content)
	hosts, err := ParseHosts(f)
	require.NoError(t, err)
	require.Len(t, hosts, 1)
	assert.Equal(t, "myserver", hosts[0].Name)
}

func TestParseHosts_MissingHostname_UsesName(t *testing.T) {
	content := `Host myalias
    User ubuntu
`
	f := writeTempSSHConfig(t, content)
	hosts, err := ParseHosts(f)
	require.NoError(t, err)
	require.Len(t, hosts, 1)
	assert.Equal(t, "myalias", hosts[0].Hostname)
}

func TestParseHosts_FileNotFound_ReturnsEmpty(t *testing.T) {
	hosts, err := ParseHosts("/nonexistent/path/ssh/config")
	assert.NoError(t, err)
	assert.Empty(t, hosts)
}

func TestParseHosts_EmptyFile(t *testing.T) {
	f := writeTempSSHConfig(t, "")
	hosts, err := ParseHosts(f)
	require.NoError(t, err)
	assert.Empty(t, hosts)
}

func TestParseHosts_CaseInsensitiveKeys(t *testing.T) {
	content := `Host myserver
    hostname myserver.example.com
    user ubuntu
    port 22
`
	f := writeTempSSHConfig(t, content)
	hosts, err := ParseHosts(f)
	require.NoError(t, err)
	require.Len(t, hosts, 1)
	assert.Equal(t, "myserver.example.com", hosts[0].Hostname)
	assert.Equal(t, "ubuntu", hosts[0].User)
	assert.Equal(t, 22, hosts[0].Port)
}

func TestAppendHost_WritesCorrectBlock(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config")

	h := Host{
		Name:         "staging",
		Hostname:     "staging.example.com",
		User:         "deploy",
		Port:         2222,
		IdentityFile: "~/.ssh/staging_rsa",
	}
	err := AppendHost(f, h)
	require.NoError(t, err)

	data, err := os.ReadFile(f)
	require.NoError(t, err)
	s := string(data)
	assert.Contains(t, s, "Host staging\n")
	assert.Contains(t, s, "    HostName staging.example.com\n")
	assert.Contains(t, s, "    User deploy\n")
	assert.Contains(t, s, "    Port 2222\n")
	assert.Contains(t, s, "    IdentityFile ~/.ssh/staging_rsa\n")
}

func TestAppendHost_NoPortOrIdentityFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config")

	h := Host{
		Name:     "simple",
		Hostname: "simple.example.com",
		User:     "ubuntu",
	}
	err := AppendHost(f, h)
	require.NoError(t, err)

	data, err := os.ReadFile(f)
	require.NoError(t, err)
	s := string(data)
	assert.Contains(t, s, "Host simple\n")
	assert.Contains(t, s, "    HostName simple.example.com\n")
	assert.Contains(t, s, "    User ubuntu\n")
	assert.NotContains(t, s, "Port")
	assert.NotContains(t, s, "IdentityFile")
}

func TestAppendHost_AppendsToExistingFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config")

	// Write initial content
	err := os.WriteFile(f, []byte("Host existing\n    User old\n"), 0600)
	require.NoError(t, err)

	h := Host{Name: "new", Hostname: "new.example.com", User: "newuser"}
	err = AppendHost(f, h)
	require.NoError(t, err)

	data, err := os.ReadFile(f)
	require.NoError(t, err)
	s := string(data)
	// Both blocks should exist
	assert.Contains(t, s, "Host existing")
	assert.Contains(t, s, "Host new")
}

func TestParseHosts_AfterAppend(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config")

	h := Host{Name: "myhost", Hostname: "myhost.example.com", User: "myuser", Port: 22}
	require.NoError(t, AppendHost(f, h))

	hosts, err := ParseHosts(f)
	require.NoError(t, err)
	require.Len(t, hosts, 1)
	assert.Equal(t, "myhost", hosts[0].Name)
	assert.Equal(t, "myhost.example.com", hosts[0].Hostname)
	assert.Equal(t, "myuser", hosts[0].User)
	assert.Equal(t, 22, hosts[0].Port)
}

func TestParseHosts_ProxyJump(t *testing.T) {
	content := `Host jump
    HostName jump.example.com
    User jumpuser

Host target
    HostName target.internal
    User admin
    ProxyJump jump
`
	f := writeTempSSHConfig(t, content)
	hosts, err := ParseHosts(f)
	require.NoError(t, err)
	require.Len(t, hosts, 2)

	assert.Equal(t, "jump", hosts[0].Name)
	assert.Equal(t, "", hosts[0].ProxyJump)

	assert.Equal(t, "target", hosts[1].Name)
	assert.Equal(t, "jump", hosts[1].ProxyJump)
}

func TestParseHosts_ProxyCommand(t *testing.T) {
	content := `Host bastion
    HostName bastion.example.com
    User admin
    ProxyCommand gcloud compute start-iap-tunnel bastion %p --listen-on-stdin --project=my-project --zone=us-central1-a
`
	f := writeTempSSHConfig(t, content)
	hosts, err := ParseHosts(f)
	require.NoError(t, err)
	require.Len(t, hosts, 1)
	assert.Equal(t, "gcloud compute start-iap-tunnel bastion %p --listen-on-stdin --project=my-project --zone=us-central1-a", hosts[0].ProxyCommand)
	assert.Equal(t, "", hosts[0].ProxyJump)
}

func TestParseHosts_NoProxyJump_Empty(t *testing.T) {
	content := `Host myserver
    HostName myserver.example.com
    User ubuntu
`
	f := writeTempSSHConfig(t, content)
	hosts, err := ParseHosts(f)
	require.NoError(t, err)
	require.Len(t, hosts, 1)
	assert.Equal(t, "", hosts[0].ProxyJump)
}

// writeTempSSHConfig creates a temp file with the given content and returns the path.
func writeTempSSHConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(f, []byte(content), 0600))
	return f
}
