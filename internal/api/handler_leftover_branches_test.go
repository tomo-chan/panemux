package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The branch probe is optional metadata: a failure means the pane header
// shows no branch, never that the git context is unusable. What the arm
// actually does is *discard* whatever the failing command printed, and that
// is the part worth pinning: a non-zero git still writes to stdout, and
// showing that text as a branch name would be worse than showing none.
//
// The fixture is therefore a git that prints and then fails, not a directory
// that is not a repository. A non-repository makes git exit 128 with empty
// stdout, so `cmd.Output()` has already returned an empty branchOut before
// the arm clears it — a test built that way stays green with
// `branchOut = nil` deleted, which is how this fixture came to be.
//
// Also worth recording, since the code says otherwise: the arm's comment
// attributes the failure to a detached HEAD, and on current git that is not
// true — `git branch --show-current` on a detached HEAD exits 0 with empty
// output (verified). The fallback is right either way; its stated reason is
// stale.
func TestLocalGitOptionalMetadataDiscardsOutputFromAFailedProbe(t *testing.T) {
	stubGitOnPath(t, "printf 'not-a-branch-name\n'\nexit 1\n")

	branch, origin := localGitOptionalMetadata(t.TempDir())

	assert.Empty(t, branch,
		"a failing probe's stdout must be discarded, not shown to the operator as a branch")
	assert.Empty(t, origin)
}

// stubGitOnPath puts a `git` on PATH that runs body and nothing else, so a
// test can choose both what the probe prints and how it exits. Scoped to the
// calling test by t.Setenv, so the tests around it still use the real git.
func stubGitOnPath(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "git")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0600))
	// Written 0600 and made executable afterwards rather than written 0700:
	// gosec's G306 caps WriteFile at 0600, and a chmod is clearer here than a
	// suppression.
	require.NoError(t, os.Chmod(path, 0700))
	t.Setenv("PATH", dir)
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
