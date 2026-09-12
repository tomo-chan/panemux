// Package homedir is panemux's single seam onto os.UserHomeDir.
//
// DEVELOPMENT.md's testability rule requires an injectable override wherever
// production code reaches for a global singleton, and a home directory is one:
// it is process-wide, environment-dependent, and different on the machine a
// test runs on than on the machine the code was written for.
//
// The seam lives in one package rather than one per caller because the
// callers do not line up with the tests. internal/api's handler tests drive
// tilde expansion that happens inside internal/config, internal/server's
// integration tests drive config, api and session at once, and the root
// package's tests drive internal/commandcenter's default paths. A per-package
// variable cannot be substituted from another package's test, so those tests
// would have had to keep mutating $HOME — which is what this exists to stop.
//
// Callers must not name os.UserHomeDir directly; .golangci.yml's forbidigo
// rule fails the build on any call outside this package, so the seam cannot
// quietly stop being the single entry point.
//
// internal/cachedir is its sibling, for os.UserCacheDir — a separate package
// because that is a separate global with its own platform behavior, and
// because one merged package would have to be excused from both forbidigo
// rules at once. See its own doc comment.
package homedir

import "os"

// dirFn is the seam itself. SetForTest and SetFailingForTest are the only
// supported ways to replace it, and both restore it when the test ends.
var dirFn = os.UserHomeDir

// Dir returns the current user's home directory, exactly as os.UserHomeDir
// does — including its error when there is none to resolve.
func Dir() (string, error) {
	return dirFn()
}

// TestingT is the subset of *testing.T the helpers below need. It is declared
// here rather than importing "testing" so that a package linking homedir into
// the panemux binary does not also link the testing package into it.
type TestingT interface {
	Helper()
	Cleanup(func())
}

// SetForTest points Dir at home for the duration of the test, restoring the
// previous value afterwards.
//
// This replaces t.Setenv("HOME", ...), which was the workaround before the seam
// existed. What it buys: the process environment is left alone, so nothing else
// in the binary — or any subprocess it starts — sees the substitution; it works
// on Windows, where os.UserHomeDir reads USERPROFILE and setting HOME does
// nothing; and a failure can be injected directly rather than conjured out of
// os.UserHomeDir rejecting an empty $HOME, which only happens on Unix.
//
// What it does NOT buy, stated plainly because the shape invites the
// assumption: this is not safe under t.Parallel. dirFn is one unsynchronized
// variable for the whole test binary, exactly as $HOME was, so two parallel
// tests substituting it race, and the loser silently reads the other's home
// directory. t.Setenv is in one way better here — it panics rather than
// letting that happen. Substitute the seam only from tests that do not call
// t.Parallel; no test in this repository does.
//
// That limitation is a recorded decision, not an open question (issue #227,
// and DEVELOPMENT.md's testability rule, which must continue to agree with
// this comment). Adding a mutex here was considered and rejected: it would
// remove the data race without making the behavior correct, since restore
// stays last-writer-wins and one test's Cleanup can still restore over
// another's live substitution — the appearance of a guarantee rather than the
// guarantee. The only shape that genuinely works under t.Parallel is per-test
// injection with no package-level variable at all, which means changing
// exported signatures across six packages; that is the route to take if a
// concrete need for t.Parallel ever appears, rather than a lock here.
func SetForTest(t TestingT, home string) {
	t.Helper()
	setForTest(t, func() (string, error) { return home, nil })
}

// SetFailingForTest makes Dir report err for the duration of the test,
// restoring the previous value afterwards.
//
// This replaces t.Setenv("HOME", ""), which reached the no-home-directory arms
// only on Unix, and only as a side effect of os.UserHomeDir rejecting an empty
// $HOME there. Injecting the error states the intent instead of relying on it.
func SetFailingForTest(t TestingT, err error) {
	t.Helper()
	setForTest(t, func() (string, error) { return "", err })
}

func setForTest(t TestingT, fn func() (string, error)) {
	original := dirFn
	t.Cleanup(func() { dirFn = original })
	dirFn = fn
}
