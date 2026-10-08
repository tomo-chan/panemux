#!/bin/sh
# Tests for scripts/install_hooks.sh, which `make install-hooks` runs.
#
# What it protects: `make install-deps` must not need to write .git/config
# when the hooks are already installed — the Claude Code sandbox refuses that
# write, and a refused write stopped install-deps before npm install ran
# (issue #315). An equivalent installation counts: a worktree whose
# core.hooksPath names the main checkout's .githooks by absolute path runs the
# same pre-push.
#
# Run with: make test-install-hooks

set -u

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
script="$here/install_hooks.sh"

work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-install-hooks-test.XXXXXX") || exit 1
trap 'chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"' EXIT
cd "$work" || exit 1
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

failures=0
checks=0
fail() {
	failures=$((failures + 1))
	echo "FAIL: $1"
	[ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/      /'
}
pass() { echo "ok   $1"; }

# fixture <pre-push body> — a repository with .githooks/pre-push. Prints its path.
# `mktemp`, not a counter: this runs in a command substitution, so a counter
# would never advance and every fixture would be the same repository.
fixture() {
	repo=$(mktemp -d "$work/repo.XXXXXX") || exit 1
	mkdir -p "$repo/.githooks" || exit 1
	git -C "$repo" init -q . || exit 1
	printf '%s\n' "$1" > "$repo/.githooks/pre-push"
	echo "$repo"
}

run() { (cd "$1" && sh "$script" 2>&1); }

# Unset: the hooks path is written.
checks=$((checks + 1))
r=$(fixture '#!/bin/sh') || exit 1
out=$(run "$r")
if [ "$(git -C "$r" config --get core.hooksPath)" = .githooks ] && [ -x "$r/.githooks/pre-push" ]; then
	pass 'an unset hooks path is set to .githooks'
else
	fail 'an unset hooks path is set to .githooks' "$out"
fi

# Already .githooks, config read-only: nothing is written, and it succeeds.
checks=$((checks + 1))
r=$(fixture '#!/bin/sh') || exit 1
git -C "$r" config core.hooksPath .githooks
chmod a-w "$r/.git" "$r/.git/config"
if out=$(run "$r"); then
	pass 'an installed .githooks leaves a read-only .git/config alone'
else
	fail 'an installed .githooks leaves a read-only .git/config alone' "$out"
fi
chmod u+w "$r/.git" "$r/.git/config"

# An absolute path to an identical pre-push (the main checkout's, from a
# worktree): nothing is written, and it succeeds.
checks=$((checks + 1))
main=$(fixture '#!/bin/sh
make check') || exit 1
r=$(fixture '#!/bin/sh
make check') || exit 1
chmod +x "$main/.githooks/pre-push"
git -C "$r" config core.hooksPath "$main/.githooks"
chmod a-w "$r/.git" "$r/.git/config"
if out=$(run "$r") && [ "$(git -C "$r" config --get core.hooksPath)" = "$main/.githooks" ]; then
	pass 'an identical pre-push elsewhere leaves a read-only .git/config alone'
else
	fail 'an identical pre-push elsewhere leaves a read-only .git/config alone' "$out"
fi
chmod u+w "$r/.git" "$r/.git/config"

# An identical pre-push without the executable bit is replaced: git skips a
# hook it cannot execute, printing only a hint, so pushes would bypass it.
checks=$((checks + 1))
main=$(fixture '#!/bin/sh
make check') || exit 1
r=$(fixture '#!/bin/sh
make check') || exit 1
chmod a-x "$main/.githooks/pre-push"
git -C "$r" config core.hooksPath "$main/.githooks"
out=$(run "$r")
if [ "$(git -C "$r" config --get core.hooksPath)" = .githooks ]; then
	pass 'an identical but non-executable pre-push is replaced'
else
	fail 'an identical but non-executable pre-push is replaced' "$out"
fi

# A path to a different pre-push is replaced.
checks=$((checks + 1))
other=$(fixture '#!/bin/sh
echo stale') || exit 1
r=$(fixture '#!/bin/sh
make check') || exit 1
git -C "$r" config core.hooksPath "$other/.githooks"
out=$(run "$r")
if [ "$(git -C "$r" config --get core.hooksPath)" = .githooks ]; then
	pass 'a hooks path with a different pre-push is replaced'
else
	fail 'a hooks path with a different pre-push is replaced' "$out"
fi

# A write that is needed but refused fails loudly.
checks=$((checks + 1))
r=$(fixture '#!/bin/sh') || exit 1
chmod a-w "$r/.git" "$r/.git/config"
if out=$(run "$r"); then
	fail 'a refused write fails' "$out"
else
	pass 'a refused write fails'
fi
chmod u+w "$r/.git" "$r/.git/config"

if [ "$failures" -ne 0 ]; then
	echo "install-hooks tests: $failures of $checks failed"
	exit 1
fi
echo "install-hooks tests: all $checks passed"
