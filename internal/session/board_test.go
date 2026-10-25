package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLocalSession_BoardHostID_ReturnsLocal(t *testing.T) {
	s := &LocalSession{}
	assert.Equal(t, "local", s.BoardHostID())
}

func TestTmuxLocalSession_BoardHostID_ReturnsLocal(t *testing.T) {
	s := &TmuxLocalSession{}
	assert.Equal(t, "local", s.BoardHostID())
}

func TestSSHSession_BoardHostID_ReturnsConnectionName(t *testing.T) {
	s := &SSHSession{connectionName: "my-server"}
	assert.Equal(t, "my-server", s.BoardHostID())
}

func TestTmuxSSHSession_BoardHostID_ReturnsConnectionName(t *testing.T) {
	s := &TmuxSSHSession{connectionName: "remote-box"}
	assert.Equal(t, "remote-box", s.BoardHostID())
}
