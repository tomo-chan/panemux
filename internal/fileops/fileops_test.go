package fileops_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/fileops"
)

// The temp-file-plus-rename dance exists to survive a disk filling or a
// filesystem going read-only *partway through* a write. Every arm after
// os.CreateTemp has already succeeded is exactly that situation — and none of
// them could be reached by a test before this seam existed, because the file
// being written is one the function created itself moments earlier: it always
// exists, is always writable, and is always owned by the process. The suite
// also runs as root in CI, so permission bits are not available either. See
// issue #222.

var errInjected = errors.New("injected failure")

// ── The happy path ───────────────────────────────────────────────────────────

func TestAtomicWriteCreatesParentDirAndWritesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "file.json")

	err := fileops.AtomicWrite(path, []byte(`{"a":1}`), 0600, "test file")
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, string(data))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestAtomicWriteOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.json")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0600))

	require.NoError(t, fileops.AtomicWrite(path, []byte("new"), 0600, "test file"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
}

func TestAtomicWriteLeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.json")

	require.NoError(t, fileops.AtomicWrite(path, []byte("x"), 0600, "test file"))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "file.json", entries[0].Name())
}

// ── The arms that only the seam can reach ────────────────────────────────────

// atomicWriteArm is one step of the write and what failing it must produce.
type atomicWriteArm struct {
	spy     func() *fileops.Spy
	name    string
	wantMsg string
	// A step that fails before the temp file exists cannot leave one behind;
	// one that fails after it does must remove it.
	wantTempFile bool
	wantCloses   int
}

func atomicWriteArms() []atomicWriteArm {
	return []atomicWriteArm{
		{
			name:    "create temp",
			spy:     func() *fileops.Spy { return &fileops.Spy{CreateTempErr: errInjected} },
			wantMsg: "creating temp test file",
		},
		{
			name:         "write",
			spy:          func() *fileops.Spy { return &fileops.Spy{WriteErr: errInjected} },
			wantMsg:      "writing temp test file",
			wantTempFile: true,
			wantCloses:   1,
		},
		{
			name:         "sync",
			spy:          func() *fileops.Spy { return &fileops.Spy{SyncErr: errInjected} },
			wantMsg:      "syncing temp test file",
			wantTempFile: true,
			wantCloses:   1,
		},
		{
			name:         "close",
			spy:          func() *fileops.Spy { return &fileops.Spy{CloseErr: errInjected} },
			wantMsg:      "closing temp test file",
			wantTempFile: true,
			wantCloses:   1,
		},
		{
			name:         "chmod",
			spy:          func() *fileops.Spy { return &fileops.Spy{ChmodErr: errInjected} },
			wantMsg:      "setting test file mode",
			wantTempFile: true,
			wantCloses:   1,
		},
		{
			name:         "rename",
			spy:          func() *fileops.Spy { return &fileops.Spy{RenameErr: errInjected} },
			wantMsg:      "replacing test file",
			wantTempFile: true,
			wantCloses:   1,
		},
	}
}

// Every failure arm has to say which step failed, and every one of them has to
// leave the directory as it found it. Table-driven because the interesting
// property is that all six behave the same way, and a per-arm test would let
// one of them quietly stop doing so.
func TestAtomicWriteReportsEachFailedStepAndNeverLeavesATempFile(t *testing.T) {
	for _, tt := range atomicWriteArms() {
		t.Run(tt.name, func(t *testing.T) {
			spy := tt.spy()
			fileops.SetOpsForTest(t, spy.Ops())
			dir := t.TempDir()
			path := filepath.Join(dir, "file.json")

			err := fileops.AtomicWrite(path, []byte("payload"), 0600, "test file")

			require.Error(t, err)
			assert.ErrorIs(t, err, errInjected,
				"the underlying failure must stay inspectable through the wrap")
			assert.Contains(t, err.Error(), tt.wantMsg,
				"the message must name the step that failed, so a log line says which one it was")
			assert.NoFileExists(t, path, "a failed write must never leave a partial target file")

			assertTempFileCleanedUp(t, spy, tt.wantTempFile)

			entries, readErr := os.ReadDir(dir)
			require.NoError(t, readErr)
			assert.Empty(t, entries, "nothing at all may survive a failed write")

			// The defer-based cleanup and the explicit close on the write arm
			// have to add up to exactly one close: zero leaks the descriptor,
			// two closes a descriptor some other goroutine may already have
			// been handed.
			assert.Equal(t, tt.wantCloses, spy.Closes())
		})
	}
}

func assertTempFileCleanedUp(t *testing.T, spy *fileops.Spy, wantTempFile bool) {
	t.Helper()

	files := spy.Files()
	if !wantTempFile {
		assert.Empty(t, files, "no temp file can exist before CreateTemp succeeds")
		return
	}
	require.Len(t, files, 1)
	assert.NoFileExists(t, files[0],
		"the temp file must be cleaned up on the way out of every arm after it exists")
}

// MkdirAll runs before anything else and is reachable without the seam (a
// regular file where the directory has to go fails with ENOTDIR for every
// uid, root included), so it stays a real-filesystem fixture.
func TestAtomicWriteReportsAnUncreatableParentDirectory(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "notadir")
	require.NoError(t, os.WriteFile(notADir, []byte("a regular file\n"), 0600))

	err := fileops.AtomicWrite(filepath.Join(notADir, "nested", "file.json"), []byte("x"), 0600, "test file")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating test file directory")
}

// ── The seam itself ──────────────────────────────────────────────────────────

// Unsubstituted, the package is the os functions and nothing else. A seam that
// quietly answered something of its own would make every caller's default
// wrong.
func TestOpsDefaultToTheRealOperations(t *testing.T) {
	dir := t.TempDir()

	f, err := fileops.CreateTemp(dir, "real-*")
	require.NoError(t, err)
	_, err = f.Write([]byte("real"))
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, fileops.Chmod(f.Name(), 0600))

	info, err := os.Stat(f.Name())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	opened, err := fileops.OpenFile(f.Name(), os.O_RDWR|os.O_APPEND, 0600)
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.Close() })
	stat, err := opened.Stat()
	require.NoError(t, err)
	assert.Equal(t, int64(4), stat.Size())

	last := make([]byte, 1)
	_, err = opened.ReadAt(last, 3)
	require.NoError(t, err)
	assert.Equal(t, byte('l'), last[0])
}

// The real openers must report a true nil File on failure, not a nil *os.File
// boxed into a non-nil interface. A caller written in the shape this package
// invites — `f, err := CreateTemp(...); if f != nil { os.Remove(f.Name()) }` —
// takes that branch on a typed nil and panics in Name().
//
// The Spy returns an untyped nil on its own failure path, so without this the
// double and the thing it doubles would disagree about exactly the value a
// caller might test — which is the one divergence a double must not have.
func TestRealOpsReportATrueNilFileWhenTheyFail(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "notadir")
	require.NoError(t, os.WriteFile(notADir, []byte("a regular file\n"), 0600))

	created, createErr := fileops.CreateTemp(notADir, "x-*")
	require.Error(t, createErr)
	// `== nil` rather than assert.Nil, deliberately: testify unwraps the
	// interface and reports a typed-nil pointer as nil, so assert.Nil would
	// hold for exactly the value this test exists to reject.
	assert.True(t, created == nil, "got a non-nil interface holding %#v", created)

	opened, openErr := fileops.OpenFile(filepath.Join(notADir, "f"), os.O_CREATE|os.O_RDWR, 0600)
	require.Error(t, openErr)
	assert.True(t, opened == nil, "got a non-nil interface holding %#v", opened)
}

// A substitution that outlived its test would poison every test after it in
// the same binary, in an order-dependent way that is miserable to track down.
func TestSetOpsForTestRestoresThePreviousOperations(t *testing.T) {
	t.Run("substituted", func(t *testing.T) {
		fileops.SetOpsForTest(t, (&fileops.Spy{CreateTempErr: errInjected}).Ops())

		_, err := fileops.CreateTemp(t.TempDir(), "x-*")
		assert.ErrorIs(t, err, errInjected)
	})

	f, err := fileops.CreateTemp(t.TempDir(), "x-*")
	require.NoError(t, err, "the substitution must not have outlived the subtest")
	require.NoError(t, f.Close())
}

// Only the operations a test names are substituted; every other one is filled
// in with the real thing. A test injecting one failure would otherwise be
// running silently against stubs for all the rest.
//
// Each case fails at its own substituted step *because* the steps before it
// were the real ones — AtomicWrite never reaches Chmod unless a real
// CreateTemp, Write and Close all succeeded first.
func TestSetOpsForTestFillsUnsetOperationsWithTheRealOnes(t *testing.T) {
	atomicWrite := func(dir string) error {
		return fileops.AtomicWrite(filepath.Join(dir, "f"), []byte("x"), 0600, "test file")
	}

	for _, tt := range []struct {
		exercise func(dir string) error
		ops      fileops.Ops
		name     string
	}{
		{
			name: "CreateTemp",
			ops: fileops.Ops{
				CreateTemp: func(string, string) (fileops.File, error) { return nil, errInjected },
			},
			exercise: func(dir string) error {
				_, err := fileops.CreateTemp(dir, "x-*")
				return err
			},
		},
		{
			name: "OpenFile",
			ops: fileops.Ops{
				OpenFile: func(string, int, os.FileMode) (fileops.File, error) { return nil, errInjected },
			},
			exercise: func(dir string) error {
				_, err := fileops.OpenFile(filepath.Join(dir, "f"), os.O_CREATE|os.O_RDWR, 0600)
				return err
			},
		},
		{
			name:     "Chmod",
			ops:      fileops.Ops{Chmod: func(string, os.FileMode) error { return errInjected }},
			exercise: atomicWrite,
		},
		{
			name:     "Rename",
			ops:      fileops.Ops{Rename: func(string, string) error { return errInjected }},
			exercise: atomicWrite,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fileops.SetOpsForTest(t, tt.ops)

			assert.ErrorIs(t, tt.exercise(t.TempDir()), errInjected)
		})
	}
}
