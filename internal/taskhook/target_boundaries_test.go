package taskhook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContextPathBudgetAndInstalledMetadataFailClosed(t *testing.T) {
	for _, size := range []int{4096, 4097} {
		path := "/" + strings.Repeat("x", size-1)
		require.Equal(t, size == 4096, identityPath(path))
	}
	target := TargetIdentity{Version: 1, UID: os.Geteuid(), AccountHome: t.TempDir(),
		CodexHome: "/workspace/account/.codex", Machine: "linux-machine-id:0123456789abcdef0123456789abcdef"}
	paths, err := ContextPaths(target)
	require.NoError(t, err)
	require.ErrorIs(t, validateInstalledTarget("wrong-root", target), ErrInput)
	require.ErrorIs(t, validateInstalledTarget(paths.State, target), ErrStorage)
	bad := target
	bad.UID = -1
	_, err = ContextPaths(bad)
	require.ErrorIs(t, err, ErrInput)
	require.NoError(t, os.MkdirAll(paths.State, 0700))
	meta := filepath.Join(paths.Context, "install.json")
	require.ErrorIs(t, validateInstalledTarget(paths.State, target), ErrStorage)
	require.NoError(t, os.WriteFile(meta, []byte("private-corrupt"), 0600))
	require.ErrorIs(t, validateInstalledTarget(paths.State, target), ErrCorrupt)
	target.Context, err = ContextID(target)
	require.NoError(t, err)
	data, err := json.Marshal(map[string]any{"target": target, "helper_protocol": ProtocolVersion})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(meta, data, 0600))
	require.NoError(t, validateInstalledTarget(paths.State, target))
	require.NoError(t, os.Chmod(paths.State, 0755))
	require.ErrorIs(t, validateInstalledTarget(paths.State, target), ErrStorage)
}

func TestRegistrationPathLimitAndProtocolFactsCannotBeRelabeled(t *testing.T) {
	entry := registration()
	for _, size := range []int{4096, 4097} {
		entry.SessionsRoot = "/" + strings.Repeat("x", size-1)
		require.Equal(t, size == 4096, validRegistration(entry))
	}
}
