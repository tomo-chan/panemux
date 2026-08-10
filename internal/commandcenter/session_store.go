package commandcenter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const sessionFileName = "command-center-session.json"
const sessionFileMode os.FileMode = 0600

// SessionState is the command center's own persisted --resume continuity: a
// single fixed Claude session id reused across every later query. See
// docs/agent-board.md's Process lifecycle section.
type SessionState struct {
	SessionID string `json:"session_id"`
}

// LoadSessionFile reads a previously persisted SessionState. A missing file
// is the normal state before the first successful query, not an error, and
// returns the zero value.
func LoadSessionFile(path string) (SessionState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SessionState{}, nil
		}
		return SessionState{}, fmt.Errorf("reading command center session file: %w", err)
	}
	var state SessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return SessionState{}, fmt.Errorf("parsing command center session file: %w", err)
	}
	return state, nil
}

// SaveSessionFile persists state, creating the parent directory if needed,
// via a temp file plus rename so a crash mid-write can never leave a
// truncated file behind.
func SaveSessionFile(path string, state SessionState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encoding command center session file: %w", err)
	}
	return atomicWriteFile(path, data, sessionFileMode, "command center session file")
}

// DefaultSessionFilePath returns ~/.config/panemux/command-center-session.json.
func DefaultSessionFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".config", "panemux", sessionFileName), nil
}
