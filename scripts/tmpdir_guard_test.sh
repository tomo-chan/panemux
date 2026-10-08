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
# throwaway git repository, with a `mktemp` on PATH that always fails. The
# script must exit non-zero, and that repository must be exactly as it was:
# same branch, same commit, same branch list, no new or changed files.
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

mkdir -p "$work/bin" "$work/tmpdir"
cat > "$work/bin/mktemp" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >> "$MKTEMP_LOG"
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
# check_script <path> <must-reach-mktemp>
#   must-reach-mktemp is "yes" for the test scripts, which create their work
#   directory before anything else. The production scripts may stop earlier
#   for want of a tool (gremlins, a JDK); that is still a pass for the
#   "stops and touches nothing" half, and the static check below covers the
#   template.
check_script() {
	script=$1
	must_reach=$2
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
		PATH="$work/bin:$PATH" MKTEMP_LOG="$log" TMPDIR="$work/tmpdir" \
			sh "$script" < /dev/null > "$work/out$n" 2>&1
	)
	status=$?
	after=$(repo_state "$copy")

	if [ "$status" -eq 0 ]; then
		fail "$script: exited 0 although mktemp failed"
		return
	fi
	if [ "$before" != "$after" ]; then
		fail "$script: changed the repository it ran from:
$(printf '%s\n' "$before" > "$work/before$n"; printf '%s\n' "$after" > "$work/after$n"; diff "$work/before$n" "$work/after$n")"
		return
	fi
	if [ "$must_reach" = yes ] && [ ! -s "$log" ]; then
		fail "$script: never called mktemp, so this case proves nothing"
		return
	fi
	if [ -s "$log" ] && ! head -n 1 "$log" | grep -q -- "$work/tmpdir/"; then
		fail "$script: first mktemp call was not under \$TMPDIR: $(head -n 1 "$log")"
		return
	fi
	if [ "$(wc -l < "$log")" -gt 1 ]; then
		fail "$script: carried on after mktemp failed (called it $(wc -l < "$log" | tr -d ' ') times)"
		return
	fi
	pass "$script stops at the first failed mktemp and leaves the repository alone"
}

for t in scripts/coverage_blocks_test.sh scripts/docs_links_check_test.sh \
	scripts/efficacy_test.sh scripts/model_check_test.sh scripts/mutation_test.sh \
	scripts/scenarios_check_test.sh scripts/screenshots_check_test.sh \
	frontend/screenshots/screenshots-env_test.sh .claude/hooks/hooks_test.sh; do
	check_script "$t" yes
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

echo
echo "$checks checks, $failures failures"
[ "$failures" -eq 0 ]
