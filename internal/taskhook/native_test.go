package taskhook

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeProcessIdentityAndHeldFileChecks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	h := NewNativeHost(os.Geteuid())
	p, err := h.Process(ctx, os.Getpid())
	if err != nil || p.PID != os.Getpid() || p.PPID != os.Getppid() || p.UID != os.Geteuid() || p.Birth == "" {
		t.Fatalf("process proof unavailable: %v", err)
	}
	root := t.TempDir()
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "rollout-test.jsonl")
	log := metadataLog(`{"type":"turn_context","payload":{"turn_id":"` + testTurn + `","approvals_reviewer":"user"}}`)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err = f.WriteString(log); err != nil {
		t.Fatal(err)
	}
	id, err := FileIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	executableID := currentExecutableID(t)
	files, err := h.Files(ctx, os.Getpid(), root, executableID)
	if err != nil || !files.Native || len(files.Sessions) != 1 || files.Sessions[0].FileID != id {
		t.Fatalf("open-set or executable identity not verified: %v", err)
	}
	file := HeldSession{SessionID: testSession, FileID: id, Path: path, Root: root}
	e, err := h.Scan(ctx, file, Permission{SessionID: testSession, TurnID: testTurn, ToolKind: ToolBash})
	if err != nil || e.Anchor.FileID != id {
		t.Fatalf("held read: %v", err)
	}
	link := filepath.Join(root, "rollout-link.jsonl")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	file.Path = link
	if _, err = h.Scan(ctx, file, Permission{SessionID: testSession, TurnID: testTurn, ToolKind: ToolBash}); err == nil {
		t.Fatal("followed link")
	}
	file.Path = path
	file.FileID = "replacement"
	if _, err = h.Scan(ctx, file, Permission{SessionID: testSession, TurnID: testTurn, ToolKind: ToolBash}); err == nil {
		t.Fatal("inode mismatch accepted")
	}
}

func currentExecutableID(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	id, err := FileIdentity(executable)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
