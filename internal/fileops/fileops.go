// Package fileops is panemux's seam onto the file operations its persisted
// state is written through: creating a temp file, writing it, closing it,
// setting its mode, and renaming it into place.
//
// It exists because those steps have failure arms no test could reach.
// DEVELOPMENT.md's testability rule already covers the ordinary case — break
// the shape of the path and the operation fails for every uid — but that only
// works while the path is one the caller was handed. Once os.CreateTemp has
// succeeded, the file being written is one the function created itself
// moments earlier: it exists, it is writable, and the process owns it. The
// suite runs as root in CI, so permission bits are not available either. The
// arms behind that wall are precisely the ones a disk filling or a filesystem
// going read-only partway through a write fires, which is the failure the
// temp-file-plus-rename discipline exists to survive. See issue #222.
//
// Why a separate package rather than a variable in each caller — the same
// question internal/homedir answers, with the same answer: the callers do not
// line up with the tests. The root package's own tests drive
// internal/board's cursor and bootstrap writes through persistBoardCursors /
// persistBootstrapState, and a package-private variable is invisible from
// another package's test.
package fileops

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// File is the subset of *os.File the writers here and their callers use. It
// is one interface rather than a narrow one per call site so that a single
// test double satisfies every one of them; *os.File satisfies it as is.
type File interface {
	io.WriteCloser
	Name() string
	Stat() (os.FileInfo, error)
	ReadAt(p []byte, off int64) (n int, err error)
}

// Ops is the set of operations this package performs. A nil field means the
// real os function, so a test substitutes only the step it is interested in.
type Ops struct {
	CreateTemp func(dir, pattern string) (File, error)
	OpenFile   func(name string, flag int, perm os.FileMode) (File, error)
	Chmod      func(name string, mode os.FileMode) error
	Rename     func(oldpath, newpath string) error
}

// ops is the seam itself. SetOpsForTest is the only supported way to replace
// it, and it restores the previous value when the test ends.
var ops = realOps()

func realOps() Ops {
	return Ops{
		CreateTemp: func(dir, pattern string) (File, error) { return os.CreateTemp(dir, pattern) },
		OpenFile: func(name string, flag int, perm os.FileMode) (File, error) {
			return os.OpenFile(name, flag, perm) //nolint:gosec // the caller owns the path; see each call site
		},
		Chmod:  os.Chmod,
		Rename: os.Rename,
	}
}

// withDefaults fills every unset field with the real operation.
func (o Ops) withDefaults() Ops {
	defaults := realOps()
	if o.CreateTemp == nil {
		o.CreateTemp = defaults.CreateTemp
	}
	if o.OpenFile == nil {
		o.OpenFile = defaults.OpenFile
	}
	if o.Chmod == nil {
		o.Chmod = defaults.Chmod
	}
	if o.Rename == nil {
		o.Rename = defaults.Rename
	}
	return o
}

// CreateTemp creates a temp file through the seam, exactly as os.CreateTemp
// does.
func CreateTemp(dir, pattern string) (File, error) {
	return ops.CreateTemp(dir, pattern)
}

// OpenFile opens a file through the seam, exactly as os.OpenFile does.
func OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	return ops.OpenFile(name, flag, perm)
}

// Chmod sets a file's mode through the seam, exactly as os.Chmod does.
func Chmod(name string, mode os.FileMode) error {
	return ops.Chmod(name, mode)
}

// AtomicWrite writes data to path via a temp file plus rename, creating the
// parent directory if needed, so a crash or power loss mid-write can never
// leave a truncated or half-written file on disk — a rename onto an existing
// path is atomic on the platforms panemux targets, unlike a direct write.
// label identifies the file kind in error messages (e.g. "relay cursor file",
// "bootstrap state file"), so a log line names which file failed.
//
// Every arm after the temp file exists removes it on the way out, including
// the ones that cannot be reached without substituting Ops.
func AtomicWrite(path string, data []byte, mode os.FileMode, label string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("creating %s directory: %w", label, err)
	}
	tmp, err := ops.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp %s: %w", label, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp %s: %w", label, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp %s: %w", label, err)
	}
	if err := ops.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("setting %s mode: %w", label, err)
	}
	if err := ops.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replacing %s: %w", label, err)
	}
	return nil
}

// TestingT is the subset of *testing.T the helper below needs. It is declared
// here rather than importing "testing" so that a package linking fileops into
// the panemux binary does not also link the testing package into it — the same
// shape internal/homedir uses.
type TestingT interface {
	Helper()
	Cleanup(func())
}

// SetOpsForTest points this package's operations at o for the duration of the
// test, filling every unset field with the real one and restoring the previous
// set afterwards.
//
// What it does NOT buy, stated plainly because the shape invites the
// assumption: this is not safe under t.Parallel. ops is one unsynchronized
// package variable for the whole test binary, so two parallel tests
// substituting it race, and the loser silently runs against the other's
// doubles. Substitute it only from tests that do not call t.Parallel; no test
// in this repository does.
func SetOpsForTest(t TestingT, o Ops) {
	t.Helper()
	original := ops
	t.Cleanup(func() { ops = original })
	ops = o.withDefaults()
}
