#!/bin/sh
# Tests that every script making a temporary directory makes it under $TMPDIR
# with an explicit template, and stops — without touching the repository it
# was run from — when it cannot.
#
# Why both halves matter: macOS's /usr/bin/mktemp ignores $TMPDIR for a bare
# `mktemp -d` (and `mktemp -d -t prefix`) and uses /var/folders/.../T/, which
# the Claude Code sandbox cannot write. A test script that then carried on with
# an empty `$work` ran `cd ""` — a no-op — and committed its fixtures, and
# switched branches, in the caller's own worktree (issue #315).
#
# Each script runs from a throwaway copy of the scripts, committed into a
# throwaway git repository, with a `mktemp` on PATH that fails on its N-th
# call and runs the real one before that. The script must exit non-zero, and
# that repository must be exactly as it was: same branch, same commit, same
# branch list, no new or changed files.
#
# N is 1 for every script, and also 2 for a test script with more than one
# mktemp call site: the second call is the first one a fixture helper makes
# (`new_repo`, `fixture`, ...) after the work directory exists, and an
# unchecked helper call once committed into the caller's repository even
# though the first call was guarded (PR #326). Every later call is not run
# dynamically — that would replay each suite once per call — so the static
# half below requires every `x=$(mktemp ...)` assignment to handle failure.
#
# Run with: make test-tmpdir-guard

set -u

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-tmpdir-guard.XXXXXX") || exit 1
trap 'rm -rf "$work"' EXIT

failures=0
checks=0

fail() {
	echo "FAIL $1" >&2
	failures=$((failures + 1))
}
pass() { echo "ok   $1"; }

# The repository's hook-time environment would point git at this checkout.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES

real_mktemp=$(command -v mktemp) || exit 1
mkdir -p "$work/bin" "$work/tmpdir"
cat > "$work/bin/mktemp" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$MKTEMP_LOG"
if [ "$(wc -l < "$MKTEMP_LOG")" -lt "$MKTEMP_FAIL_AT" ]; then
	exec "$REAL_MKTEMP" "$@"
fi
echo "mktemp: simulated failure" >&2
exit 1
SH
chmod +x "$work/bin/mktemp"

# repo_state prints everything a stray git command or file write would change.
repo_state() {
	git -C "$1" rev-parse HEAD
	git -C "$1" branch --show-current
	git -C "$1" branch --list --format='%(refname)'
	git -C "$1" status --porcelain --untracked-files=all
	git -C "$1" config --local --list
}

n=0
# check_script <path> <must-reach-mktemp> [fail-at]
#   fail-at (default 1) is the mktemp call that fails.
#   must-reach-mktemp is "yes" for the test scripts, which create their work
#   directory before anything else. The production scripts may stop earlier
#   for want of a tool (gremlins, a JDK); that is still a pass for the
#   "stops and touches nothing" half, and the static check below covers the
#   template.
check_script() {
	script=$1
	must_reach=$2
	fail_at=${3:-1}
	n=$((n + 1))
	checks=$((checks + 1))
	copy="$work/case$n"
	mkdir -p "$copy"
	(cd "$root" && tar -cf - scripts frontend/e2e/*.sh frontend/screenshots/*.sh .claude/hooks) | tar -xf - -C "$copy"
	(
		cd "$copy" || exit 1
		git init -q .
		git config user.email "test@example.invalid"
		git config user.name "tmpdir guard test"
		git add -A
		git commit -q -m base
	) || {
		fail "$script: could not build the throwaway repository"
		return
	}
	before=$(repo_state "$copy")
	log="$work/mktemp$n.log"
	: > "$log"
	(
		cd "$copy" || exit 1
		PATH="$work/bin:$PATH" MKTEMP_LOG="$log" MKTEMP_FAIL_AT="$fail_at" \
			REAL_MKTEMP="$real_mktemp" TMPDIR="$work/tmpdir" \
			sh "$script" < /dev/null > "$work/out$n" 2>&1
	)
	status=$?
	after=$(repo_state "$copy")

	label="$script (mktemp call $fail_at fails)"
	if [ "$status" -eq 0 ]; then
		fail "$label: exited 0 although mktemp failed"
		return
	fi
	if [ "$before" != "$after" ]; then
		fail "$label: changed the repository it ran from:
$(printf '%s\n' "$before" > "$work/before$n"; printf '%s\n' "$after" > "$work/after$n"; diff "$work/before$n" "$work/after$n")"
		return
	fi
	if [ "$must_reach" = yes ] && [ "$(wc -l < "$log")" -lt "$fail_at" ]; then
		fail "$label: never reached that mktemp call, so this case proves nothing"
		return
	fi
	if [ -s "$log" ] && ! head -n 1 "$log" | grep -q -- "$work/tmpdir/"; then
		fail "$label: first mktemp call was not under \$TMPDIR: $(head -n 1 "$log")"
		return
	fi
	# A later failure only has to fail the run: a case that runs inside a
	# command substitution (hooks_test.sh's expect_status) cannot stop the
	# script, and the repository check above is what matters there.
	if [ "$fail_at" -eq 1 ] && [ "$(wc -l < "$log")" -gt 1 ]; then
		fail "$label: carried on after mktemp failed (called it $(wc -l < "$log" | tr -d ' ') times)"
		return
	fi
	pass "$label: stops and leaves the repository alone"
}

for t in scripts/coverage_blocks_test.sh scripts/docs_links_check_test.sh \
	scripts/efficacy_test.sh scripts/model_check_test.sh scripts/mutation_test.sh \
	scripts/scenarios_check_test.sh scripts/screenshots_check_test.sh \
	scripts/install_hooks_test.sh scripts/golangci_lint_cache_test.sh scripts/require_pty_test.sh \
	scripts/go_toolchain_test.sh scripts/node_toolchain_test.sh \
	frontend/screenshots/screenshots-env_test.sh .claude/hooks/hooks_test.sh; do
	check_script "$t" yes
	# More than one call site: fail the second call too (see the header).
	if [ "$(grep -vE '^[[:space:]]*#' "$root/$t" | grep -cE '(\$\(|^[[:space:]]*)mktemp[[:space:]]')" -gt 1 ]; then
		check_script "$t" yes 2
	fi
done
for s in scripts/coverage_blocks.sh scripts/docs_links_check.sh scripts/efficacy.sh \
	scripts/model_check.sh scripts/mutation.sh; do
	check_script "$s" no
done

# Static half: every mktemp invocation in the repository's shell scripts names
# a template (XXXXXX), and none hardcodes /tmp — a bare `mktemp`/`mktemp -d`
# ignores $TMPDIR on macOS, and /tmp is outside the sandbox's writable set.
checks=$((checks + 1))
bad=$(cd "$root" && grep -nE '(\$\(|^|[;&|]|[[:space:]])mktemp([[:space:]]+-|[[:space:]]*\)|[[:space:]]*$)' \
	scripts/*.sh frontend/e2e/*.sh frontend/screenshots/*.sh .claude/hooks/*.sh |
	grep -vE '^[^:]+:[0-9]+:[[:space:]]*#' |
	grep -vE "^scripts/tmpdir_guard_test\.sh:" |
	grep -vE 'XXXXXX' ; cd "$root" && grep -nE 'mktemp[^#]*[" ]/tmp/' \
	scripts/*.sh frontend/e2e/*.sh frontend/screenshots/*.sh .claude/hooks/*.sh |
	grep -vE '^[^:]+:[0-9]+:[[:space:]]*#')
if [ -n "$bad" ]; then
	fail "mktemp calls without a \$TMPDIR template:
$bad"
else
	pass "every mktemp call names a template under \$TMPDIR"
fi

# And every `x=$(mktemp ...)` assignment handles a failure on the same line
# (`|| exit 1`, `|| return`, or inside an `if`): an empty path makes `cd ""` a
# no-op and the next git command run in the caller's repository.
checks=$((checks + 1))
unchecked=$(cd "$root" && grep -nE '=\$\(mktemp[[:space:]]' \
	scripts/*.sh frontend/e2e/*.sh frontend/screenshots/*.sh .claude/hooks/*.sh |
	grep -vE '^[^:]+:[0-9]+:[[:space:]]*#' |
	grep -vE "^scripts/tmpdir_guard_test\.sh:" |
	grep -vE '\|\||^[^:]+:[0-9]+:[[:space:]]*(if|elif)[[:space:]]')
if [ -n "$unchecked" ]; then
	fail "mktemp assignments that ignore a failure:
$unchecked"
else
	pass "every mktemp assignment handles a failure"
fi

echo
echo "$checks checks, $failures failures"
[ "$failures" -eq 0 ]
