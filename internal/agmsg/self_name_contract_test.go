package agmsg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/homedir"
	"panemux/internal/host"
	"panemux/internal/hostexec"
)

// Read only the public hook, never copy an install or read its private store.
func TestAgmsgContract_BoardSendDoesNotSuppressLaterRename(t *testing.T) {
	source := os.Getenv("PANEMUX_AGMSG_PATH")
	if source == "" {
		t.Skip("PANEMUX_AGMSG_PATH not set: public upstream hook contract is opt-in")
	}
	home := t.TempDir()
	homedir.SetForTest(t, home)
	fake := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(fake, "scripts", "lib"), 0700))
	for _, file := range []string{"type-registry", "terminal-registry", "role-session", "team-status",
		"codex-session-index", "safe-poke"} {
		require.NoError(t, os.WriteFile(filepath.Join(fake, "scripts", "lib", file+".sh"), nil, 0600))
	}
	observer, err := filepath.Abs("testdata/self-name/rename-observer.sh")
	require.NoError(t, err)
	require.NoError(t, os.Symlink(observer, filepath.Join(fake, "scripts", "send.sh")))
	mark, poke := filepath.Join(home, "mark"), filepath.Join(home, "poke")
	t.Setenv("PANEMUX_TEST_RENAME_HOOK", filepath.Join(source, "scripts", "lib", "self-rename.sh"))
	t.Setenv("PANEMUX_TEST_MARK", mark)
	t.Setenv("PANEMUX_TEST_POKE", poke)
	t.Setenv("SKILL_DIR", fake)
	t.Setenv("BASH_ENV", "")
	t.Setenv("ENV", "")
	t.Setenv("TMUX", "/workspace/fake-socket,123,0")
	t.Setenv("TMUX_PANE", "%7")
	pool := hostexec.New(hostexec.Options{})
	t.Cleanup(pool.Close)
	client := NewRemoteAgmsgClient("local", fake, host.Bind(pool, host.Local))
	require.NoError(t, client.Send(context.Background(), "team", "sender", "recipient", "body"))
	require.NoFileExists(t, mark, "board calls must not leave a durable skip mark")
	require.NoFileExists(t, poke, "board calls must not type a rename command")
	t.Setenv("AGMSG_SELF_NAME", "on")
	t.Setenv("AGMSG_SELF_RENAME", "on")
	//nolint:gosec // fixed repository observer, fake dependencies, opt-in public hook
	out, err := exec.CommandContext(context.Background(), observer, "team", "sender").CombinedOutput()
	require.NoError(t, err, string(out))
	require.FileExists(t, poke, "the sender's later enabled action must still rename")
	contents, err := os.ReadFile(mark)
	require.NoError(t, err)
	require.Contains(t, string(contents), "attempted")
}
