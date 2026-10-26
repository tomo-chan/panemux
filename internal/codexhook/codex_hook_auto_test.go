package codexhook

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/homedir"
	"panemux/internal/host"
	"panemux/internal/hosttools"
)

func TestCodexHookAutoStoreKeepsTheHostsAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panemux", "codex-hook.json")
	store := hosttools.NewCodexHookAutoStore(path)
	on, err := store.Enabled("")
	require.NoError(t, err)
	assert.False(t, on, "off without a file")

	require.NoError(t, store.Set("", true))
	require.NoError(t, store.Set("build-box", true))
	require.NoError(t, store.Set("build-box", true))
	require.NoError(t, store.Set("other", true))
	require.NoError(t, store.Set("other", false))

	restarted := hosttools.NewCodexHookAutoStore(path)
	for host, want := range map[string]bool{"": true, "build-box": true, "other": false, "never": false} {
		got, readErr := restarted.Enabled(host)
		require.NoError(t, readErr)
		assert.Equal(t, want, got, host)
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":1,"auto_update":["","build-box"]}`, string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, restarted.Set("", false))
	require.NoError(t, restarted.Set("build-box", false))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":1,"auto_update":[]}`, string(data))
}

func TestCodexHookAutoStoreDefaultPath(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	require.NoError(t, hosttools.NewCodexHookAutoStore("").Set("", true))
	_, err := os.Stat(filepath.Join(home, ".config", "panemux", "codex-hook.json"))
	require.NoError(t, err)

	homedir.SetFailingForTest(t, os.ErrNotExist)
	_, err = hosttools.NewCodexHookAutoStore("").Enabled("")
	require.Error(t, err)
}

func TestCodexHookAutoStoreLeavesAFileItCannotReadAlone(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":      `{"auto_update":`,
		"other version": `{"version":2,"auto_update":[""]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "codex-hook.json")
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			store := hosttools.NewCodexHookAutoStore(path)
			_, err := store.Enabled("")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "codex hook settings file")
			require.Error(t, store.Set("", true))
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, content, string(data), "never written over")
		})
	}
}

// outdatedCodexHome is a HOME whose hook is registered but differs from the
// one panemux embeds, so its menu shows Update.
func outdatedCodexHome(t *testing.T) (home, hookPath string) {
	t.Helper()
	home = codexHome(t, "")
	hookPath = filepath.Join(home, codexHookFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(hookPath), 0o700))
	require.NoError(t, os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 0\n"), 0o600))
	hooksJSON := `{"hooks":{"PermissionRequest":[{"hooks":[{"type":"command","command":"` +
		hookPath + `","timeout":1}]}]}}`
	require.NoError(t, os.WriteFile(filepath.Join(home, ".codex", "hooks.json"), []byte(hooksJSON), 0o600))
	homedir.SetForTest(t, home)
	return home, hookPath
}

// countingRunLocal runs scripts as panemux does and counts the hook writes.
type countingRunLocal struct {
	mu     sync.Mutex
	writes int
}

func (c *countingRunLocal) run(ctx context.Context, script string) ([]byte, error) {
	if strings.Contains(script, "PANEMUX_HOOK_") {
		c.mu.Lock()
		c.writes++
		c.mu.Unlock()
	}
	return runLocal(ctx, script)
}

func (c *countingRunLocal) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes
}

func autoService(
	t *testing.T, store *hosttools.CodexHookAutoStore, run func(context.Context, string) ([]byte, error),
) *testService {
	t.Helper()
	s := newTestService(testOptions{
		Hosts: func() []string { return nil }, RunLocal: run, CodexHookAuto: store, Logf: t.Logf,
	})
	t.Cleanup(s.Close)
	return s
}

func TestCodexHookUpdatesByItselfOnlyWhenTurnedOn(t *testing.T) {
	_, hookPath := outdatedCodexHome(t)
	store := hosttools.NewCodexHookAutoStore(filepath.Join(t.TempDir(), "codex-hook.json"))
	runs := &countingRunLocal{}
	s := autoService(t, store, runs.run)

	got := codexHookStatus(t, s, "")
	assert.Equal(t, hosttools.State{Status: hosttools.Outdated}, got, "off by default")
	assert.Zero(t, runs.count())

	require.NoError(t, s.SetCodexHookAuto("", true))
	got = codexHookStatus(t, s, "")
	assert.Equal(t, hosttools.State{Status: hosttools.Installed, AutoUpdate: true, AutoUpdated: true}, got)
	data, err := os.ReadFile(hookPath)
	require.NoError(t, err)
	assert.Equal(t, codexPermissionHook, string(data))

	// A manual Update afterwards forgets what was done by itself.
	got, err = s.InstallCodexHook(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, hosttools.State{Status: hosttools.Installed, AutoUpdate: true}, got)
	assert.Equal(t, 1, runs.count())
}

func TestCodexHookDoesNotInstallByItself(t *testing.T) {
	home := codexHome(t, "")
	homedir.SetForTest(t, home)
	store := hosttools.NewCodexHookAutoStore(filepath.Join(t.TempDir(), "codex-hook.json"))
	require.NoError(t, store.Set("", true))
	runs := &countingRunLocal{}
	s := autoService(t, store, runs.run)

	assert.Equal(t, hosttools.State{Status: hosttools.Missing, AutoUpdate: true}, codexHookStatus(t, s, ""))
	assert.Zero(t, runs.count(), "a host without the hook is never installed to")
}

func TestCodexHookUpdateByItselfThatFailsIsNotRepeated(t *testing.T) {
	_, hookPath := outdatedCodexHome(t)
	// The hook's path is a directory: the check reads no hook file, and the
	// write fails.
	require.NoError(t, os.Remove(hookPath))
	require.NoError(t, os.Mkdir(hookPath, 0o700))
	store := hosttools.NewCodexHookAutoStore(filepath.Join(t.TempDir(), "codex-hook.json"))
	require.NoError(t, store.Set("", true))
	runs := &countingRunLocal{}
	s := autoService(t, store, runs.run)

	got := codexHookStatus(t, s, "")
	assert.Equal(t, hosttools.Outdated, got.Status)
	assert.True(t, got.AutoUpdate)
	assert.False(t, got.AutoUpdated)
	assert.Contains(t, got.AutoError, "install the codex hook")
	assert.Equal(t, 1, runs.count())

	again := codexHookStatus(t, s, "")
	assert.Equal(t, got, again, "the failure is still shown")
	assert.Equal(t, 1, runs.count(), "and not tried again")

	_, err := s.InstallCodexHook(context.Background(), "")
	require.Error(t, err)
	assert.Equal(t, 2, runs.count(), "Update can still be pressed")
}

func TestCodexHookUpdatesByItselfAfterTheFirstCollection(t *testing.T) {
	_, hookPath := outdatedCodexHome(t)
	store := hosttools.NewCodexHookAutoStore(filepath.Join(t.TempDir(), "codex-hook.json"))
	require.NoError(t, store.Set("", true))
	runs := &countingRunLocal{}
	s := autoService(t, store, func(ctx context.Context, script string) ([]byte, error) {
		return runs.run(ctx, script)
	})

	s.manager.AfterCollection(host.Local)
	s.manager.Wait()
	data, err := os.ReadFile(hookPath)
	require.NoError(t, err)
	assert.Equal(t, codexPermissionHook, string(data), "updated without the menu")
	assert.Equal(t, 1, runs.count())

	// Later collections check nothing more.
	require.NoError(t, os.WriteFile(hookPath, []byte("#!/bin/sh\n"), 0o600))
	s.manager.AfterCollection(host.Local)
	s.manager.Wait()
	assert.Equal(t, 1, runs.count())
}

func TestCodexHookChecksOnceAfterStart(t *testing.T) {
	homedir.SetForTest(t, codexHome(t, ""))
	store := hosttools.NewCodexHookAutoStore(filepath.Join(t.TempDir(), "codex-hook.json"))
	require.NoError(t, store.Set("", true))
	var mu sync.Mutex
	checks := 0
	s := autoService(t, store, func(ctx context.Context, script string) ([]byte, error) {
		if script == codexHookReadScript {
			mu.Lock()
			checks++
			mu.Unlock()
		}
		return runLocal(ctx, script)
	})

	s.manager.AfterCollection(host.Local)
	s.manager.Wait()
	s.manager.AfterCollection(host.Local)
	s.manager.Wait()
	s.manager.AfterCollection(host.Local)
	s.manager.Wait()
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, checks, "only the first collection is followed by a check")
}

func TestCodexHookChecksNothingAfterACollectionWhenTurnedOff(t *testing.T) {
	outdatedCodexHome(t)
	store := hosttools.NewCodexHookAutoStore(filepath.Join(t.TempDir(), "codex-hook.json"))
	var scripts []string
	s := autoService(t, store, func(_ context.Context, script string) ([]byte, error) {
		scripts = append(scripts, script)
		return nil, nil
	})
	s.manager.AfterCollection(host.Local)
	s.manager.Wait()
	assert.Empty(t, scripts, "no hook check after a collection when turned off")
}

func TestCodexHookAutoSettingErrors(t *testing.T) {
	s := newTestService(testOptions{Hosts: func() []string { return nil }})
	defer s.Close()
	require.ErrorIs(t, s.SetCodexHookAuto("nope", true), hosttools.ErrUnknownHost)
	require.Error(t, s.SetCodexHookAuto("", true), "no store, nothing kept")

	path := filepath.Join(t.TempDir(), "codex-hook.json")
	require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))
	home := codexHome(t, "")
	homedir.SetForTest(t, home)
	bad := autoService(t, hosttools.NewCodexHookAutoStore(path), runLocal)
	got := codexHookStatus(t, bad, "")
	assert.Equal(t, hosttools.Missing, got.Status)
	assert.False(t, got.AutoUpdate)
	assert.Empty(t, got.AutoError, "no update was tried")
	assert.Contains(t, got.AutoSettingsError, "codex hook settings file", "a settings file that cannot be read says so")
}

func TestCodexHookAutoStoreReportsAFileItCannotOpen(t *testing.T) {
	path := t.TempDir() // a directory where the file should be
	_, err := hosttools.NewCodexHookAutoStore(path).Enabled("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading codex hook settings file")
}
