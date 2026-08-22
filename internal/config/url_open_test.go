package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserShimEnabled_DefaultsToOnWhenUnset(t *testing.T) {
	cfg := &Config{}
	assert.True(t, cfg.BrowserShimEnabled())
}

func TestBrowserShimEnabled_RespectsExplicitValues(t *testing.T) {
	enabled := true
	disabled := false

	assert.True(t, (&Config{URLOpen: URLOpenConfig{BrowserShim: &enabled}}).BrowserShimEnabled())
	assert.False(t, (&Config{URLOpen: URLOpenConfig{BrowserShim: &disabled}}).BrowserShimEnabled())
}

func TestLoad_ReadsURLOpenBrowserShim(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want bool
	}{
		{
			name: "omitted block defaults to enabled",
			yaml: "server:\n  port: 8080\n",
			want: true,
		},
		{
			name: "explicitly disabled",
			yaml: "server:\n  port: 8080\nurl_open:\n  browser_shim: false\n",
			want: false,
		},
		{
			name: "explicitly enabled",
			yaml: "server:\n  port: 8080\nurl_open:\n  browser_shim: true\n",
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o600))

			cfg, err := Load(path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.BrowserShimEnabled())
		})
	}
}

// Saving a config that never set url_open must not start writing the block,
// so an operator's file is not rewritten with settings they did not choose.
func TestSaveLayout_LeavesURLOpenUnsetWhenItWasNeverConfigured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)

	require.NoError(t, cfg.SaveLayout(cfg.Layout))

	saved, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(saved), "url_open")
}

func TestSaveLayout_PreservesDisabledBrowserShim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(
		path, []byte("server:\n  port: 8080\nurl_open:\n  browser_shim: false\n"), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)

	require.NoError(t, cfg.SaveLayout(cfg.Layout))

	reloaded, err := Load(path)
	require.NoError(t, err)
	assert.False(t, reloaded.BrowserShimEnabled())
}
