package agmsg

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/cachedir"
	"panemux/internal/homedir"
	"panemux/internal/host"
	"panemux/internal/hostexec"
)

func TestSuccessfulGitWarningsDoNotContaminateSourceData(t *testing.T) {
	for _, stage := range []string{"rev-parse", "archive", "both"} {
		t.Run(stage, func(t *testing.T) {
			dir := isolatedCommandPath(t)
			cachedir.SetForTest(t, t.TempDir())
			payload := "fixture contents\x00\xff\n"
			archive := installerFixture(t, payload)
			path := filepath.Join(t.TempDir(), "source.tar")
			require.NoError(t, os.WriteFile(path, archive, 0600))
			t.Setenv("PANEMUX_TEST_ARCHIVE", path)
			script := `#!/bin/sh
if [ "$2" = '` + stage + `' ] || [ '` + stage + `' = both ]; then
  printf 'warning: sample git notice\n' >&2
fi
case "$2" in
  rev-parse) printf '%s\n' '` + TestedAgmsgCommit + `' ;;
  archive) /bin/cat "$PANEMUX_TEST_ARCHIVE" ;;
  *) exit 2 ;;
esac
`
			//nolint:gosec // executable git fixture is confined to the temporary PATH
			require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0700))
			execution := hostexec.New(hostexec.Options{})
			defer execution.Close()
			tool := NewTool(ToolOptions{Execution: execution, Input: execution})
			got, err := tool.opts.Archive(context.Background())
			require.NoError(t, err, "successful git warnings must not fail commit verification")
			require.Equal(t, archive, got, "only stdout belongs in the archive stream")
			reader := tar.NewReader(bytes.NewReader(got))
			header, err := reader.Next()
			require.NoError(t, err)
			require.Equal(t, "install.sh", header.Name)
			contents, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.Equal(t, payload, string(contents))
			_, err = reader.Next()
			require.ErrorIs(t, err, io.EOF)
		})
	}
}

func isolatedCommandPath(t *testing.T) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Symlink(sh, filepath.Join(dir, "sh")))
	homedir.SetForTest(t, t.TempDir())
	t.Setenv("PATH", dir)
	return dir
}

func TestLocalPrerequisitesPreserveMissingToolNames(t *testing.T) {
	isolatedCommandPath(t)
	execution := hostexec.New(hostexec.Options{})
	defer execution.Close()
	tool := NewTool(ToolOptions{Execution: execution, Input: execution, Archive: func(context.Context) ([]byte, error) {
		return nil, nil
	}})
	err := tool.Prerequisites(host.Local)[0].Check(context.Background())
	for _, name := range []string{"bash", "sqlite3", "tar", "mktemp", "node"} {
		require.ErrorContains(t, err, "Missing "+name)
	}
	require.ErrorContains(t, err, "install it on this host")
}

func TestLocalGitFailuresRetainSourceDiagnostics(t *testing.T) {
	for _, stage := range []string{"init", "fetch", "rev-parse", "archive"} {
		t.Run(stage, func(t *testing.T) {
			dir := isolatedCommandPath(t)
			cachedir.SetForTest(t, t.TempDir())
			script := `#!/bin/sh
case "$2" in
  ` + stage + `) printf 'fatal: source diagnostic\n' >&2; exit 128 ;;
  rev-parse) `
			if stage == "archive" {
				script += "printf '%s\\n' '" + TestedAgmsgCommit + "';;"
			} else {
				script += "exit 1;;"
			}
			script += "\nesac\n"
			//nolint:gosec // executable git fixture is confined to the temporary PATH
			require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0700))
			_, err := (&Source{}).Archive(context.Background())
			require.ErrorContains(t, err, "fatal: source diagnostic")
			require.ErrorContains(t, err, "agmsg")
			require.NotContains(t, err.Error(), "task collection")
		})
	}
}

func TestNonReleaseProvenanceIsNotInstalledOrSilentlyDowngraded(t *testing.T) {
	for _, version := range []string{"v1.2.0-6-g1a2b3c4", "v1.2.0-dirty", "v1.2.0-6-g1a2b3c4-dirty"} {
		e := &toolExecution{probe: "installed\n" + version}
		tool := NewTool(ToolOptions{Execution: e, Input: e, Archive: func(context.Context) ([]byte, error) {
			return nil, nil
		}})
		require.Equal(t, "unknown", string(tool.Check(context.Background(), host.Local).Status))
		require.NotEmpty(t, VersionMismatchWarning("sample", version))
		_, err := tool.Install(context.Background(), host.Local)
		require.Error(t, err)
		require.Zero(t, e.inputs)
	}
}
