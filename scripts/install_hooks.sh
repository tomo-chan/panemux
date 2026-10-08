#!/bin/sh
# Points core.hooksPath at this checkout's .githooks, so pre-push runs
# `make check`. Run by `make install-hooks`, from the repository root.
#
# Writes .git/config only when it has to: when core.hooksPath already leads to
# an executable pre-push identical to ours — `.githooks` itself, or the main
# checkout's copy named by absolute path from a worktree — the hooks are
# installed and the write is skipped. Git silently skips a hook without the
# executable bit, so an identical but non-executable copy is not installed. The Claude Code sandbox refuses writes to .git/config,
# and an unconditional write stopped `make install-deps` there (issue #315).

set -u

chmod +x .githooks/pre-push || exit 1

current=$(git config --get core.hooksPath) || current=""
if [ -n "$current" ] && [ -f "$current/pre-push" ] && [ -x "$current/pre-push" ] && cmp -s "$current/pre-push" .githooks/pre-push; then
	echo "install-hooks: core.hooksPath ($current) already runs this pre-push; .git/config left unchanged"
	exit 0
fi
git config core.hooksPath .githooks
