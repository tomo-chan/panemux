package fileops

import (
	"os"
	"sync"
)

// Spy is the test double this seam exists for: an Ops set that performs every
// real operation but fails whichever steps a test names, while recording the
// files it handed out so the test can assert they were cleaned up.
//
// It lives in the production package rather than a _test.go file because the
// tests that need it are in other packages — internal/board's callers are
// driven from the root package's tests — and it imports nothing beyond os and
// sync, so nothing linking fileops pays for it.
//
// Each real file is genuinely created on disk before the injected failure
// fires. That is the point: a double that never touched the filesystem could
// not tell the difference between "the temp file was removed" and "no temp
// file was ever there".
type Spy struct {
	// Each of these fails the named step when non-nil. The real operation
	// runs when it is nil.
	CreateTempErr error
	OpenFileErr   error
	ChmodErr      error
	RenameErr     error
	WriteErr      error
	SyncErr       error
	CloseErr      error
	StatErr       error
	ReadAtErr     error

	files []string

	mu     sync.Mutex
	closes int
}

// Ops returns the operation set to hand to SetOpsForTest.
func (s *Spy) Ops() Ops {
	return Ops{
		CreateTemp: func(dir, pattern string) (File, error) {
			if s.CreateTempErr != nil {
				return nil, s.CreateTempErr
			}
			return s.wrap(os.CreateTemp(dir, pattern))
		},
		OpenFile: func(name string, flag int, perm os.FileMode) (File, error) {
			if s.OpenFileErr != nil {
				return nil, s.OpenFileErr
			}
			return s.wrap(os.OpenFile(name, flag, perm))
		},
		Chmod: func(name string, mode os.FileMode) error {
			if s.ChmodErr != nil {
				return s.ChmodErr
			}
			return os.Chmod(name, mode)
		},
		Rename: func(oldpath, newpath string) error {
			if s.RenameErr != nil {
				return s.RenameErr
			}
			return os.Rename(oldpath, newpath)
		},
	}
}

// Files returns the path of every file this Spy created or opened, in order.
func (s *Spy) Files() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.files...)
}

// Closes returns how many times Close was called across every file this Spy
// handed out. Zero leaks a descriptor; two closes one the runtime may already
// have handed to something else.
func (s *Spy) Closes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

func (s *Spy) wrap(f *os.File, err error) (File, error) {
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.files = append(s.files, f.Name())
	s.mu.Unlock()
	return &spyFile{File: f, spy: s}, nil
}

// spyFile is one file handed out by a Spy. Where a real failure would still
// have changed the file, the real *os.File is operated on first and only the
// reported error is substituted — see Write, which is a short write rather
// than a write that never happened.
type spyFile struct {
	*os.File
	spy *Spy
}

func (f *spyFile) Write(p []byte) (int, error) {
	if f.spy.WriteErr != nil {
		// A real ENOSPC is a short write, not a write that never happened:
		// n > 0 bytes land on disk and then the error is reported. Reproduce
		// that, or a test asserting the temp file was cleaned up would be
		// asserting about an empty file — the easy half of the case this
		// whole seam exists for.
		n, _ := f.File.Write(p[:len(p)/2])
		return n, f.spy.WriteErr
	}
	return f.File.Write(p)
}

func (f *spyFile) Sync() error {
	if f.spy.SyncErr != nil {
		return f.spy.SyncErr
	}
	return f.File.Sync()
}

func (f *spyFile) Close() error {
	f.spy.mu.Lock()
	f.spy.closes++
	f.spy.mu.Unlock()
	err := f.File.Close()
	if f.spy.CloseErr != nil {
		return f.spy.CloseErr
	}
	return err
}

func (f *spyFile) Stat() (os.FileInfo, error) {
	if f.spy.StatErr != nil {
		return nil, f.spy.StatErr
	}
	return f.File.Stat()
}

func (f *spyFile) ReadAt(p []byte, off int64) (int, error) {
	if f.spy.ReadAtErr != nil {
		return 0, f.spy.ReadAtErr
	}
	return f.File.ReadAt(p, off)
}
