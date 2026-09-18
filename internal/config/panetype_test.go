package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPaneTypeConstantsAreTheDocumentedWireValues pins the four wire values
// themselves. They are part of config.yaml's user-facing schema and of the API
// contract the frontend's Zod schemas parse, so centralizing them must not
// change what they are.
func TestPaneTypeConstantsAreTheDocumentedWireValues(t *testing.T) {
	assert.Equal(t, "local", PaneTypeLocal)
	assert.Equal(t, "ssh", PaneTypeSSH)
	assert.Equal(t, "tmux", PaneTypeTmux)
	assert.Equal(t, "ssh_tmux", PaneTypeSSHTmux)
}

// TestPaneTypesListsEveryConstantExactlyOnce keeps the list validation reads
// from in step with the constants: a pane type added as a constant but left out
// of the list would validate as invalid, and a duplicate would hide a typo.
func TestPaneTypesListsEveryConstantExactlyOnce(t *testing.T) {
	assert.Equal(
		t,
		[]string{PaneTypeLocal, PaneTypeSSH, PaneTypeTmux, PaneTypeSSHTmux},
		PaneTypes(),
	)
}

// TestPaneTypesReturnsACopy guards the exported list against a caller mutating
// the package's own state through it.
func TestPaneTypesReturnsACopy(t *testing.T) {
	got := PaneTypes()
	require.NotEmpty(t, got)
	got[0] = "mutated"

	assert.Equal(t, PaneTypeLocal, PaneTypes()[0])
}

// TestValidatePaneAcceptsEveryPaneTypeAndRejectsTheRest covers both directions
// of the type check: every centralized constant validates, and a value outside
// the list still produces the unchanged error message.
func TestValidatePaneAcceptsEveryPaneTypeAndRejectsTheRest(t *testing.T) {
	sshConns := map[string]SSHConnection{"host1": {Host: "example.test", User: "demo"}}

	for _, paneType := range PaneTypes() {
		t.Run(paneType, func(t *testing.T) {
			pane := &PaneConfig{ID: "p1", Type: paneType, Connection: "host1", TmuxSession: "work"}

			errs := validatePane(pane, sshConns)

			for _, e := range errs {
				assert.NotContains(t, e, "invalid type")
			}
		})
	}

	for _, invalid := range []string{"", "Local", "ssh-tmux", "unknown"} {
		t.Run(fmt.Sprintf("invalid %q", invalid), func(t *testing.T) {
			pane := &PaneConfig{ID: "p1", Type: invalid, Connection: "host1", TmuxSession: "work"}

			errs := validatePane(pane, sshConns)

			assert.Contains(
				t,
				errs,
				fmt.Sprintf("pane %q has invalid type %q: must be local, ssh, tmux, or ssh_tmux", "p1", invalid),
			)
		})
	}
}
