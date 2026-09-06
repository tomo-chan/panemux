package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two loopback checks below guard GET /api/session-token, the one
// unauthenticated route that hands out the credential every other board route
// requires (docs/security.md). Both parse an address that may not have a
// port, and what they do when it does not is the difference between the guard
// holding and the guard rejecting every legitimate request.

// A Host header carries no port when the request went to the default one, so
// SplitHostPort failing is the ordinary case rather than the exceptional one:
// the whole value is the host. Treating the parse failure as "not loopback"
// would reject a perfectly normal request to http://localhost/.
func TestIsLoopbackAuthorityAcceptsAnAuthorityWithNoPort(t *testing.T) {
	for _, authority := range []string{"localhost", loopbackIPv4, "0.0.0.0"} {
		assert.True(t, isLoopbackAuthority(authority), "%q with no port is still loopback", authority)
	}
	for _, authority := range []string{"localhost:8080", loopbackIPv4 + ":8080"} {
		assert.True(t, isLoopbackAuthority(authority), "%q with a port is loopback too", authority)
	}

	assert.False(t, isLoopbackAuthority("panemux.example.com"),
		"the no-port path must not turn into an accept-everything path")
	assert.False(t, isLoopbackAuthority("panemux.example.com:8080"))
}

// RemoteAddr is net/http's own view of the socket peer and normally carries a
// port, but the same fallback applies — and here the value is checked with
// net.IP.IsLoopback rather than string equality, since a real peer address
// can be any loopback representation, including IPv4-mapped IPv6.
func TestIsLoopbackRemoteAddrAcceptsAnAddressWithNoPort(t *testing.T) {
	assert.True(t, isLoopbackRemoteAddr(loopbackIPv4))
	assert.True(t, isLoopbackRemoteAddr("::1"))
	assert.True(t, isLoopbackRemoteAddr("127.0.0.2:54321"),
		"the whole 127.0.0.0/8 range is loopback, which string equality would miss")

	assert.False(t, isLoopbackRemoteAddr("192.0.2.10"))
	assert.False(t, isLoopbackRemoteAddr("not-an-address"),
		"an unparsable address is not loopback — the fallback must not admit it")
}

// defaultCommandHistoryFn resolves its path on every call rather than once at
// construction, so both of its failures reach a live request. Each names the
// step that failed, because "resolving the path" and "reading the file" are
// different problems for the operator: the first is an environment without a
// home directory, the second a file they can go and look at.
func TestDefaultCommandHistoryFnNamesWhichStepFailed(t *testing.T) {
	t.Run("path cannot be resolved", func(t *testing.T) {
		t.Setenv("HOME", "")

		entries, err := defaultCommandHistoryFn()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolving command center history path")
		assert.Nil(t, entries)
	})

	t.Run("file cannot be read", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		// The history file lives at ~/.config/panemux/<name>. Making that
		// directory a regular file leaves the path resolvable but the read
		// failing with ENOTDIR — the second arm, reached only because the
		// first one succeeded.
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".config"), 0750))
		require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "panemux"), []byte("x\n"), 0600))

		entries, err := defaultCommandHistoryFn()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "loading command center history")
		assert.NotContains(t, err.Error(), "resolving command center history path",
			"the path resolved fine; only the read failed")
		assert.Nil(t, entries)
	})
}
