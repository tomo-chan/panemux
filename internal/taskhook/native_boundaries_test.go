package taskhook

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func heldFixture(t *testing.T) (HeldSession, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(root, 0700))
	path := filepath.Join(root, "nested", "rollout-test.jsonl")
	require.NoError(t, os.Mkdir(filepath.Dir(path), 0700))
	data := metadataLog(`{"type":"turn_context","payload":{"turn_id":"` + testTurn + `","approvals_reviewer":"user"}}`)
	require.NoError(t, os.WriteFile(path, []byte(data), 0600))
	id, err := FileIdentity(path)
	require.NoError(t, err)
	return HeldSession{Root: root, Path: path, FileID: id, SessionID: testSession}, data
}

func TestNativePathChecksCannotEscapeOrReadUnsafeFiles(t *testing.T) {
	for _, failure := range []string{"relative", "outside", "root-is-file", "missing-root", "symlink-root", "shared-root",
		"missing-component", "shared-component", "foreign-component", "missing-file", "shared-file",
		"hardlink", "directory"} {
		t.Run(failure, func(t *testing.T) {
			f, _ := heldFixture(t)
			uid := os.Geteuid()
			switch failure {
			case "relative":
				f.Path = "relative"
			case "outside":
				f.Path = filepath.Join(filepath.Dir(f.Root), "outside")
			case "root-is-file":
				f.Root = f.Path
			case "missing-root":
				f.Root = filepath.Join(f.Root, "missing")
				f.Path = filepath.Join(f.Root, "file")
			case "symlink-root":
				alias := f.Root + "-alias"
				require.NoError(t, os.Symlink(f.Root, alias))
				t.Cleanup(func() { _ = os.Remove(alias) })
				f.Path = alias + f.Path[len(f.Root):]
				f.Root = alias
			case "shared-root":
				require.NoError(t, os.Chmod(f.Root, 0777))
			case "missing-component":
				f.Path = filepath.Join(f.Root, "missing", "rollout-test.jsonl")
			case "shared-component":
				require.NoError(t, os.Chmod(filepath.Dir(f.Path), 0777))
			case "foreign-component":
				uid++
			case "missing-file":
				f.Path = filepath.Join(f.Root, "nested", "missing")
			case "shared-file":
				require.NoError(t, os.Chmod(f.Path, 0777))
			case "hardlink":
				require.NoError(t, os.Link(f.Path, f.Path+".link"))
			case "directory":
				f.Path = filepath.Dir(f.Path)
			}
			opened, err := openHeld(f, uid)
			require.Error(t, err)
			require.Nil(t, opened)
		})
	}
	for _, root := range []string{"relative", "/", "/workspace/../other"} {
		_, err := openRoot(root)
		require.Error(t, err)
	}
	original := openNative
	defer func() { openNative = original }()
	openNative = func(string, int, uint32) (int, error) { return -1, unix.EMFILE }
	_, err := openRoot("/workspace")
	require.Error(t, err)
}

func TestNativeHeldMetadataAndSnapshotsRejectUnknownOrChangedEvidence(t *testing.T) {
	p := Permission{SessionID: testSession, TurnID: testTurn, ToolKind: ToolBash}
	inputs := []string{"partial", `{"type":"session_meta","payload":{"id":"` + testSession + `","source":"other"}}` + "\n"}
	for _, data := range inputs {
		f, _ := heldFixture(t)
		require.NoError(t, os.WriteFile(f.Path, []byte(data), 0600))
		_, err := heldMetadata(f.Root, f.Path, f.FileID, os.Geteuid())
		require.Error(t, err)
	}
	f, _ := heldFixture(t)
	h := NewNativeHost(os.Geteuid())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.Scan(ctx, f, p)
	require.Error(t, err)
	missing := f
	missing.Path = filepath.Join(f.Root, "missing")
	_, err = h.Scan(context.Background(), missing, p)
	require.Error(t, err)
	require.NoError(t, os.Truncate(f.Path, MaxRolloutBytes+1))
	_, err = h.Scan(context.Background(), f, p)
	require.Error(t, err)
	f, log := heldFixture(t)
	require.NoError(t, os.WriteFile(f.Path, []byte("broken\n"), 0600))
	_, err = h.Scan(context.Background(), f, p)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(f.Path, []byte(log), 0600))
	h.scanSnapshot = func(reader io.Reader, p Permission, id string) (RolloutEvidence, error) {
		e, scanErr := ScanRollout(reader, p, id)
		require.NoError(t, os.WriteFile(f.Path, []byte(log+"\n"), 0600))
		return e, scanErr
	}
	_, err = h.Scan(context.Background(), f, p)
	require.Error(t, err)
}

func TestNativeFilesBudgetAndFailedInspectionNeverSupplyBinding(t *testing.T) {
	h := NewNativeHost(os.Geteuid())
	pid := os.Getpid()
	ctx := context.Background()
	_, err := h.Files(ctx, 0, "/workspace", "exe")
	require.Error(t, err)
	for _, count := range []int{MaxHeldFiles, MaxHeldFiles + 1} {
		h.files = func(context.Context, int) ([]heldFile, error) {
			files := make([]heldFile, count)
			files[0] = heldFile{id: "exe", executable: true}
			return files, nil
		}
		files, readErr := h.Files(ctx, pid, "/workspace", "exe")
		if count == MaxHeldFiles {
			require.NoError(t, readErr)
			require.True(t, files.Native)
		} else {
			require.Error(t, readErr)
		}
	}
	h.files = func(context.Context, int) ([]heldFile, error) { return nil, ErrUnsupported }
	_, err = h.Files(ctx, pid, "/workspace", "exe")
	require.Error(t, err)
	f, _ := heldFixture(t)
	require.NoError(t, os.WriteFile(f.Path, []byte("broken\n"), 0600))
	h.files = func(context.Context, int) ([]heldFile, error) { return []heldFile{{path: f.Path, id: f.FileID}}, nil }
	_, err = h.Files(ctx, pid, f.Root, "exe")
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.files = func(context.Context, int) ([]heldFile, error) { cancel(); return nil, nil }
	_, err = h.Files(ctx, pid, f.Root, "exe")
	require.Error(t, err)
	h = NewNativeHost(os.Geteuid() + 1)
	_, err = h.Process(context.Background(), pid)
	require.Error(t, err)
	_, err = NewNativeHost(os.Geteuid()).Process(context.Background(), int(^uint32(0)>>1))
	require.Error(t, err)
	_, err = FileIdentity(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
	_, err = FileIdentity(t.TempDir())
	require.Error(t, err)
}

func TestMissingHeldFileCannotSupplySessionMetadata(t *testing.T) {
	f, _ := heldFixture(t)
	require.NoError(t, os.Remove(f.Path))
	_, err := heldMetadata(f.Root, f.Path, f.FileID, os.Geteuid())
	require.Error(t, err)
}

func TestNativeProcessBudgetRejectsInitButAcceptsFirstEligiblePID(t *testing.T) {
	h := NewNativeHost(os.Geteuid())
	h.process = func(pid int) (ProcessIdentity, error) {
		return ProcessIdentity{PID: pid, PPID: 1, UID: os.Geteuid(), Birth: "kernel:0"}, nil
	}
	for _, pid := range []int{0, 1, 2} {
		_, err := h.Process(context.Background(), pid)
		require.Equal(t, pid < 2, err != nil)
	}
}
