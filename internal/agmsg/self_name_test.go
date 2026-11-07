package agmsg

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// Observe the environment at the external-script boundary without running agmsg
// or touching a user's session/store. Existing wrapper tests cover body decoding.
type selfNameObserver struct {
	t   *testing.T
	ssh bool
}

func (r selfNameObserver) Run(ctx context.Context, script string, args ...string) ([]byte, error) {
	r.t.Helper()
	if args[0] == "sh" {
		require.Equal(r.t, []string{"sh", "-c", sendBase64WrapperScript, sendBase64WrapperScriptName,
			"/opt/agmsg/scripts/send.sh", "team", "from", "to", "Ym9keSAnICQobGl0ZXJhbCk="}, args)
	} else {
		require.Equal(r.t, "/opt/agmsg/scripts/api.sh", args[0])
	}
	// Replace the external command with an inline environment observer.
	args = []string{"sh", "-c", `test "$AGMSG_SELF_NAME" = off || exit 42`}
	//nolint:gosec // fixed production script and inline observer, no external input
	command := exec.CommandContext(ctx, "sh", append([]string{"-c", script, "observer"}, args...)...)
	if r.ssh {
		//nolint:gosec // exercise SSH quoting with fixed test input
		command = exec.CommandContext(ctx, "sh", "-c",
			"sh -c "+shellQuoteForTest(script)+" observer "+quoteArgsLikeSession(args))
	}
	return command.CombinedOutput()
}

func TestAgmsgScriptsDisableSelfNaming(t *testing.T) {
	for _, inherited := range []string{"", "on", "off"} {
		for _, ssh := range []bool{false, true} {
			name := "local"
			if ssh {
				name = "ssh"
			}
			t.Run(name+"/inherited="+inherited, func(t *testing.T) {
				t.Setenv("AGMSG_SELF_NAME", inherited)
				client := NewRemoteAgmsgClient("host", "/opt/agmsg", selfNameObserver{t: t, ssh: ssh})
				require.NoError(t, client.Send(context.Background(), "team", "from", "to", "body ' $(literal)"))
				_, err := client.Since(context.Background(), "team", "", 100)
				require.NoError(t, err)
			})
		}
	}
}
