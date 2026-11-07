package agmsg

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/hostexec"
)

type settingsFixtureRunner struct{ drop bool }

func (r settingsFixtureRunner) Run(ctx context.Context, script string, args ...string) ([]byte, error) {
	if r.drop {
		script = "unset AGMSG_CONFIG\n" + script
	}
	return hostexec.RunLocal(ctx, observeLoginEnvironment+script, args...)
}

func TestSettingsFixtureGoAndBashAgree(t *testing.T) {
	values := []string{"", "=", "==", "a=b=", " \tUnicode-日本語\r\\'\"$(literal)`literal` "}
	for _, newline := range []string{"", "\n"} {
		for _, value := range values {
			t.Run("newline="+newline+"/value="+value, func(t *testing.T) {
				startupEnvironment(t)
				file := filepath.Join(t.TempDir(), "settings.env")
				require.NoError(t, os.WriteFile(file, []byte("# data only\nAGMSG_CONFIG="+value+newline), 0600))
				loadPreservedAgmsgSettings(t, file)
				root := settingsObserverRoot(t)
				for _, drop := range []bool{false, true} {
					client := NewRemoteAgmsgClient("local", root, settingsFixtureRunner{drop: drop})
					err := client.Send(context.Background(), "team", "from", "to", "body")
					if drop {
						require.Error(t, err, "missing final setting must fail, including an empty value")
					} else {
						require.NoError(t, err, "literal fixture values must be identical in Go and Bash")
					}
				}
			})
		}
	}
}

func settingsObserverRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "scripts"), 0700))
	observer, err := filepath.Abs("testdata/self-name/observer.sh")
	require.NoError(t, err)
	require.NoError(t, os.Symlink(observer, filepath.Join(root, "scripts", "send.sh")))
	return root
}
