package fileops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var (
	ErrPrivate      = errors.New("private record unavailable or unsafe")
	ErrPrivateLimit = errors.New("private record exceeds read limit")
	privateName     = regexp.MustCompile(`^[a-z0-9.-]{1,128}$`)
)

// PrivateDir keeps all operations relative to one no-follow, fstat-checked
// directory descriptor. Unlike AtomicWrite, it rejects linked/shared targets
// and syncs the directory before acknowledging a commit. The directory must
// already exist, belong to the effective user, and have exactly mode 0700.
// It is not a configuration-file writer: operator symlinks are not supported.
type PrivateDir struct {
	write     func(io.Writer, []byte) (int, error)
	sync      func(*os.File) error
	closeFile func(*os.File) error
	rename    func(int, string, int, string) error
	syncDir   func(int) error
	fd        int
}

func OpenPrivateDir(root string) (*PrivateDir, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || os.Getuid() != os.Geteuid() {
		return nil, ErrPrivate
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrPrivate
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || !privateStat(st, unix.S_IFDIR, 0700, os.Geteuid()) {
		_ = unix.Close(fd)
		return nil, ErrPrivate
	}
	return &PrivateDir{
		fd: fd, write: func(w io.Writer, b []byte) (int, error) { return w.Write(b) },
		sync: (*os.File).Sync, closeFile: (*os.File).Close, rename: unix.Renameat, syncDir: unix.Fsync,
	}, nil
}

func (d *PrivateDir) Close() error {
	if err := unix.Close(d.fd); err != nil {
		return ErrPrivate
	}
	return nil
}

func privateStat(st unix.Stat_t, kind, mode uint32, uid int) bool {
	return uint32(st.Mode)&unix.S_IFMT == kind && uint32(st.Mode)&07777 == mode && int(st.Uid) == uid &&
		(kind != unix.S_IFREG || st.Nlink == 1)
}

func safePrivateName(name string) bool {
	return name != "." && name != ".." && privateName.MatchString(name)
}

func (d *PrivateDir) openRegular(name string, flags int) (*os.File, error) {
	if !safePrivateName(name) {
		return nil, ErrPrivate
	}
	fd, err := unix.Openat(d.fd, name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, os.ErrNotExist
		}
		return nil, ErrPrivate
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || !privateStat(st, unix.S_IFREG, 0600, os.Geteuid()) {
		_ = unix.Close(fd)
		return nil, ErrPrivate
	}
	return os.NewFile(uintptr(fd), "private-record"), nil
}

func (d *PrivateDir) Read(name string, limit int) ([]byte, error) {
	if limit < 1 || limit > 1<<20 {
		return nil, ErrPrivateLimit
	}
	f, err := d.openRegular(name, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, ErrPrivate
	}
	if info.Size() > int64(limit) {
		return nil, ErrPrivateLimit
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, ErrPrivate
	}
	if len(data) > limit {
		return nil, ErrPrivateLimit
	}
	return data, nil
}

func (d *PrivateDir) checkTarget(name string) error {
	f, err := d.openRegular(name, unix.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_ = f.Close()
	return nil
}

// Replace requires the caller's Lock when used for read/compare/write. A
// failure after rename but before directory sync has an unknown durability
// result: callers must read again, never acknowledge it as a successful write.
func (d *PrivateDir) Replace(name string, data []byte) error {
	if !safePrivateName(name) {
		return ErrPrivate
	}
	if err := d.checkTarget(name); err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ErrPrivate
	}
	temp := ".tmp-" + hex.EncodeToString(random[:])
	f, err := d.openRegular(temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return ErrPrivate
	}
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
		_ = unix.Unlinkat(d.fd, temp, 0)
	}()
	n, err := d.write(f, data)
	if err != nil || n != len(data) {
		return ErrPrivate
	}
	if d.sync(f) != nil {
		return ErrPrivate
	}
	err = d.closeFile(f)
	closed = true
	if err != nil {
		return ErrPrivate
	}
	if err := d.checkTarget(name); err != nil {
		return err
	}
	if d.rename(d.fd, temp, d.fd, name) != nil {
		return ErrPrivate
	}
	if d.syncDir(d.fd) != nil {
		return ErrPrivate
	}
	return nil
}

// Lock is process-safe and releases automatically if a helper exits or is
// killed. Its inode is never removed or replaced; stale lock files are harmless.
// Every call opens its own descriptor, including calls in the same process.
func (d *PrivateDir) Lock(ctx context.Context, budget time.Duration) (func(), error) {
	if budget <= 0 || budget > time.Second {
		return nil, ErrPrivate
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("private lock canceled: %w", err)
	}
	var f *os.File
	// Concurrent creation can report ENOENT on macOS even with O_CREAT.
	// Retry only that transient condition, under the same finite deadline.
	for {
		var err error
		f, err = d.openRegular(".lock", unix.O_CREAT|unix.O_RDWR)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, ErrPrivate
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("private lock canceled: %w", ctxErr)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("private lock canceled: %w", ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("private lock canceled: %w", err)
		}
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				_ = f.Close()
				return nil, fmt.Errorf("private lock canceled: %w", ctxErr)
			}
			var once sync.Once
			return func() { once.Do(func() { _ = f.Close() }) }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			_ = f.Close()
			return nil, ErrPrivate
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
}
