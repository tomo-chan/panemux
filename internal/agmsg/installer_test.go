package agmsg

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/cachedir"
	"panemux/internal/homedir"
	"panemux/internal/host"
	"panemux/internal/hostexec"
	"panemux/internal/hosttools"
)

func installerFixture(t *testing.T, script string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	require.NoError(t, w.WriteHeader(&tar.Header{Name: "install.sh", Mode: 0700, Size: int64(len(script))}))
	_, err := w.Write([]byte(script))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func TestInstallerUsesArchiveAndIsolatedHome(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	execution := hostexec.New(hostexec.Options{})
	t.Cleanup(execution.Close)
	archive := installerFixture(t, `set -eu
test ! -t 0
if [ -f "$HOME/.agents/skills/agmsg/scripts/api.sh" ]; then
  test "$#" = 1 && test "$1" = --update
else
  test "$#" = 0
fi
mkdir -p "$HOME/.agents/skills/agmsg/scripts"
printf '#!/bin/sh\n' > "$HOME/.agents/skills/agmsg/scripts/api.sh"
printf '1.2.0\n' > "$HOME/.agents/skills/agmsg/VERSION"
printf 'owned\n' > "$HOME/.agents/skills/agmsg/.agmsg"
`)
	tool := NewTool(ToolOptions{Execution: execution, Input: execution,
		Archive: func(context.Context) ([]byte, error) { return archive, nil }})
	state, err := tool.Install(context.Background(), host.Local)
	require.NoError(t, err)
	require.Equal(t, hosttools.Installed, state.Status)
	require.Equal(t, "1.2.0", state.Version)
	versionPath := filepath.Join(home, ".agents", "skills", "agmsg", "VERSION")
	require.NoError(t, os.WriteFile(versionPath, []byte("1.1.0"), 0600))
	state, err = tool.Install(context.Background(), host.Local)
	require.NoError(t, err)
	require.Equal(t, hosttools.Installed, state.Status)
	other := filepath.Join(home, ".agents", "skills", "renamed")
	require.NoError(t, os.MkdirAll(other, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(other, ".agmsg"), nil, 0600))
	_, err = execution.RunInput(context.Background(), host.Local, installerScript, bytes.NewReader(archive), "--update")
	require.Error(t, err)
	_, err = execution.RunInput(context.Background(), host.Local, installerScript, bytes.NewReader(archive), "--cmd")
	require.Error(t, err)
}

func TestInstallerFailureRetainsExitAndOutput(t *testing.T) {
	homedir.SetForTest(t, t.TempDir())
	execution := hostexec.New(hostexec.Options{})
	t.Cleanup(execution.Close)
	archive := installerFixture(t, "echo 'manual recovery required' >&2; exit 7\n")
	tool := NewTool(ToolOptions{Execution: execution, Input: execution,
		Archive: func(context.Context) ([]byte, error) { return archive, nil }})
	_, err := tool.Install(context.Background(), host.Local)
	require.ErrorContains(t, err, "exit status 7")
	require.ErrorContains(t, err, "manual recovery required")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = tool.Install(ctx, host.Local)
	require.Error(t, err)
}

func TestToolProbeAndPrerequisiteFailures(t *testing.T) {
	cachedir.SetFailingForTest(t, errors.New("no cache"))
	defaultTool := NewTool(ToolOptions{})
	_, sourceErr := defaultTool.opts.Archive(context.Background())
	require.ErrorContains(t, sourceErr, "no cache")
	for _, probe := range []string{"nonsense", "installed\n", "installed\n1.0.0\n"} {
		e := &toolExecution{probe: probe}
		tool := NewTool(ToolOptions{Execution: e, Input: e, Archive: func(context.Context) ([]byte, error) {
			return nil, nil
		}})
		state := tool.Check(context.Background(), host.Local)
		require.NoError(t, hosttools.CheckPrerequisites(context.Background(), tool.Prerequisites(host.Local)))
		if probe != "installed\n1.0.0\n" {
			require.Equal(t, hosttools.Unknown, state.Status)
		}
	}
	e := &toolExecution{probe: "node missing", err: errors.New("exit status 1")}
	tool := NewTool(ToolOptions{Execution: e, Input: e, Archive: func(context.Context) ([]byte, error) {
		return nil, errors.New("offline")
	}})
	require.Equal(t, hosttools.Unknown, tool.Check(context.Background(), host.Local).Status)
	err := hosttools.CheckPrerequisites(context.Background(), tool.Prerequisites(host.Local))
	require.ErrorContains(t, err, "node missing")
	require.ErrorContains(t, err, "offline")
}
