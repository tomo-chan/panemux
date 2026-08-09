package board

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// quoteArgsLikeSession mirrors internal/session's quoteArgs/shellQuotePath
// exactly (single-quote each argument, escaping embedded single quotes,
// join with spaces). It is duplicated here — rather than imported — because
// internal/board deliberately does not depend on internal/session; this
// test exists specifically to prove sendBase64WrapperScript survives that
// real quoting discipline when actually interpreted by a POSIX shell, not
// just against a fake executor.
func quoteArgsLikeSession(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

// TestSendBase64WrapperScript_RealShell_DecodesMetacharacterBody proves the
// full round trip end to end: RemoteAgmsgClient.Send's argument list, once
// single-quote-escaped exactly as internal/session's RunBoardCommand would
// escape it and handed to a real /bin/sh, decodes the base64-encoded body
// back to its original bytes (including shell metacharacters) before the
// stand-in send.sh script ever sees it — the property the whole
// base64-wrapper design exists to guarantee.
func TestSendBase64WrapperScript_RealShell_DecodesMetacharacterBody(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if _, err := exec.LookPath("base64"); err != nil {
		t.Skip("base64 not available")
	}

	dir := t.TempDir()
	capturePath := filepath.Join(dir, "captured-argv")
	sendStub := filepath.Join(dir, "send.sh")

	// A stand-in for agmsg's real send.sh: dumps its argv, NUL-separated,
	// to capturePath so the test can inspect exactly what it received.
	stubScript := "#!/bin/sh\nprintf '%s\\0' \"$@\" > " + shellQuoteForTest(capturePath) + "\n"
	//nolint:gosec // G306: test fixture needs the exec bit
	require.NoError(t, os.WriteFile(sendStub, []byte(stubScript), 0700))

	body := `it's; $(evil) && ` + "`echo hi`" + ` | rm -rf / ; "double" 'single'`
	encoded := base64.StdEncoding.EncodeToString([]byte(body))

	args := []string{
		"sh", "-c", sendBase64WrapperScript, sendBase64WrapperScriptName,
		sendStub, "team-a", "from-a", "to-a", encoded,
	}
	cmd := quoteArgsLikeSession(args)

	out, err := exec.CommandContext(context.Background(), "sh", "-c", cmd).CombinedOutput()
	require.NoError(t, err, "shell command failed: %s", string(out))

	captured, err := os.ReadFile(capturePath)
	require.NoError(t, err)
	fields := strings.Split(strings.TrimSuffix(string(captured), "\x00"), "\x00")

	require.Equal(t, []string{"team-a", "from-a", "to-a", body, "--force"}, fields)
}

func shellQuoteForTest(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}
