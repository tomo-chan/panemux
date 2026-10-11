package taskhook

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const MaxHeldFiles = 2048

type NativeHost struct{ uid int }

func NewNativeHost(uid int) *NativeHost { return &NativeHost{uid: uid} }

func statIdentity(st unix.Stat_t) string { return fmt.Sprintf("%x:%x", st.Dev, st.Ino) }

// FileIdentity is for a launcher's trusted, resolved executable path. It never
// executes a program or consumes a transcript path from hook stdin.
func FileIdentity(path string) (string, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", ErrUnsupported
	}
	defer func() { _ = unix.Close(fd) }()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || uint32(st.Mode)&unix.S_IFMT != unix.S_IFREG || uint32(st.Mode)&022 != 0 {
		return "", ErrUnsupported
	}
	return statIdentity(st), nil
}

func (h *NativeHost) Process(ctx context.Context, pid int) (ProcessIdentity, error) {
	if ctx.Err() != nil || pid <= 1 || h.uid != os.Geteuid() || os.Getuid() != os.Geteuid() {
		return ProcessIdentity{}, ErrUnsupported
	}
	p, err := platformProcess(pid)
	if err != nil || p.UID != h.uid || p.PID != pid || !opaque.MatchString(p.Birth) {
		return ProcessIdentity{}, ErrUnsupported
	}
	return p, nil
}

func openRoot(root string) (int, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return -1, ErrUnsupported
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, ErrUnsupported
	}
	for _, part := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		if part == "" {
			_ = unix.Close(fd)
			return -1, ErrUnsupported
		}
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return -1, ErrUnsupported
		}
		fd = next
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || int(st.Uid) != os.Geteuid() || uint32(st.Mode)&022 != 0 {
		_ = unix.Close(fd)
		return -1, ErrUnsupported
	}
	return fd, nil
}

func openHeld(f HeldSession, uid int) (*os.File, error) {
	if !filepath.IsAbs(f.Path) || filepath.Clean(f.Path) != f.Path {
		return nil, ErrUnsupported
	}
	rel, err := filepath.Rel(f.Root, f.Path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, ErrUnsupported
	}
	fd, err := openRoot(f.Root)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return nil, ErrUnsupported
		}
		fd = next
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || int(st.Uid) != uid || uint32(st.Mode)&022 != 0 {
			_ = unix.Close(fd)
			return nil, ErrUnsupported
		}
	}
	opened, err := unix.Openat(fd, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	_ = unix.Close(fd)
	if err != nil {
		return nil, ErrUnsupported
	}
	var st unix.Stat_t
	if unix.Fstat(opened, &st) != nil || uint32(st.Mode)&unix.S_IFMT != unix.S_IFREG || int(st.Uid) != uid ||
		uint32(st.Mode)&022 != 0 || st.Nlink != 1 || statIdentity(st) != f.FileID {

		_ = unix.Close(opened)
		return nil, ErrUnsupported
	}
	return os.NewFile(uintptr(opened), "verified-rollout"), nil
}

func heldMetadata(root, path, id string, uid int) (HeldSession, error) {
	f := HeldSession{Root: root, Path: path, FileID: id}
	opened, err := openHeld(f, uid)
	if err != nil {
		return HeldSession{}, err
	}
	defer func() { _ = opened.Close() }()
	line, err := bufio.NewReaderSize(opened, MaxRolloutLineBytes).ReadSlice('\n')
	if err != nil {
		return HeldSession{}, ErrUnsupported
	}
	var item rolloutMetadata
	if !decodeMetadata(line, &item) || item.Type != "session_meta" || !uuid.MatchString(item.Payload.ID) ||
		string(item.Payload.Source) != `"cli"` {

		return HeldSession{}, ErrUnsupported
	}
	f.SessionID = item.Payload.ID
	return f, nil
}

func (h *NativeHost) Scan(ctx context.Context, f HeldSession, p Permission) (RolloutEvidence, error) {
	if ctx.Err() != nil {
		return RolloutEvidence{}, ErrUnsupported
	}
	opened, err := openHeld(f, h.uid)
	if err != nil {
		return RolloutEvidence{}, err
	}
	defer func() { _ = opened.Close() }()
	before, err := opened.Stat()
	if err != nil || before.Size() > MaxRolloutBytes {
		return RolloutEvidence{}, ErrUnsupported
	}
	e, err := ScanRollout(io.NewSectionReader(opened, 0, before.Size()), p, f.FileID)
	if err != nil {
		return RolloutEvidence{}, err
	}
	after, err := opened.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || ctx.Err() != nil {
		return RolloutEvidence{}, ErrUnsupported
	}
	return e, nil
}
