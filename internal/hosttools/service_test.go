package hosttools

import (
	"context"
	"errors"
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"panemux/internal/host"
	"path/filepath"
	"testing"
)

type fakeTool struct {
	fail               error
	checked, installed []host.Host
}

func (f *fakeTool) Check(_ context.Context, h host.Host) State {
	f.checked = append(f.checked, h)
	return State{Status: Missing}
}
func (f *fakeTool) Install(_ context.Context, h host.Host) (State, error) {
	f.installed = append(f.installed, h)
	return State{Status: Installed}, f.fail
}

func TestServiceRoutesToolsAndValidatesHosts(t *testing.T) {
	first, second := &fakeTool{}, &fakeTool{}
	hosts := []string{"build-box"}
	s := New(Options{Hosts: func() []string { return hosts }, Tools: map[ID]Tool{"first": first, "second": second}})
	defer s.Close()
	s.AfterCollection(host.Local) // A registry without the Codex adapter has no background hook check.
	got, err := s.Check(context.Background(), "first", host.Local)
	require.NoError(t, err)
	require.Equal(t, Missing, got.Status)
	got, err = s.Install(context.Background(), "second", host.Host("build-box"))
	require.NoError(t, err)
	require.Equal(t, Installed, got.Status)
	require.Equal(t, []host.Host{host.Local}, first.checked)
	require.Equal(t, []host.Host{host.Host("build-box")}, second.installed)
	require.Empty(t, first.installed)
	require.Empty(t, second.checked)
	for _, operation := range []func() (State, error){
		func() (State, error) { return s.Check(context.Background(), "first", "unknown") },
		func() (State, error) { return s.Install(context.Background(), "first", "unknown") },
	} {
		_, err = operation()
		require.ErrorIs(t, err, ErrUnknownHost)
	}
	hosts = nil
	_, err = s.Check(context.Background(), "first", "build-box")
	require.ErrorIs(t, err, ErrUnknownHost)
	_, err = s.Check(context.Background(), "unknown", host.Local)
	require.ErrorIs(t, err, ErrUnknownTool)
	_, err = s.Install(context.Background(), "unknown", host.Local)
	require.ErrorIs(t, err, ErrUnknownTool)
	second.fail = errors.New("write failed")
	_, err = s.Install(context.Background(), "second", host.Local)
	require.ErrorIs(t, err, second.fail)
}

func TestAfterCollectionChecksOnceAndDoesNotInstallMissing(t *testing.T) {
	tool := &fakeTool{}
	store := NewCodexHookAutoStore(filepath.Join(t.TempDir(), "codex-hook.json"))
	require.NoError(t, store.Set("", true))
	s := New(Options{Tools: map[ID]Tool{CodexHookTool: tool}, CodexHookAuto: store})
	defer s.Close()
	s.AfterCollection(host.Local)
	s.Wait()
	s.AfterCollection(host.Local)
	s.Wait()
	require.Equal(t, []host.Host{host.Local}, tool.checked)
	require.Empty(t, tool.installed)
}

type blockingTool struct{ started chan struct{} }

func (f blockingTool) Check(ctx context.Context, _ host.Host) State {
	close(f.started)
	<-ctx.Done()
	return State{Status: Unknown, Error: ctx.Err().Error()}
}
func (f blockingTool) Install(context.Context, host.Host) (State, error) { panic("unexpected install") }
func TestCloseCancelsAndJoinsBackgroundToolCheck(t *testing.T) {
	tool := blockingTool{started: make(chan struct{})}
	store := NewCodexHookAutoStore(filepath.Join(t.TempDir(), "codex-hook.json"))
	require.NoError(t, store.Set("", true))
	s := New(Options{Tools: map[ID]Tool{CodexHookTool: tool}, CodexHookAuto: store})
	s.AfterCollection(host.Local)
	<-tool.started
	s.Close()
	s.AfterCollection("other")
	s.Close()
}

func TestCodexHookAutoStoreWritesThroughSymlinks(t *testing.T) {
	for _, relative := range []bool{false, true} {
		for _, exists := range []bool{false, true} {
			t.Run(fmt.Sprintf("relative=%t/exists=%t", relative, exists), func(t *testing.T) {
				dir := t.TempDir()
				target := filepath.Join(dir, "settings.json")
				link := filepath.Join(dir, "codex-hook.json")
				if exists {
					require.NoError(t, os.WriteFile(target, []byte(`{"version":1,"auto_update":[]}`), 0600))
				}
				dest := target
				if relative {
					dest = "settings.json"
				}
				require.NoError(t, os.Symlink(dest, link))
				require.NoError(t, NewCodexHookAutoStore(link).Set("build-box", true))
				got, err := os.Readlink(link)
				require.NoError(t, err)
				require.Equal(t, dest, got)
				on, err := NewCodexHookAutoStore(target).Enabled("build-box")
				require.NoError(t, err)
				require.True(t, on)
			})
		}
	}
}

func TestAfterCollectionWithoutSettingsOrAfterCloseDoesNotCheck(t *testing.T) {
	store := NewCodexHookAutoStore(filepath.Join(t.TempDir(), "codex-hook.json"))
	require.NoError(t, store.Set("", true))
	for _, closed := range []bool{false, true} {
		tool := &fakeTool{}
		options := Options{Tools: map[ID]Tool{CodexHookTool: tool}}
		if closed {
			options.CodexHookAuto = store
		}
		s := New(options)
		if closed {
			s.Close()
		}
		s.AfterCollection(host.Local)
		s.Wait()
		s.Close()
		require.Empty(t, tool.checked)
		_, err := s.Check(context.Background(), CodexHookTool, "unknown")
		require.ErrorIs(t, err, ErrUnknownHost)
	}
}
