package board

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseIdentity(t *testing.T) {
	for _, tc := range []struct{ raw, name, host string }{
		{"worker.1", "worker.1", ""}, {SystemID, SystemID, ""},
		{"worker@local", "worker", "local"},
		{"worker@ssh:local", "worker", "ssh:local"},
		{"A_1.-@ssh:build.1-x_y", "A_1.-", "ssh:build.1-x_y"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			id, err := ParseIdentity(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.name, id.Name)
			assert.Equal(t, tc.host, id.Host)
			assert.Equal(t, tc.raw, id.String())
		})
	}
	for _, raw := range []string{
		"", "@local", "w@", "w@@local", "w@local@local", "w@build", "w@ssh:", "w@ssh:a:b", "w@ssh:a/b",
		"w@LOCAL", "w x", "w:local", "w@ssh:$(id)", "w\n", "_system@local", "_system@ssh:build",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseIdentity(raw)
			require.Error(t, err)
		})
	}
}
