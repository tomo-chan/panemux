package agmsg

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/host"
	"panemux/internal/hosttools"
)

type changingToolExecution struct {
	toolExecution
	probes int
}

func (e *changingToolExecution) Run(context.Context, host.Host, string, ...string) ([]byte, error) {
	e.probes++
	if e.probes < 3 {
		return []byte("installed\n1.1.9\n"), nil
	}
	return []byte("missing\n"), nil
}

func TestAutomaticUpdateNeverBecomesInstallAfterFinalProbe(t *testing.T) {
	e := &changingToolExecution{}
	tool := NewTool(ToolOptions{Execution: e, Input: e,
		Archive: func(context.Context) ([]byte, error) { return []byte("archive"), nil }})
	s := hosttools.New(hosttools.Options{
		Tools:      map[hosttools.ID]hosttools.Tool{hosttools.AgmsgTool: tool},
		GlobalAuto: map[hosttools.ID]func() bool{hosttools.AgmsgTool: func() bool { return true }},
	})
	defer s.Close()
	s.StartAutoUpdates(hosttools.AgmsgTool)
	s.Wait()
	require.Zero(t, e.inputs, "automatic Update must never invoke Install if the final probe finds a missing copy")
	got, err := s.Check(context.Background(), hosttools.AgmsgTool, host.Local)
	require.NoError(t, err)
	require.Contains(t, got.AutoError, "missing")
}

func TestAgmsgUpdateOnlyUsesThePinnedUpdatePath(t *testing.T) {
	e := &toolExecution{probe: "installed\n1.1.9\n"}
	tool := NewTool(ToolOptions{Execution: e, Input: e,
		Archive: func(context.Context) ([]byte, error) { return []byte("archive"), nil }})
	_, err := tool.Update(context.Background(), host.Local)
	require.NoError(t, err)
	require.Equal(t, 1, e.inputs)
	require.Equal(t, []string{updateFlag}, e.args)
	require.Equal(t, installerScript, e.script)
}
