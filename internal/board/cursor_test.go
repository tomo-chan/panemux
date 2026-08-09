package board

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileCursorStore_LoadMissingFileReturnsEmpty(t *testing.T) {
	s := NewFileCursorStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	cursors, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cursors) != 0 {
		t.Fatalf("expected empty map, got %+v", cursors)
	}
}

func TestFileCursorStore_SaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "board-relay-cursor.json")
	s := NewFileCursorStore(path)

	want := map[CursorKey]string{
		{Host: "local", Team: "panemux"}:      "42",
		{Host: "build-host", Team: "panemux"}: "7",
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("cursor[%+v] = %q, want %q", k, got[k], v)
		}
	}
}

// Simulates a panemux restart: a fresh store pointed at the same file must
// resume from the persisted cursor, not from empty.
func TestFileCursorStore_SurvivesSimulatedRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board-relay-cursor.json")

	first := NewFileCursorStore(path)
	if err := first.Save(map[CursorKey]string{{Host: "local", Team: "panemux"}: "99"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	second := NewFileCursorStore(path)
	got, err := second.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got[CursorKey{Host: "local", Team: "panemux"}] != "99" {
		t.Fatalf("expected cursor to survive restart, got %+v", got)
	}
}

func TestMemCursorStore_RoundTrips(t *testing.T) {
	s := NewMemCursorStore()
	want := map[CursorKey]string{{Host: "local", Team: "panemux"}: "1"}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got[CursorKey{Host: "local", Team: "panemux"}] != "1" {
		t.Fatalf("got %+v", got)
	}
}

func TestDefaultCursorPath_UsesInjectableHomeDir(t *testing.T) {
	orig := userHomeDirFn
	defer func() { userHomeDirFn = orig }()
	userHomeDirFn = func() (string, error) { return "/home/testuser", nil }

	path, err := DefaultCursorPath()
	if err != nil {
		t.Fatalf("DefaultCursorPath: %v", err)
	}
	want := filepath.Join("/home/testuser", ".config", "panemux", "board-relay-cursor.json")
	if path != want {
		t.Fatalf("DefaultCursorPath() = %q, want %q", path, want)
	}
}

func TestDefaultCursorPath_HomeDirError(t *testing.T) {
	orig := userHomeDirFn
	defer func() { userHomeDirFn = orig }()
	userHomeDirFn = func() (string, error) { return "", errors.New("no home") }

	if _, err := DefaultCursorPath(); err == nil {
		t.Fatal("expected an error when the home directory cannot be resolved")
	}
}

// Distinct from TestFileCursorStore_LoadMissingFileReturnsEmpty: a read
// error that is NOT os.ErrNotExist (here, pointing at a directory instead of
// a file) must propagate as an error, not be treated as "fresh install."
func TestFileCursorStore_Load_NonNotExistReadErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	s := NewFileCursorStore(dir)
	if _, err := s.Load(); err == nil {
		t.Fatal("expected an error when the cursor path is a directory")
	}
}

func TestFileCursorStore_Load_MalformedJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board-relay-cursor.json")
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := NewFileCursorStore(path)
	if _, err := s.Load(); err == nil {
		t.Fatal("expected an error for malformed cursor file JSON")
	}
}

// A regular file occupying a path component that Save needs as a directory
// makes os.MkdirAll fail (ENOTDIR).
func TestFileCursorStore_Save_MkdirAllFailurePropagates(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := NewFileCursorStore(filepath.Join(blocker, "subdir", "board-relay-cursor.json"))
	if err := s.Save(map[CursorKey]string{{Host: "local", Team: "panemux"}: "1"}); err == nil {
		t.Fatal("expected an error when the parent directory cannot be created")
	}
}

// A directory occupying the target file path makes os.WriteFile fail.
func TestFileCursorStore_Save_WriteFileFailurePropagates(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "board-relay-cursor.json")
	if err := os.MkdirAll(target, 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	s := NewFileCursorStore(target)
	if err := s.Save(map[CursorKey]string{{Host: "local", Team: "panemux"}: "1"}); err == nil {
		t.Fatal("expected an error when the target path is itself a directory")
	}
}
