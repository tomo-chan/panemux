package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The branch probe is optional metadata: a failure means the pane header
// shows no branch, never that the git context is unusable.
//
// Worth recording, since the code says otherwise: the arm's comment
// attributes the failure to a detached HEAD, and on current git that is not
// true — `git branch --show-current` on a detached HEAD exits 0 with empty
// output (verified). What does make it exit non-zero is not being in a
// repository at all, which is what this fixture uses. The fallback is right
// either way; the stated reason for it is stale.
func TestLocalGitOptionalMetadataReturnsNothingRatherThanFailing(t *testing.T) {
	notARepo := t.TempDir()

	branch, origin := localGitOptionalMetadata(notARepo)

	assert.Empty(t, branch, "a failed probe yields no branch, not a partial one")
	assert.Empty(t, origin)
}

// The complement, so the emptiness above is not passing for want of git
// working at all.
func TestLocalGitOptionalMetadataReadsBranchAndOriginFromARepository(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "sample@example.com")
	runGit(t, repo, "config", "user.name", "Sample")
	runGit(t, repo, "config", "commit.gpgsign", "false")
	runGit(t, repo, "commit", "-q", "--allow-empty", "-m", "first")
	runGit(t, repo, "remote", "add", "origin", "https://git.example.com/sample/project.git")

	branch, origin := localGitOptionalMetadata(repo)

	assert.NotEmpty(t, branch, "a real repository has a current branch")
	assert.Equal(t, "https://git.example.com/sample/project.git", string(origin))
}

// No test for resolveSSHConfigHostAlias's empty-Hostname fallback, and the
// reason is worth writing down rather than leaving as a gap.
//
// That arm cannot run. sshconfig.ParseHosts defaults Hostname to the alias
// name when a Host block does not set one (parse.go: `if host.Hostname == ""
// { host.Hostname = host.Name }`), so no host it returns ever carries an
// empty Hostname, and the `return host` inside that check is dead code. A
// test written for it passed with the arm deleted — which is how this was
// found — and was removed rather than kept as coverage it does not provide.
