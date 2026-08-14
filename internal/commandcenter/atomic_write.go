// Package commandcenter implements the Agent Board command center: a
// headless, per-query `claude -p --resume` subprocess that reads and writes
// the board only through panemux's own authenticated REST API (see
// docs/agent-board.md's Command center section).
package commandcenter

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to path via a temp file plus rename, creating
// the parent directory if needed, so a crash or power loss mid-write can
// never leave a truncated or half-written file on disk — a rename onto an
// existing path is atomic on the platforms panemux targets, unlike a direct
// write. This mirrors internal/board's own atomicWriteFile helper; it is
// duplicated here rather than shared because internal/board's copy is
// unexported and this package's persisted files (command-center session id,
// history) are otherwise unrelated to anything internal/board owns.
func atomicWriteFile(path string, data []byte, mode os.FileMode, label string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("creating %s directory: %w", label, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp %s: %w", label, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp %s: %w", label, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp %s: %w", label, err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("setting %s mode: %w", label, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replacing %s: %w", label, err)
	}
	return nil
}
