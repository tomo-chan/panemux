package hosttools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/stretchr/testify/require"
	"log"
	"os"
	"panemux/internal/host"
	"path/filepath"
	"testing"
)

type fakeTool struct {
	fail               error
	state              *State
	checked, installed []host.Host
}

func (f *fakeTool) Check(_ context.Context, h host.Host) State {
	f.checked = append(f.checked, h)
	if f.state != nil {
		return *f.state
	}
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

// Management owns these transitions; run them against a tool port fake so
// package-scoped mutation checks verify the owner rather than an adapter.
func TestServiceCodexAutomaticUpdateAndManualReset(t *testing.T) {
	for _, name := range []host.Host{host.Local, "build-box"} {
		for _, fails := range []bool{false, true} {
			for _, defaultLog := range []bool{false, true} {
				t.Run(fmt.Sprintf("host=%q/fails=%t/defaultLog=%t", name, fails, defaultLog), func(t *testing.T) {
					tool := &fakeTool{state: &State{Status: Outdated}}
					if fails {
						tool.fail = errors.New("write refused")
					}
					var logs bytes.Buffer
					options := Options{
						Hosts:         func() []string { return []string{"build-box"} },
						Tools:         map[ID]Tool{CodexHookTool: tool},
						CodexHookAuto: NewCodexHookAutoStore(filepath.Join(t.TempDir(), "codex-hook.json")),
					}
					if defaultLog {
						writer := log.Writer()
						log.SetOutput(&logs)
						defer log.SetOutput(writer)
					} else {
						options.Logf = func(format string, args ...any) { fmt.Fprintf(&logs, format, args...) }
					}
					service := New(options)
					defer service.Close()
					require.NoError(t, service.SetCodexHookAuto(string(name), true))
					got, err := service.Check(context.Background(), CodexHookTool, name)
					require.NoError(t, err)
					require.True(t, got.AutoUpdate)
					require.Equal(t, !fails, got.AutoUpdated)
					if fails {
						require.Equal(t, Outdated, got.Status)
						require.Equal(t, "write refused", got.AutoError)
						require.Contains(t, logs.String(), "failed: write refused")
					} else {
						require.Equal(t, Installed, got.Status)
						require.Empty(t, got.AutoError)
						require.Contains(t, logs.String(), "updated it by itself")
					}
					label := string(name)
					if name == host.Local {
						label = "the panemux host"
					}
					require.Contains(t, logs.String(), "on "+label)
					require.Equal(t, []host.Host{name}, tool.installed)
					_, err = service.Check(context.Background(), CodexHookTool, name)
					require.NoError(t, err)
					require.Len(t, tool.installed, 1, "only one automatic attempt per process")
					tool.fail = nil
					manual, err := service.Install(context.Background(), CodexHookTool, name)
					require.NoError(t, err)
					require.Equal(t, State{Status: Installed, AutoUpdate: true}, manual, "manual install clears either outcome")
					again, err := service.Check(context.Background(), CodexHookTool, name)
					require.NoError(t, err)
					require.Empty(t, again.AutoError)
					require.False(t, again.AutoUpdated)
					require.Len(t, tool.installed, 2, "manual install does not re-enable automatic retries")
				})
			}
		}
	}
}

func TestServiceCodexAutoSettingsErrors(t *testing.T) {
	service := New(Options{})
	defer service.Close()
	require.ErrorIs(t, service.SetCodexHookAuto("unknown", true), ErrUnknownHost)
	require.EqualError(t, service.SetCodexHookAuto("", true), "codex hook settings are not kept")
	path := filepath.Join(t.TempDir(), "codex-hook.json")
	require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))
	tool := &fakeTool{state: &State{Status: Outdated}}
	broken := New(Options{Tools: map[ID]Tool{CodexHookTool: tool}, CodexHookAuto: NewCodexHookAutoStore(path)})
	defer broken.Close()
	require.ErrorContains(t, broken.SetCodexHookAuto("", true), "parsing codex hook settings file")
	got, err := broken.Check(context.Background(), CodexHookTool, host.Local)
	require.NoError(t, err)
	require.Contains(t, got.AutoSettingsError, "parsing codex hook settings file")
	require.Empty(t, tool.installed)
}
