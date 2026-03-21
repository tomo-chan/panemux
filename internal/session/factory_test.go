package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
)

func TestCreateFromConfig_Local(t *testing.T) {
	pane := &config.PaneConfig{
		ID:    "test-local",
		Type:  "local",
		Shell: "/bin/sh",
		Title: "Test",
	}
	sess, err := CreateFromConfig(pane, nil)
	require.NoError(t, err)
	defer sess.Close()

	assert.Equal(t, "test-local", sess.ID())
	assert.Equal(t, TypeLocal, sess.Type())
}

func TestCreateFromConfig_UnknownType(t *testing.T) {
	pane := &config.PaneConfig{
		ID:   "test",
		Type: "unknown",
	}
	_, err := CreateFromConfig(pane, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
}

func TestCreateFromConfig_SSHMissing(t *testing.T) {
	pane := &config.PaneConfig{
		ID:         "test-ssh",
		Type:       "ssh",
		Connection: "nonexistent",
	}
	_, err := CreateFromConfig(pane, map[string]config.SSHConnection{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
