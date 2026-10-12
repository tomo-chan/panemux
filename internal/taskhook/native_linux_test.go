package taskhook

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func procFixture(t *testing.T) string {
	t.Helper()
	original := procRoot
	procRoot = t.TempDir()
	t.Cleanup(func() { procRoot = original })
	root := filepath.Join(procRoot, "42")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "fd"), 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "sys/kernel/random"), 0700))
	fields := append([]string{"S", "1"}, strings.Fields(strings.Repeat("0 ", 17))...)
	fields = append(fields, "123")
	require.NoError(t, os.WriteFile(filepath.Join(root, "stat"), []byte("42 (fixture) "+strings.Join(fields, " ")), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(procRoot, "sys/kernel/random/boot_id"), []byte(testSession), 0600))
	executable := filepath.Join(procRoot, "binary")
	require.NoError(t, os.WriteFile(executable, []byte("fixture"), 0600))
	require.NoError(t, os.Symlink(executable, filepath.Join(root, "exe")))
	require.NoError(t, os.Symlink(executable, filepath.Join(root, "fd/0")))
	return root
}

func TestLinuxProcIdentityRejectsPartialOrCorruptKernelMetadata(t *testing.T) {
	root := procFixture(t)
	p, err := platformProcess(42)
	require.NoError(t, err)
	require.Equal(t, 42, p.PID)
	require.Equal(t, 1, p.PPID)
	require.Equal(t, testSession+":123", p.Birth)
	for _, data := range []string{"bad", "42 (fixture) S 1", "42 (fixture) S invalid " + strings.Repeat("0 ", 18),
		"42 (fixture) S 1 " + strings.Repeat("0 ", 17) + "invalid"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "stat"), []byte(data), 0600))
		_, err = platformProcess(42)
		require.Error(t, err)
	}
	require.NoError(t, os.Remove(filepath.Join(root, "stat")))
	_, err = platformProcess(42)
	require.Error(t, err)
	_, err = platformProcess(99)
	require.Error(t, err)
	root = procFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(procRoot, "sys/kernel/random/boot_id"), []byte("invalid"), 0600))
	_, err = platformProcess(42)
	require.Error(t, err)
	require.NoError(t, os.Remove(filepath.Join(procRoot, "sys/kernel/random/boot_id")))
	_, err = platformProcess(42)
	require.Error(t, err)
	for _, size := range []int{4096, 4097} {
		file := filepath.Join(root, "bounded")
		require.NoError(t, os.WriteFile(file, []byte(strings.Repeat("x", size)), 0600))
		data, readErr := smallProcFile(file)
		if size == 4096 {
			require.NoError(t, readErr)
			require.Len(t, data, size)
		} else {
			require.Error(t, readErr)
		}
	}
	_, err = smallProcFile(root)
	require.Error(t, err)
}

func TestLinuxProcOpenFilesAcceptFDZeroAndRejectUnknownSet(t *testing.T) {
	root := procFixture(t)
	files, err := platformFiles(context.Background(), 42)
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.True(t, files[0].executable)
	require.NotEmpty(t, files[1].id)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = platformFiles(ctx, 42)
	require.Error(t, err)
	require.NoError(t, os.Rename(filepath.Join(root, "fd/0"), filepath.Join(root, "fd/-1")))
	_, err = platformFiles(context.Background(), 42)
	require.Error(t, err)
	require.NoError(t, os.Remove(filepath.Join(root, "fd/-1")))
	require.NoError(t, os.WriteFile(filepath.Join(root, "fd/0"), nil, 0600))
	_, err = platformFiles(context.Background(), 42)
	require.Error(t, err)
	require.NoError(t, os.Remove(filepath.Join(root, "fd/0")))
	require.NoError(t, os.Remove(filepath.Join(root, "fd")))
	_, err = platformFiles(context.Background(), 42)
	require.Error(t, err)
	require.NoError(t, os.Remove(filepath.Join(root, "exe")))
	_, err = platformFiles(context.Background(), 42)
	require.Error(t, err)
}

func TestLinuxOpenSetExactDescriptorBudgetAndOverflow(t *testing.T) {
	root := procFixture(t)
	for i := 1; i < MaxHeldFiles; i++ {
		require.NoError(t, os.Symlink(filepath.Join(procRoot, "binary"), filepath.Join(root, "fd", strconv.Itoa(i))))
	}
	files, err := platformFiles(context.Background(), 42)
	require.NoError(t, err)
	require.Len(t, files, MaxHeldFiles+1)
	overflow := filepath.Join(root, "fd", strconv.Itoa(MaxHeldFiles))
	require.NoError(t, os.Symlink(filepath.Join(procRoot, "binary"), overflow))
	_, err = platformFiles(context.Background(), 42)
	require.Error(t, err)
}
