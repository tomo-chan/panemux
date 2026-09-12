// Package cachedir is panemux's single seam onto os.UserCacheDir.
//
// It exists for the same reason internal/homedir does — DEVELOPMENT.md's
// testability rule requires an injectable override wherever production code
// reaches for a process-wide global — and it is a second package rather than
// a second function there because os.UserCacheDir is a different global with
// different platform behavior. It never consults os.UserHomeDir: on a
// non-darwin Unix it reads $XDG_CACHE_HOME and falls back to $HOME/.cache,
// while on darwin it reads $HOME/Library/Caches and ignores XDG_CACHE_HOME
// entirely. So substituting the home directory cannot influence it, and no
// single environment variable covers it either.
//
// That asymmetry is exactly what a shared package would bury: each seam's doc
// comment is about the platform behavior of its own variable. Keeping them
// apart also keeps .golangci.yml's forbidigo exclusions narrow — each package
// is excused from naming one function, so a stray os.UserHomeDir inside this
// package is still caught, which a merged package excused from both rules
// could not be.
//
// Callers must not name os.UserCacheDir directly; .golangci.yml's forbidigo
// rule fails the build on any call outside this package, so the seam cannot
// quietly stop being the single entry point.
package cachedir

import "os"

// dirFn is the seam itself. SetForTest and SetFailingForTest are the only
// supported ways to replace it, and both restore it when the test ends.
//
// The forbidigo waiver is on this line rather than on the package, so that an
// os.UserHomeDir call added in here is still caught — see internal/homedir's
// copy of this note.
var dirFn = os.UserCacheDir //nolint:forbidigo // this package is the seam; every other caller goes through Dir()

// Dir returns the current user's cache directory, exactly as os.UserCacheDir
// does — including its error when there is none to resolve.
func Dir() (string, error) {
	return dirFn()
}

// TestingT is the subset of *testing.T the helpers below need. It is declared
// here rather than importing "testing" so that a package linking cachedir into
// the panemux binary does not also link the testing package into it.
type TestingT interface {
	Helper()
	Cleanup(func())
}

// SetForTest points Dir at cache for the duration of the test, restoring the
// previous value afterwards.
//
// This replaces t.Setenv("XDG_CACHE_HOME", ...) plus t.Setenv("HOME", ...) —
// both were needed, since neither variable alone decides the answer on every
// platform, and getting that pair wrong wrote real files into a developer's
// own cache directory on macOS while CI (Linux-only) stayed green.
//
// What it does NOT buy, the same as internal/homedir's own helper: this is not
// safe under t.Parallel. dirFn is one unsynchronized variable for the whole
// test binary, so two parallel tests substituting it race, and the loser
// silently reads the other's cache directory — where t.Setenv would have
// panicked instead. That limitation is deliberate and recorded: see issue
// #227 and DEVELOPMENT.md's testability rule. Substitute the seam only from
// tests that do not call t.Parallel; no test in this repository does.
func SetForTest(t TestingT, cache string) {
	t.Helper()
	setForTest(t, func() (string, error) { return cache, nil })
}

// SetFailingForTest makes Dir report err for the duration of the test,
// restoring the previous value afterwards. It reaches the arms that run when
// there is no cache directory to resolve at all, which no combination of
// environment variables can produce on every platform.
func SetFailingForTest(t TestingT, err error) {
	t.Helper()
	setForTest(t, func() (string, error) { return "", err })
}

func setForTest(t TestingT, fn func() (string, error)) {
	original := dirFn
	t.Cleanup(func() { dirFn = original })
	dirFn = fn
}
