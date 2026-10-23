package tasks

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Temporary diagnostic for issue #423; removed before review.
func TestDiag423(t *testing.T) {
	for _, c := range []string{
		"cat /etc/os-release | head -3",
		"ls -l /usr/bin/sleep /usr/bin/mkdir; readlink -f /usr/bin/sleep",
		"sleep --version | head -1; mkdir --version | head -1",
		"tr --version | head -1; readlink --version | head -1",
		`cd /tmp && env -i A="x
PANEMUX_PANE_ID=bad" PANEMUX_PANE_ID=good sleep 5 & p=$!; sleep 0.3
readlink /proc/$p/cwd; tr '\n\000' '\001\n' </proc/$p/environ | od -c | head
tr '\n\000' '\001\n' </proc/$p/environ | grep -m 1 '^PANEMUX_PANE_ID='; kill $p`,
	} {
		out, err := exec.Command("sh", "-c", c).CombinedOutput()
		t.Logf("$ %s\n%s(err=%v)", c, out, err)
	}
	dir := t.TempDir()
	p, _ := exec.LookPath("sleep")
	_ = os.Symlink(p, filepath.Join(dir, "claude"))
	out, err := exec.Command(filepath.Join(dir, "claude"), "1").CombinedOutput() //nolint:gosec // G204: diagnostic fixture
	t.Logf("symlinked sleep: %q err=%v", out, err)
	_ = os.WriteFile(filepath.Join(dir, ".local"), nil, 0o600)
	out, err = exec.Command("sh", "-c", `mkdir -p "$1/.local/bin"`, "sh", dir).CombinedOutput()
	t.Logf("mkdir -p: %q err=%v", out, err)
}
