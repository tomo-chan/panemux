package board

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// userHomeDirFn is overridable in tests so DefaultCursorPath does not touch
// the real filesystem/home directory (DEVELOPMENT.md's testability rule).
var userHomeDirFn = os.UserHomeDir

// CursorKey identifies one (host, team) polling cursor.
type CursorKey struct {
	Host string
	Team string
}

// CursorStore persists the relay's per-(host,team) polling cursor across
// restarts. See docs/agent-board.md's Cross-host relay section.
type CursorStore interface {
	Load() (map[CursorKey]string, error)
	Save(map[CursorKey]string) error
}

// DefaultCursorPath returns ~/.config/panemux/board-relay-cursor.json.
func DefaultCursorPath() (string, error) {
	home, err := userHomeDirFn()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".config", "panemux", "board-relay-cursor.json"), nil
}

// cursorFileEntry is the on-disk JSON shape: a flat list rather than a map,
// since CursorKey (a struct) cannot be a JSON object key.
type cursorFileEntry struct {
	Host   string `json:"host"`
	Team   string `json:"team"`
	Cursor string `json:"cursor"`
}

// FileCursorStore persists cursors to a small local JSON file — not a
// database table, since panemux owns no database (see docs/agent-board.md's
// "Local vs remote resource placement").
type FileCursorStore struct {
	path string
}

// NewFileCursorStore creates a store backed by the file at path.
func NewFileCursorStore(path string) *FileCursorStore {
	return &FileCursorStore{path: path}
}

// Load reads persisted cursors, returning an empty, non-error map when the
// file does not exist yet (fresh install / never persisted before).
func (s *FileCursorStore) Load() (map[CursorKey]string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[CursorKey]string{}, nil
		}
		return nil, fmt.Errorf("reading relay cursor file: %w", err)
	}
	var entries []cursorFileEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing relay cursor file: %w", err)
	}
	out := make(map[CursorKey]string, len(entries))
	for _, e := range entries {
		out[CursorKey{Host: e.Host, Team: e.Team}] = e.Cursor
	}
	return out, nil
}

// Save writes cursors, creating the parent directory if needed.
func (s *FileCursorStore) Save(cursors map[CursorKey]string) error {
	entries := make([]cursorFileEntry, 0, len(cursors))
	for k, v := range cursors {
		entries = append(entries, cursorFileEntry{Host: k.Host, Team: k.Team, Cursor: v})
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling relay cursor file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0750); err != nil {
		return fmt.Errorf("creating relay cursor directory: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0600); err != nil {
		return fmt.Errorf("writing relay cursor file: %w", err)
	}
	return nil
}

// MemCursorStore is an in-memory CursorStore, useful for tests and for
// running without persistence.
type MemCursorStore struct {
	cursors map[CursorKey]string
}

func NewMemCursorStore() *MemCursorStore {
	return &MemCursorStore{cursors: make(map[CursorKey]string)}
}

func (s *MemCursorStore) Load() (map[CursorKey]string, error) {
	out := make(map[CursorKey]string, len(s.cursors))
	for k, v := range s.cursors {
		out[k] = v
	}
	return out, nil
}

func (s *MemCursorStore) Save(cursors map[CursorKey]string) error {
	s.cursors = make(map[CursorKey]string, len(cursors))
	for k, v := range cursors {
		s.cursors[k] = v
	}
	return nil
}
