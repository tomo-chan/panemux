package fileops

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestPrivateReadExactLimitsAndGrowthDuringRead(t *testing.T) {
	d, err := OpenPrivateDir(privateRoot(t))
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	require.NoError(t, d.Replace("record", bytes.Repeat([]byte("x"), 1<<20)))
	_, err = d.Read("record", 1<<20)
	require.NoError(t, err)
	for _, limit := range []int{0, -1, (1 << 20) + 1} {
		_, err = d.Read("record", limit)
		require.ErrorIs(t, err, ErrPrivateLimit)
	}
	require.NoError(t, d.Replace("record", []byte("ok")))
	d.stat = func(*os.File) (os.FileInfo, error) { return nil, errors.New("stat failed") }
	_, err = d.Read("record", 2)
	require.ErrorIs(t, err, ErrPrivate)
	d.stat = (*os.File).Stat
	d.read = func(io.Reader) ([]byte, error) { return nil, errors.New("read failed") }
	_, err = d.Read("record", 2)
	require.ErrorIs(t, err, ErrPrivate)
	d.read = func(io.Reader) ([]byte, error) { return []byte("grew"), nil }
	_, err = d.Read("record", 2)
	require.ErrorIs(t, err, ErrPrivateLimit)
}

func TestPrivateReplacementCreationAndTargetRaceFailures(t *testing.T) {
	root := privateRoot(t)
	d, err := OpenPrivateDir(root)
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	d.random = bytes.NewReader(nil)
	require.ErrorIs(t, d.Replace("record", []byte("new")), ErrPrivate)
	d.random = bytes.NewReader(make([]byte, 16))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".tmp-00000000000000000000000000000000"), nil, 0600))
	require.ErrorIs(t, d.Replace("record", []byte("new")), ErrPrivate)
	require.NoError(t, os.Remove(filepath.Join(root, ".tmp-00000000000000000000000000000000")))
	d.random = bytes.NewReader(make([]byte, 16))
	d.write = func(w io.Writer, b []byte) (int, error) {
		require.NoError(t, os.Symlink("elsewhere", filepath.Join(root, "record")))
		return w.Write(b)
	}
	require.ErrorIs(t, d.Replace("record", []byte("new")), ErrPrivate)
	target, err := os.Readlink(filepath.Join(root, "record"))
	require.NoError(t, err)
	require.Equal(t, "elsewhere", target)
}

func TestPrivateLockBudgetAndKernelFailures(t *testing.T) {
	d, err := OpenPrivateDir(privateRoot(t))
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	for _, budget := range []time.Duration{0, -1, time.Second + 1} {
		_, err = d.Lock(context.Background(), budget)
		require.ErrorIs(t, err, ErrPrivate)
	}
	unlock, err := d.Lock(context.Background(), time.Second)
	require.NoError(t, err)
	unlock()
	unlock()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = d.Lock(canceled, time.Second)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPrivateLockCancellationDuringOpenAndKernelAcquire(t *testing.T) {
	d, err := OpenPrivateDir(privateRoot(t))
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	original := d.openFile
	d.openFile = func(string, int) (*os.File, error) { return nil, ErrPrivate }
	_, err = d.Lock(context.Background(), time.Second)
	require.ErrorIs(t, err, ErrPrivate)
	d.openFile = func(string, int) (*os.File, error) { return nil, os.ErrNotExist }
	_, err = d.Lock(context.Background(), time.Millisecond)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	ctxOpen, cancelOpen := context.WithCancel(context.Background())
	d.openFile = func(string, int) (*os.File, error) { cancelOpen(); return nil, os.ErrNotExist }
	_, err = d.Lock(ctxOpen, time.Second)
	require.ErrorIs(t, err, context.Canceled)
	d.openFile = original
	d.flock = func(int, int) error { return unix.EBADF }
	_, err = d.Lock(context.Background(), time.Second)
	require.ErrorIs(t, err, ErrPrivate)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.flock = func(int, int) error { cancel(); return nil }
	_, err = d.Lock(ctx, time.Second)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPrivateClosedDescriptorCannotAcknowledgeIO(t *testing.T) {
	d, err := OpenPrivateDir(privateRoot(t))
	require.NoError(t, err)
	require.NoError(t, d.Close())
	require.ErrorIs(t, d.Close(), ErrPrivate)
	_, err = d.Read("record", 1)
	require.ErrorIs(t, err, ErrPrivate)
	_, err = d.Read("../escape", 1)
	require.ErrorIs(t, err, ErrPrivate)
	require.ErrorIs(t, d.Replace("../escape", nil), ErrPrivate)
}

func TestPrivateBusyLockRetriesWithoutSpinning(t *testing.T) {
	d, err := OpenPrivateDir(privateRoot(t))
	require.NoError(t, err)
	defer func() { _ = d.Close() }()
	attempts := 0
	d.flock = func(int, int) error { attempts++; return unix.EWOULDBLOCK }
	_, err = d.Lock(context.Background(), 100*time.Millisecond)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Greater(t, attempts, 1)
	require.LessOrEqual(t, attempts, 30, "a busy lock must back off instead of spinning")
}
