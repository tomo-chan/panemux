package session

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"panemux/internal/config"
)

// TestSessionTypeConstantsMatchConfigPaneTypes pins the correspondence
// createSession depends on: it converts a PaneConfig's raw string with
// Type(pane.Type), so a session type constant that drifts from the config wire
// value stops matching any branch and falls through to "unknown session type".
func TestSessionTypeConstantsMatchConfigPaneTypes(t *testing.T) {
	assert.Equal(t, TypeLocal, Type(config.PaneTypeLocal))
	assert.Equal(t, TypeSSH, Type(config.PaneTypeSSH))
	assert.Equal(t, TypeTmux, Type(config.PaneTypeTmux))
	assert.Equal(t, TypeSSHTmux, Type(config.PaneTypeSSHTmux))
}

// TestEveryConfigPaneTypeHasASessionBranch is the other half: a pane type the
// config accepts but the factory has no branch for would be rejected at
// runtime after passing validation.
func TestEveryConfigPaneTypeHasASessionBranch(t *testing.T) {
	branches := map[Type]struct{}{
		TypeLocal:   {},
		TypeSSH:     {},
		TypeTmux:    {},
		TypeSSHTmux: {},
	}

	for _, paneType := range config.PaneTypes() {
		assert.Contains(t, branches, Type(paneType), "no session branch for pane type %q", paneType)
	}
}
