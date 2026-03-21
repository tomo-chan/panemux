package session

import (
	"fmt"

	"panemux/internal/config"
)

// CreateFromConfig creates a Session from a PaneConfig and SSH connections map.
func CreateFromConfig(pane *config.PaneConfig, sshConns map[string]config.SSHConnection) (Session, error) {
	switch Type(pane.Type) {
	case TypeLocal:
		return NewLocal(pane.ID, pane.Shell, pane.Cwd, pane.Title)

	case TypeSSH:
		conn, ok := sshConns[pane.Connection]
		if !ok {
			return nil, fmt.Errorf("ssh connection %q not found", pane.Connection)
		}
		return NewSSH(pane.ID, pane.Title, SSHConfig{
			Host:     conn.Host,
			Port:     conn.Port,
			User:     conn.User,
			KeyFile:  conn.KeyFile,
			Password: conn.Password,
		})

	case TypeTmux:
		return NewTmuxLocal(pane.ID, pane.Title, pane.TmuxSession)

	case TypeSSHTmux:
		conn, ok := sshConns[pane.Connection]
		if !ok {
			return nil, fmt.Errorf("ssh connection %q not found", pane.Connection)
		}
		return NewTmuxSSH(pane.ID, pane.Title, pane.TmuxSession, SSHConfig{
			Host:     conn.Host,
			Port:     conn.Port,
			User:     conn.User,
			KeyFile:  conn.KeyFile,
			Password: conn.Password,
		})

	default:
		return nil, fmt.Errorf("unknown session type: %s", pane.Type)
	}
}
