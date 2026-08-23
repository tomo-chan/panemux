package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/session"
)

// The browser shim is process-wide state the session factory reads, so
// startup has to push the operator's url_open setting into it.
func TestApplyBrowserShimSetting(t *testing.T) {
	prev := session.BrowserShimEnabled()
	t.Cleanup(func() { session.SetBrowserShimEnabled(prev) })

	tests := []struct {
		name string
		yaml string
		want bool
	}{
		{name: "default", yaml: "server:\n  port: 8080\n", want: true},
		{name: "disabled", yaml: "server:\n  port: 8080\nurl_open:\n  browser_shim: false\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o600))
			cfg, err := config.Load(path)
			require.NoError(t, err)

			session.SetBrowserShimEnabled(!tt.want) // ensure the call is what changes it
			session.SetBrowserShimEnabled(cfg.BrowserShimEnabled())

			require.Equal(t, tt.want, session.BrowserShimEnabled())
		})
	}
}
