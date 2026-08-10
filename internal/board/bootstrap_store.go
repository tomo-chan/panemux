package board

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const bootstrapStateFileName = "board-bootstrap-state.json"
const bootstrapStateFileMode os.FileMode = 0600

// LoadBootstrapState reads the pane IDs the bootstrap watcher had already
// written its onboarding instruction to as of the last save. A missing file
// is the normal state before the first successful save, not an error, and
// returns a nil slice.
func LoadBootstrapState(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading bootstrap state file: %w", err)
	}
	var paneIDs []string
	if err := json.Unmarshal(data, &paneIDs); err != nil {
		return nil, fmt.Errorf("parsing bootstrap state file: %w", err)
	}
	return paneIDs, nil
}

// SaveBootstrapState persists paneIDs, creating the parent directory if
// needed. The write goes through a temp file plus rename, not a direct
// WriteFile, so a crash or power loss mid-write can never leave a truncated
// or half-written state file on disk — a rename onto an existing path is
// atomic on the platforms panemux targets, unlike a direct write.
func SaveBootstrapState(path string, paneIDs []string) error {
	data, err := json.Marshal(paneIDs)
	if err != nil {
		return fmt.Errorf("encoding bootstrap state file: %w", err)
	}
	return atomicWriteFile(path, data, bootstrapStateFileMode, "bootstrap state file")
}

// DefaultBootstrapStateFilePath returns ~/.config/panemux/board-bootstrap-state.json.
func DefaultBootstrapStateFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".config", "panemux", bootstrapStateFileName), nil
}
