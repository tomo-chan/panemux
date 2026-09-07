package homedir_test

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/homedir"
)

// Unsubstituted, Dir is os.UserHomeDir and nothing else. A seam that quietly
// answered something of its own would make every caller's default wrong.
func TestDirDefaultsToTheOperatingSystemHomeDirectory(t *testing.T) {
	want, wantErr := os.UserHomeDir()

	got, err := homedir.Dir()

	if wantErr != nil {
		require.Error(t, err)
		return
	}
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestSetForTestSubstitutesTheHomeDirectory(t *testing.T) {
	homedir.SetForTest(t, "/workspace/user/home")

	got, err := homedir.Dir()

	require.NoError(t, err)
	assert.Equal(t, "/workspace/user/home", got)
}

func TestSetFailingForTestSubstitutesAFailure(t *testing.T) {
	sentinel := errors.New("no home directory")
	homedir.SetFailingForTest(t, sentinel)

	got, err := homedir.Dir()

	require.ErrorIs(t, err, sentinel, "callers wrap this error, so it has to survive as itself")
	assert.Empty(t, got)
}

// The restore runs through t.Cleanup, so it is the enclosing subtest ending
// that has to put the previous value back — not the process exiting. Without
// that, one test's substitution would leak into every test after it.
func TestSubstitutionIsRestoredWhenTheTestEnds(t *testing.T) {
	before, beforeErr := homedir.Dir()

	t.Run("substituted", func(t *testing.T) {
		homedir.SetForTest(t, "/workspace/user/other")
		got, err := homedir.Dir()
		require.NoError(t, err)
		assert.Equal(t, "/workspace/user/other", got)
	})

	after, afterErr := homedir.Dir()
	assert.Equal(t, before, after)
	assert.Equal(t, beforeErr == nil, afterErr == nil)
}

// Nesting has to unwind in order: the inner substitution restores the outer
// one, not the operating system's answer.
func TestNestedSubstitutionsRestoreTheEnclosingValue(t *testing.T) {
	homedir.SetForTest(t, "/workspace/user/outer")

	t.Run("inner", func(t *testing.T) {
		homedir.SetFailingForTest(t, errors.New("no home directory"))
		_, err := homedir.Dir()
		require.Error(t, err)
	})

	got, err := homedir.Dir()
	require.NoError(t, err)
	assert.Equal(t, "/workspace/user/outer", got)
}

// SetForTest calls Helper so a failure inside a helper that wraps it is
// reported at the caller's line rather than inside homedir.
func TestHelpersMarkThemselvesAsHelpers(t *testing.T) {
	spy := &helperSpy{}

	homedir.SetForTest(spy, "/workspace/user/home")
	homedir.SetFailingForTest(spy, errors.New("no home directory"))

	assert.Equal(t, 2, spy.helperCalls)
	require.Len(t, spy.cleanups, 2)
	// LIFO, as testing.T unwinds cleanups. Running these in registration order
	// would end with dirFn holding SetForTest's closure — the value
	// SetFailingForTest captured as its "original" — rather than the real
	// os.UserHomeDir, and spy is not a *testing.T, so nothing else would put it
	// back. That leaks into every later test in the binary: go test -shuffle=on
	// then fails TestDirDefaultsToTheOperatingSystemHomeDirectory about half the
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
