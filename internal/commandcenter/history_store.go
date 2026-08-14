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
// the parent directory and file if needed. A crash mid-write can leave a
// truncated final line with no trailing newline; if the file is already in
// that state, a leading newline is written first so this call's own entries
// land on their own lines rather than concatenating onto the truncated
// tail — see TestAppendHistoryAddsSeparatingNewlineAfterUnterminatedPriorWrite
// for the corruption this avoids, and LoadHistory's own doc comment for why
// a bad line further in the file is tolerated either way. Empty entries is
// a no-op and never creates the file, so a command center that has never
// run leaves no history file behind.
func AppendHistory(path string, entries []HistoryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("creating command center history directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, historyFileMode)
	if err != nil {
		return fmt.Errorf("opening command center history file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	needsLeadingNewline, err := fileEndsWithoutTrailingNewline(f)
	if err != nil {
		return fmt.Errorf("checking command center history file: %w", err)
	}

	var buf bytes.Buffer
	if needsLeadingNewline {
		buf.WriteByte('\n')
	}
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

// fileEndsWithoutTrailingNewline reports whether f is non-empty and its
// last byte is not '\n'. Uses ReadAt at an explicit offset rather than
// Read, so it doesn't disturb the file's O_APPEND write position (every
// Write on an O_APPEND-opened file always targets end-of-file regardless of
// the current offset, but ReadAt avoids relying on that rather than
// assuming it).
func fileEndsWithoutTrailingNewline(f *os.File) (bool, error) {
	info, err := f.Stat()
	if err != nil {
		return false, fmt.Errorf("stat: %w", err)
	}
	if info.Size() == 0 {
		return false, nil
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil {
		return false, fmt.Errorf("reading last byte: %w", err)
	}
	return last[0] != '\n', nil
}

// LoadHistory reads every persisted HistoryEntry. A missing file is the
// normal state before the command center's first query and returns an empty
// slice, not an error.
//
// A line that fails to parse is skipped, wherever it falls in the file, not
// treated as a fatal error for the whole read. This is deliberately more
// lenient than a typical parser: history is best-effort conversation
// record, not authoritative state, and one bad line — the tail of a write
// interrupted mid-append by a crash or power loss, which AppendHistory's
// own newline-separation guard (see its doc comment) keeps isolated to
// exactly one line rather than merged into a neighbor — permanently 500-ing
// every future read, including every good line around it, is a worse
// failure mode than silently omitting that one line. An earlier revision of
// this function only tolerated a malformed *trailing* line and hard-failed
// on any other position; that was wrong in practice, since a truncated line
// stops being trailing the moment the process restarts and appends again,
// which is the normal recovery path, not an exceptional one.
func LoadHistory(path string) ([]HistoryEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening command center history file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	var entries []HistoryEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry HistoryEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading command center history file: %w", err)
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
