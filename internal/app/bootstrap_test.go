package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBootstrapDesktopUsesLoopbackWithEphemeralPort(t *testing.T) {
	t.Parallel()

	cfgPath := writeConfig(t)

	runtime, err := Bootstrap(Options{
		ConfigPath: cfgPath,
		Mode:       ModeDesktop,
	}, fstest.MapFS{
		"frontend/dist/index.html": {Data: []byte("ok")},
	})
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1", runtime.Config.Server.Host)
	assert.Equal(t, 0, runtime.Config.Server.Port)
	assert.Regexp(t, `^http://127\.0\.0\.1:\d+$`, runtime.BaseURL)
}

func TestBootstrapBrowserUsesConfiguredAddress(t *testing.T) {
	t.Parallel()

	cfgPath := writeConfig(t)

	runtime, err := Bootstrap(Options{
		ConfigPath: cfgPath,
		Mode:       ModeBrowser,
		Port:       9191,
	}, fstest.MapFS{
		"frontend/dist/index.html": {Data: []byte("ok")},
	})
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1", runtime.Config.Server.Host)
	assert.Equal(t, 9191, runtime.Config.Server.Port)
	assert.Equal(t, "http://127.0.0.1:9191", runtime.BaseURL)
}

func TestRuntimeStartAndShutdownWithDesktopListener(t *testing.T) {
	t.Parallel()

	cfgPath := writeConfig(t)

	runtime, err := Bootstrap(Options{
		ConfigPath: cfgPath,
		Mode:       ModeDesktop,
	}, fstest.MapFS{
		"frontend/dist/index.html": {Data: []byte("ok")},
	})
	require.NoError(t, err)

	runtime.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, runtime.Shutdown(ctx))

	err = <-runtime.Errors()
	assert.NoError(t, err)
}

func writeConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	config := []byte(`server:
  host: "127.0.0.1"
  port: 8080
workspaces:
  active: dev
  tab_position: top
  items:
    - id: dev
      title: "Development"
      layout:
        direction: horizontal
        children:
          - size: 100
            pane:
              id: "local-main"
              type: local
              shell: "/bin/sh"
              title: "Shell"
`)
	require.NoError(t, os.WriteFile(path, config, 0o600))
	return path
}
