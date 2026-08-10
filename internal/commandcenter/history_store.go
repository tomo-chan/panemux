package commandcenter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const historyFileName = "command-center-history.jsonl"
const historyFileMode os.FileMode = 0600

// HistoryEntry is one line of the command center's own captured
// conversation history: a raw --output-format=stream-json line, exactly as
// captured while relaying it to the WS client — never re-derived from
// Claude Code's transcript file after the fact. See docs/agent-board.md's
// "API and streaming" section.
type HistoryEntry struct {
	At  time.Time       `json:"at"`
	Raw json.RawMessage `json:"raw"`
}

// AppendHistory appends entries to path, one JSON object per line, creating
// the parent directory and file if needed. A crash mid-write can leave at
// most one truncated trailing line, which LoadHistory tolerates by design —
// see its own doc comment. Empty entries is a no-op and never creates the
// file, so a command center that has never run leaves no history file
// behind.
func AppendHistory(path string, entries []HistoryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("creating command center history directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, historyFileMode)
	if err != nil {
		return fmt.Errorf("opening command center history file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	var buf bytes.Buffer
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("encoding command center history entry: %w", err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("writing command center history file: %w", err)
	}
	return nil
}

// LoadHistory reads every persisted HistoryEntry. A missing file is the
// normal state before the command center's first query and returns an empty
// slice, not an error.
//
// A malformed final line — the tail of a write interrupted mid-append by a
// crash or power loss — is silently dropped rather than treated as an
// error, since AppendHistory's own write pattern can produce exactly that
// shape and it represents no genuinely lost history (the interrupted append
// call itself never returned success). A malformed line anywhere else in
// the file is a real corruption, not an expected truncation, and is
// reported as an error instead of silently skipped.
func LoadHistory(path string) ([]HistoryEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening command center history file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading command center history file: %w", err)
	}

	entries := make([]HistoryEntry, 0, len(lines))
	for i, line := range lines {
		var entry HistoryEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			if i == len(lines)-1 {
				break // tolerate a truncated trailing line, see doc comment
			}
			return nil, fmt.Errorf("parsing command center history file: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// DefaultHistoryFilePath returns ~/.config/panemux/command-center-history.jsonl.
func DefaultHistoryFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".config", "panemux", historyFileName), nil
}
