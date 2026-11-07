package agmsg

import (
	"context"
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

// shellMessageRunner exercises the actual adapter and the local/SSH shell boundary.
type shellMessageRunner struct {
	stub string
	ssh  bool
}

func (r shellMessageRunner) Run(ctx context.Context, script string, args ...string) ([]byte, error) {
	args = append([]string(nil), args...)
	args[4] = r.stub
	//nolint:gosec // fixed production script, validated arguments and a test-owned executable
	command := exec.CommandContext(ctx, "sh", append([]string{"-c", script, "observer"}, args...)...)
	if r.ssh {
		cmd := "sh -c " + shellQuoteForTest(script) + " observer " + quoteArgsLikeSession(args)
		//nolint:gosec // exercise SSH quoting with validated arguments
		command = exec.CommandContext(ctx, "sh", "-c", cmd)
	}
	return command.CombinedOutput()
}

// TestSendBase64WrapperScript_RealShell_DecodesMetacharacterBody proves the
// full round trip end to end: RemoteMessageClient.Send's argument list, once
// single-quote-escaped exactly as the host execution adapter would
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

	// A stand-in for message transport's real send.sh: dumps its argv, NUL-separated,
	// to capturePath so the test can inspect exactly what it received.
	stubScript := "#!/bin/sh\nprintf '%s\\0' \"$@\" > " + shellQuoteForTest(capturePath) + "\n"
	//nolint:gosec // G306: test fixture needs the exec bit
	require.NoError(t, os.WriteFile(sendStub, []byte(stubScript), 0700))

	body := `it's; $(evil) && ` + "`echo hi`" + ` | rm -rf / ; "double" 'single'` + "\n世界"
	for _, ssh := range []bool{false, true} {
		client := NewRemoteAgmsgClient("local", "/opt/agmsg", shellMessageRunner{stub: sendStub, ssh: ssh})
		require.NoError(t, client.Send(context.Background(), "team-a", "from.1@ssh:build.1", "to.2@local", body))

		captured, err := os.ReadFile(capturePath)
		require.NoError(t, err)
		fields := strings.Split(strings.TrimSuffix(string(captured), "\x00"), "\x00")

		require.Equal(t, []string{"team-a", "from.1@ssh:build.1", "to.2@local", body, "--force"}, fields)
	}
}

func shellQuoteForTest(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}
