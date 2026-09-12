package fileops_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/fileops"
)

// The Spy is the thing every other test in the repository trusts to tell the
// truth about a failure, so its own contract is worth pinning: it fails
// exactly the steps it was given, performs every other one for real, and
// records what it handed out. A double that quietly stubbed a step it was not
// asked about would make every test using it assert against a fiction.

// With nothing injected a Spy is the real operations, end to end — which is
// also what makes "fails only the named step" a meaningful claim below.
func TestSpyWithNothingInjectedPerformsEveryRealOperation(t *testing.T) {
	spy := &fileops.Spy{}
	fileops.SetOpsForTest(t, spy.Ops())
	dir := t.TempDir()
	path := filepath.Join(dir, "file.json")

	require.NoError(t, fileops.AtomicWrite(path, []byte("payload"), 0600, "test file"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "payload", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	require.Len(t, spy.Files(), 1, "the temp file it created must be recorded")
	assert.Equal(t, 1, spy.Closes())
}

// spyKnob is one of the Spy's injectable errors and the smallest call that
// reaches the step it fails.
type spyKnob struct {
	exercise func(t *testing.T, dir string) error
	spy      func() *fileops.Spy
	name     string
}

func spyKnobs() []spyKnob {
	return append(spyOperationKnobs(), spyFileKnobs()...)
}

// The four operations the seam itself performs.
func spyOperationKnobs() []spyKnob {
	return []spyKnob{
		{
			name: "CreateTempErr",
			spy:  func() *fileops.Spy { return &fileops.Spy{CreateTempErr: errInjected} },
			exercise: func(t *testing.T, dir string) error {
				t.Helper()
				_, err := fileops.CreateTemp(dir, "x-*")
				return err
			},
		},
		{
			name: "OpenFileErr",
			spy:  func() *fileops.Spy { return &fileops.Spy{OpenFileErr: errInjected} },
			exercise: func(t *testing.T, dir string) error {
				t.Helper()
				_, err := fileops.OpenFile(filepath.Join(dir, "f"), os.O_CREATE|os.O_RDWR, 0600)
				return err
			},
		},
		{
			name: "ChmodErr",
			spy:  func() *fileops.Spy { return &fileops.Spy{ChmodErr: errInjected} },
			exercise: func(t *testing.T, dir string) error {
				t.Helper()
				return fileops.Chmod(dir, 0700)
			},
		},
		{
			name: "RenameErr",
			spy:  func() *fileops.Spy { return &fileops.Spy{RenameErr: errInjected} },
			exercise: func(t *testing.T, dir string) error {
				t.Helper()
				return fileops.AtomicWrite(filepath.Join(dir, "f"), []byte("x"), 0600, "test file")
			},
		},
	}
}

// The four methods on the file those operations hand back — the half that
// needs the temp file itself behind an interface, not just the os functions.
func spyFileKnobs() []spyKnob {
	return []spyKnob{
		{
			name: "WriteErr",
			spy:  func() *fileops.Spy { return &fileops.Spy{WriteErr: errInjected} },
			exercise: func(t *testing.T, dir string) error {
				return onATempFile(t, dir, func(f fileops.File) error {
					if _, err := f.Write([]byte("x")); err != nil {
						return fmt.Errorf("writing: %w", err)
					}
					return nil
				})
			},
		},
		{
			name: "SyncErr",
			spy:  func() *fileops.Spy { return &fileops.Spy{SyncErr: errInjected} },
			exercise: func(t *testing.T, dir string) error {
				return onATempFile(t, dir, func(f fileops.File) error { return f.Sync() })
			},
		},
		{
			name: "CloseErr",
			spy:  func() *fileops.Spy { return &fileops.Spy{CloseErr: errInjected} },
			exercise: func(t *testing.T, dir string) error {
				t.Helper()
				f, err := fileops.CreateTemp(dir, "x-*")
				require.NoError(t, err)
				return f.Close()
			},
		},
		{
			name: "StatErr",
			spy:  func() *fileops.Spy { return &fileops.Spy{StatErr: errInjected} },
			exercise: func(t *testing.T, dir string) error {
				return onATempFile(t, dir, func(f fileops.File) error {
					_, err := f.Stat()
					return err
				})
			},
		},
		{
			name: "ReadAtErr",
			spy:  func() *fileops.Spy { return &fileops.Spy{ReadAtErr: errInjected} },
			exercise: func(t *testing.T, dir string) error {
				return onATempFile(t, dir, func(f fileops.File) error {
					_, err := f.ReadAt(make([]byte, 1), 0)
					return err
				})
			},
		},
	}
}

// onATempFile runs do against a temp file from the seam, closing it
// afterwards. Close itself is the one knob that cannot go through this, since
// its own error is what the case is about.
func onATempFile(t *testing.T, dir string, do func(f fileops.File) error) error {
	t.Helper()
	f, err := fileops.CreateTemp(dir, "x-*")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	return do(f)
}

// Each knob fails its own step and nothing else. The assertion that the other
// steps still ran for real is the ErrorIs: a stubbed-out earlier step would
// fail with something else first.
func TestSpyFailsOnlyTheStepItWasGiven(t *testing.T) {
	for _, tt := range spyKnobs() {
		t.Run(tt.name, func(t *testing.T) {
			fileops.SetOpsForTest(t, tt.spy().Ops())

			assert.ErrorIs(t, tt.exercise(t, t.TempDir()), errInjected)
		})
	}
}

// A real failure with nothing injected has to travel back unchanged. Reporting
// an injected sentinel here, or swallowing the failure and handing back a file
// that does not exist, would both be worse than the failure itself.
func TestSpyPassesARealFailureThroughUnchanged(t *testing.T) {
	spy := &fileops.Spy{}
	fileops.SetOpsForTest(t, spy.Ops())
	notADir := filepath.Join(t.TempDir(), "notadir")
	require.NoError(t, os.WriteFile(notADir, []byte("a regular file\n"), 0600))

	_, err := fileops.CreateTemp(notADir, "x-*")

	require.Error(t, err)
	assert.NotErrorIs(t, err, errInjected)
	assert.Empty(t, spy.Files(), "a file that was never created must not be recorded as one")
}

// A real ENOSPC or EDQUOT is a SHORT write: some bytes reach the file and
// then the error is reported. A double that returned the error without writing
// anything would leave every "the temp file was cleaned up" assertion in this
// repository asserting about an empty file — which is the easy half of the
// case, and not the one the temp-file discipline exists for.
func TestSpyWriteErrIsAShortWriteNotAWriteThatNeverHappened(t *testing.T) {
	fileops.SetOpsForTest(t, (&fileops.Spy{WriteErr: errInjected}).Ops())
	f, err := fileops.CreateTemp(t.TempDir(), "x-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	n, err := f.Write([]byte("abcdef"))

	require.ErrorIs(t, err, errInjected)
	assert.Equal(t, 3, n, "a short write reports how much of the payload landed")
	require.NoError(t, f.Sync())
	onDisk, readErr := os.ReadFile(f.Name())
	require.NoError(t, readErr)
	assert.Equal(t, "abc", string(onDisk),
		"the partial payload must really be on disk, not just reported as written")
}

// The real Stat and ReadAt have to answer about the real file, since
// AppendHistory's decision — does this file end mid-line — is made from them.
func TestSpyReadsTheRealFileWhenNothingIsInjected(t *testing.T) {
	fileops.SetOpsForTest(t, (&fileops.Spy{}).Ops())
	path := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(path, []byte("abc"), 0600))

	f, err := fileops.OpenFile(path, os.O_RDWR|os.O_APPEND, 0600)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	require.NoError(t, f.Sync())
	info, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, int64(3), info.Size())

	last := make([]byte, 1)
	_, err = f.ReadAt(last, info.Size()-1)
	require.NoError(t, err)
	assert.Equal(t, byte('c'), last[0])
}
