package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"panemux/internal/cachedir"
)

// TestMain points the cache seam at a throwaway directory for the whole test
// binary. NewLocal installs the browser-open shim under cachedir.Dir(), so
// every test that starts a local pane without its own cachedir.SetForTest
// wrote panemux-open into the developer's real cache directory (issue #315).
// A test that substitutes the seam itself still wins: SetForTest restores
// this value, not the real one, when it ends.
func TestMain(m *testing.M) {
	os.Exit(runWithThrowawayCache(m))
}

func runWithThrowawayCache(m *testing.M) int {
	dir, err := os.MkdirTemp("", "panemux-session-cache-")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	var main mainCleanup
	cachedir.SetForTest(&main, dir)
	defer main.run()
	return m.Run()
}

// mainCleanup is the cachedir.TestingT a TestMain can offer: it runs the
// registered cleanups, in reverse, when the binary's tests are done.
type mainCleanup struct{ fns []func() }

func (*mainCleanup) Helper() {}

func (c *mainCleanup) Cleanup(fn func()) { c.fns = append(c.fns, fn) }

func (c *mainCleanup) run() {
	for i := len(c.fns) - 1; i >= 0; i-- {
		c.fns[i]()
	}
}

func TestTestsNeverResolveTheRealCacheDirectory(t *testing.T) {
	got, err := cachedir.Dir()
	if err != nil {
		t.Fatalf("cachedir.Dir: %v", err)
	}
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", os.TempDir(), err)
	}
	resolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", got, err)
	}
	if !strings.HasPrefix(resolved, tmp+string(filepath.Separator)) {
		t.Fatalf("cachedir.Dir() = %s during tests; want a directory under %s, never the developer's own cache", got, tmp)
	}
}
