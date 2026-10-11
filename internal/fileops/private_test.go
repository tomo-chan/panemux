package fileops

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func privateRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}
func privateDir(t *testing.T, root string) *PrivateDir {
	t.Helper()
	d, err := OpenPrivateDir(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestPrivateRootRefusesUnsafeObjects(t *testing.T) {
	root := privateRoot(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, file, "relative", "", filepath.Join(t.TempDir(), "missing")} {
		if d, err := OpenPrivateDir(path); err == nil {
			_ = d.Close()
			t.Fatalf("accepted unsafe root: %s", filepath.Base(path))
		}
	}
	if err := os.Chmod(root, 0750); err != nil {
		t.Fatal(err)
	}
	if d, err := OpenPrivateDir(root); err == nil {
		_ = d.Close()
		t.Fatal("accepted shared root")
	}
}

func TestPrivateRecordSafetyAndBounds(t *testing.T) {
	root := privateRoot(t)
	d := privateDir(t, root)
	for _, name := range []string{"../escape", "/absolute", ".", "..", "a/b", "a\x00b"} {
		if _, err := d.Read(name, 16); err == nil {
			t.Fatal("unsafe name accepted")
		}
		if err := d.Replace(name, []byte("x")); err == nil {
			t.Fatal("unsafe write accepted")
		}
	}
	if _, err := d.Read("missing", 16); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	if err := d.Replace("record.json", []byte("complete")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "record.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private record: %v", err)
	}
	if _, err := d.Read("record.json", 7); !errors.Is(err, ErrPrivateLimit) {
		t.Fatalf("oversize: %v", err)
	}
	if data, err := d.Read("record.json", 8); err != nil || string(data) != "complete" {
		t.Fatalf("exact bound: %q %v", data, err)
	}
}

func TestPrivateRecordRefusesLinksAndSharedModes(t *testing.T) {
	root := privateRoot(t)
	d := privateDir(t, root)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, filepath.Join(root, "hard-link")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(root, "shared")
	if err := os.WriteFile(shared, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // Deliberately shared fixture, independent of the runner's umask.
	if err := os.Chmod(shared, 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"linked", "hard-link", "fifo", "directory", "shared"} {
		if _, err := d.Read(name, 16); err == nil {
			t.Fatalf("accepted unsafe %s", name)
		}
		if err := d.Replace(name, []byte("replacement")); err == nil {
			t.Fatalf("replaced unsafe %s", name)
		}
	}
	data, _ := os.ReadFile(target)
	if string(data) != "untouched" {
		t.Fatal("followed an unsafe link")
	}
}

func TestPrivateLockDeadlineAndRelease(t *testing.T) {
	root := privateRoot(t)
	a := privateDir(t, root)
	b := privateDir(t, root)
	release, err := a.Lock(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if unlock, lockErr := b.Lock(ctx, time.Second); !errors.Is(lockErr, context.DeadlineExceeded) {
		if unlock != nil {
			unlock()
		}
		t.Fatalf("deadline: %v", lockErr)
	}
	release()
	unlock, err := b.Lock(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	ctx, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if unlock, err := a.Lock(ctx, time.Second); !errors.Is(err, context.Canceled) {
		if unlock != nil {
			unlock()
		}
		t.Fatalf("canceled: %v", err)
	}
}

func TestPrivateAtomicFailuresPreserveOldRecord(t *testing.T) {
	for _, step := range []string{"write", "short write", "sync", "close", "rename", "directory sync"} {
		t.Run(step, func(t *testing.T) {
			root := privateRoot(t)
			d := privateDir(t, root)
			if err := d.Replace("record.json", []byte("old")); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("disk failure")
			switch step {
			case "write":
				d.write = func(io.Writer, []byte) (int, error) { return 0, failure }
			case "short write":
				d.write = func(io.Writer, []byte) (int, error) { return 1, nil }
			case "sync":
				d.sync = func(*os.File) error { return failure }
			case "close":
				d.closeFile = func(f *os.File) error { _ = f.Close(); return failure }
			case "rename":
				d.rename = func(int, string, int, string) error { return failure }
			case "directory sync":
				d.syncDir = func(int) error { return failure }
			}
			if err := d.Replace("record.json", []byte("new")); err == nil {
				t.Fatal("reported a failed commit as success")
			}
			data, err := d.Read("record.json", 16)
			if err != nil {
				t.Fatal(err)
			}
			expected := "old"
			if step == "directory sync" {
				expected = "new"
			}
			if string(data) != expected {
				t.Fatalf("record torn: %q", data)
			}
			files, _ := os.ReadDir(root)
			if len(files) != 1 {
				t.Fatal("temporary file leaked after failure")
			}
		})
	}
}

func TestPrivateDirectoryFDDoesNotFollowRootReplacement(t *testing.T) {
	root := privateRoot(t)
	d := privateDir(t, root)
	moved := root + "-moved"
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	other := privateRoot(t)
	if err := os.Symlink(other, root); err != nil {
		t.Fatal(err)
	}
	if err := d.Replace("record.json", []byte("bound-to-open-directory")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, "record.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("write followed substituted root")
	}
	data, readErr := os.ReadFile(filepath.Join(moved, "record.json"))
	if readErr != nil || string(data) != "bound-to-open-directory" {
		t.Fatal("lost directory binding")
	}
}
