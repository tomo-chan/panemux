package cachedir_test

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/cachedir"
)

// Unsubstituted, Dir is os.UserCacheDir and nothing else. A seam that quietly
// answered something of its own would make every caller's default wrong.
func TestDirDefaultsToTheOperatingSystemCacheDirectory(t *testing.T) {
	want, wantErr := os.UserCacheDir() //nolint:forbidigo // the seam's own default is what this asserts

	got, err := cachedir.Dir()

	if wantErr != nil {
		require.Error(t, err)
		return
	}
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestSetForTestSubstitutesTheCacheDirectory(t *testing.T) {
	cachedir.SetForTest(t, "/workspace/user/cache")

	got, err := cachedir.Dir()

	require.NoError(t, err)
	assert.Equal(t, "/workspace/user/cache", got)
}

func TestSetFailingForTestSubstitutesAFailure(t *testing.T) {
	sentinel := errors.New("no cache directory")
	cachedir.SetFailingForTest(t, sentinel)

	got, err := cachedir.Dir()

	require.ErrorIs(t, err, sentinel, "callers wrap this error, so it has to survive as itself")
	assert.Empty(t, got)
}

// The restore runs through t.Cleanup, so it is the enclosing subtest ending
// that has to put the previous value back — not the process exiting. Without
// that, one test's substitution would leak into every test after it.
func TestSubstitutionIsRestoredWhenTheTestEnds(t *testing.T) {
	before, beforeErr := cachedir.Dir()

	t.Run("substituted", func(t *testing.T) {
		cachedir.SetForTest(t, "/workspace/user/other-cache")
		got, err := cachedir.Dir()
		require.NoError(t, err)
		assert.Equal(t, "/workspace/user/other-cache", got)
	})

	after, afterErr := cachedir.Dir()
	assert.Equal(t, before, after)
	assert.Equal(t, beforeErr == nil, afterErr == nil)
}

// Nesting has to unwind in order: the inner substitution restores the outer
// one, not the operating system's answer.
func TestNestedSubstitutionsRestoreTheEnclosingValue(t *testing.T) {
	cachedir.SetForTest(t, "/workspace/user/outer-cache")

	t.Run("inner", func(t *testing.T) {
		cachedir.SetFailingForTest(t, errors.New("no cache directory"))
		_, err := cachedir.Dir()
		require.Error(t, err)
	})

	got, err := cachedir.Dir()
	require.NoError(t, err)
	assert.Equal(t, "/workspace/user/outer-cache", got)
}

// SetForTest calls Helper so a failure inside a helper that wraps it is
// reported at the caller's line rather than inside cachedir.
func TestHelpersMarkThemselvesAsHelpers(t *testing.T) {
	spy := &helperSpy{}

	cachedir.SetForTest(spy, "/workspace/user/cache")
	cachedir.SetFailingForTest(spy, errors.New("no cache directory"))

	assert.Equal(t, 2, spy.helperCalls)
	require.Len(t, spy.cleanups, 2)
	// LIFO, as testing.T unwinds cleanups. Running these in registration order
	// would end with dirFn holding SetForTest's closure — the value
	// SetFailingForTest captured as its "original" — rather than the real
	// os.UserCacheDir, and spy is not a *testing.T, so nothing else would put it
	// back. That leaks into every later test in the binary: go test -shuffle=on
	// then fails TestDirDefaultsToTheOperatingSystemCacheDirectory about half the
	// time, and any test added above it in this file fails permanently.
	for i := len(spy.cleanups) - 1; i >= 0; i-- {
		spy.cleanups[i]()
	}
}

type helperSpy struct {
	cleanups    []func()
	helperCalls int
}

func (s *helperSpy) Helper()           { s.helperCalls++ }
func (s *helperSpy) Cleanup(fn func()) { s.cleanups = append(s.cleanups, fn) }
