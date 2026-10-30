package agmsg

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/host"
	"panemux/internal/hosttools"
)

type toolExecution struct {
	probe  string
	err    error
	script string
	args   []string
	inputs int
}

func (e *toolExecution) Run(context.Context, host.Host, string, ...string) ([]byte, error) {
	return []byte(e.probe), e.err
}
func (e *toolExecution) RunInput(
	_ context.Context, _ host.Host, script string, in io.Reader, args ...string,
) ([]byte, error) {
	if script == targetPrerequisites {
		return []byte(e.probe), e.err
	}
	e.inputs++
	e.script, e.args = script, args
	_, err := io.ReadAll(in)
	if err != nil {
		return nil, err
	}
	return []byte("installer output"), e.err
}

func TestAgmsgToolStatesAndInstallSafety(t *testing.T) {
	for _, tc := range []struct {
		probe  string
		status hosttools.Status
		mode   string
	}{
		{"missing\n", hosttools.Missing, ""},
		{"installed\n1.1.9\n", hosttools.Outdated, "--update"},
		{"installed\nv1.2.0\n", hosttools.Installed, "refuse"},
		{"installed\n1.2.1\n", hosttools.Newer, "refuse"},
		{"installed\nunknown\n", hosttools.Unknown, "refuse"},
		{"alternate\n", hosttools.Invalid, "refuse"},
	} {
		t.Run(tc.probe, func(t *testing.T) {
			e := &toolExecution{probe: tc.probe}
			tool := NewTool(ToolOptions{Execution: e, Input: e, Archive: func(context.Context) ([]byte, error) {
				return []byte("archive"), nil
			}})
			state := tool.Check(context.Background(), host.Local)
			require.Equal(t, tc.status, state.Status)
			_, err := tool.Install(context.Background(), host.Local)
			if tc.mode == "refuse" {
				require.Error(t, err)
				require.Zero(t, e.inputs)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, e.inputs)
			require.Equal(t, []string{tc.mode}, e.args)
			require.Contains(t, e.script, "mktemp")
			require.Contains(t, e.script, "</dev/null")
		})
	}
}

func TestAgmsgToolPreservesFailureOutputAndSourceFailure(t *testing.T) {
	e := &toolExecution{probe: "missing\n"}
	tool := NewTool(ToolOptions{Execution: e, Input: e, Archive: func(context.Context) ([]byte, error) {
		return nil, errors.New("source unavailable")
	}})
	_, err := tool.Install(context.Background(), host.Local)
	require.ErrorContains(t, err, "source unavailable")
	require.Zero(t, e.inputs)
}

func TestPinnedWarningIncludesNewerPatch(t *testing.T) {
	require.NotEmpty(t, VersionMismatchWarning("demo", "1.2.1"))
	require.Equal(t, "3c537a51bb76a23e312e7c27770df4a58ba794ff", TestedAgmsgCommit)
}

func TestToolReleaseOrderingAtEveryComponent(t *testing.T) {
	want := agmsgRelease{1, 2, 5}
	for _, tc := range []struct {
		status hosttools.Status
		got    agmsgRelease
	}{
		{hosttools.Installed, agmsgRelease{1, 2, 5}},
		{hosttools.Outdated, agmsgRelease{0, 9, 9}},
		{hosttools.Outdated, agmsgRelease{1, 1, 9}},
		{hosttools.Outdated, agmsgRelease{1, 2, 4}},
		{hosttools.Newer, agmsgRelease{1, 2, 6}},
		{hosttools.Newer, agmsgRelease{1, 3, 0}},
		{hosttools.Newer, agmsgRelease{2, 0, 0}},
	} {
		require.Equal(t, tc.status, toolReleaseStatus(tc.got, want))
	}
}
