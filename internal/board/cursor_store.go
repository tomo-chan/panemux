package board

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const cursorFileName = "board-relay-cursor.json"
const cursorFileMode os.FileMode = 0600

// CursorEntry is one (host, team) relay poll cursor. Persisted as a JSON
// array rather than a map keyed by an encoded "host|team" string, since
// host names (SSH connection aliases in particular) and team names aren't
// constrained enough to guarantee any chosen delimiter is collision-free.
type CursorEntry struct {
	Host   string
	Team   string
	Cursor string
}

// LoadCursorFile reads previously persisted cursor entries. A missing file
// is the normal state before the relay's first successful save, not an
// error, and returns a nil slice.
func LoadCursorFile(path string) ([]CursorEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading relay cursor file: %w", err)
	}
	var entries []CursorEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing relay cursor file: %w", err)
	}
	return entries, nil
}

// SaveCursorFile persists entries, creating the parent directory if needed.
// The write goes through a temp file plus rename, not a direct WriteFile,
// so a crash or power loss mid-write can never leave a truncated or
// half-written cursor file on disk — a rename onto an existing path is
// atomic on the platforms panemux targets, unlike a direct write.
func SaveCursorFile(path string, entries []CursorEntry) error {
	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("encoding relay cursor file: %w", err)
	}
	return atomicWriteFile(path, data, cursorFileMode, "relay cursor file")
}

// DefaultCursorFilePath returns ~/.config/panemux/board-relay-cursor.json.
func DefaultCursorFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".config", "panemux", cursorFileName), nil
}
