#!/bin/sh
#
# Tests for scripts/coverage_blocks.sh.
#
# Two properties carry this gate, and both are easy to get subtly wrong:
#
#   1. Summing per unique block. A profile produced by one `go test` command
#      over several packages with a shared `-coverpkg` list contains the SAME
#      block many times, once per package whose test binary was linked against
#      it, with a different count each time. Reading the lines one at a time
#      reports blocks as unexecuted that other packages' tests do execute — the
#      exact false positive that would make this gate untrusted on day one.
#   2. Scoping to the diff. The gate only speaks about lines this branch
#      touched, so it starts green on a repository that has pre-existing gaps
#      (this one has 278 of them). A rule that fired on all of those would be
#      switched off rather than satisfied.
#
# Run with: make test-coverage-blocks

set -u

# The two variables DEVELOPMENT.md tells developers to set are exactly the two
# that would silently rewrite what this suite is testing — a base ref makes
# every report case run the gate instead, and the branch-wide exemption makes
# every gate case pass. `make check` runs this, so an exported variable in a
# developer's shell must not turn into ten unrelated-looking failures.
# scripts/efficacy_test.sh solves the same problem by passing `EFFICACY_BASE=`
# on every invocation; unsetting once is the same rule, stated once.
unset COVERAGE_BLOCKS_BASE COVERAGE_BLOCKS_EXEMPT

scripts_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
checker="$scripts_dir/coverage_blocks.sh"

failures=0
checks=0

fail() {
	failures=$((failures + 1))
	echo "FAIL: $1"
	[ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/      /'
	return 0
}
pass() { echo "ok   $1"; }

# rewrite <file> <old> <new> — replace a fixed string, without `sed -i`.
# `sed -i` with no suffix is a GNU extension; BSD/macOS sed reads the next
# argument as the backup suffix and then fails. macOS is a supported developer
# platform (.goreleaser.yaml builds darwin) and this suite runs inside
# `make check`, so it has to work there too. Lifted from
# scripts/efficacy_test.sh, which carries the same helper for the same reason.
rewrite() {
	rw_file=$1
	rw_old=$2
	rw_new=$3
	OLD=$rw_old NEW=$rw_new awk '
		BEGIN { old = ENVIRON["OLD"]; new = ENVIRON["NEW"] }
		{
			p = index($0, old)
			if (p > 0) $0 = substr($0, 1, p - 1) new substr($0, p + length(old))
			print
		}
	' "$rw_file" > "$rw_file.tmp" && mv "$rw_file.tmp" "$rw_file"
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# new_repo — an empty git repository with a go.mod, and one commit on `main`
# so there is always a base ref to diff against. Prints its path.
#
# `mktemp -d`, not a counter: this runs inside a command substitution, so a
# counter variable would be incremented in a subshell and never seen by the
# caller — every repository would be the same directory, and the second test
# onwards would run against the first one's history.
new_repo() {
	repo=$(mktemp -d "$work/repoXXXXXX")
	mkdir -p "$repo/pkg"
	(
		cd "$repo" || exit 1
		git init -q -b main .
		git config user.email dev@example.com
		git config user.name dev
		git config commit.gpgsign false
		printf 'module example\n\ngo 1.25\n' > go.mod
		git add go.mod
		git commit -qm base
		# The gate diffs HEAD against a base ref. Committing the test's
		# changes onto `main` itself would make the merge base equal HEAD,
		# and every gate case would pass for the wrong reason — "nothing
		# changed" rather than "nothing uncovered".
		git checkout -qb feature
	) || return 1
	printf '%s\n' "$repo"
}

# commit_on_main <repo> <message> — commit the working tree's pkg/ files onto
# `main` and re-branch from there, so they are part of the base rather than of
# the branch under test. Without this, a file the test means to present as
# pre-existing is added BY the branch, every one of its lines counts as changed,
# and a case meant to prove the gate ignores untouched code proves nothing.
commit_on_main() {
	(
		cd "$1" || exit 1
		git checkout -q main
		git add -A pkg
		git commit -qm "$2"
		git branch -f feature main
		git checkout -q feature
	) > /dev/null 2>&1
}

# run_checker <repo> <args...> — run the checker inside <repo>, capture output
# and exit status into $out / $status.
run_checker() {
	rc=$1
	shift
	out=$(cd "$rc" && sh "$checker" "$@" 2>&1)
	status=$?
	return 0
}

# expect_status_in <subdir> <want> <name> <repo> <args...> — like expect_status,
# but starting from a subdirectory of the fixture repository. Nothing in the
# script's own usage says it must run from the repository root, so a pathspec
# that silently resolves against the caller's cwd is a fail-open the root-only
# cases cannot see.
expect_status_in() {
	sub=$1
	want=$2
	name=$3
	rc=$4
	shift 4
	checks=$((checks + 1))
	out=$(cd "$rc/$sub" && sh "$checker" "$@" 2>&1)
	status=$?
	if [ "$status" -eq "$want" ]; then
		pass "$name"
	else
		fail "$name: wanted exit $want, got $status" "$out"
	fi
}

# expect_status <want> <name> <repo> <args...>
expect_status() {
	want=$1
	name=$2
	rc=$3
	shift 3
	checks=$((checks + 1))
	run_checker "$rc" "$@"
	if [ "$status" -eq "$want" ]; then
		pass "$name"
	else
		fail "$name: wanted exit $want, got $status" "$out"
	fi
}

# expect_output <substring> <name>  — asserts against the last run's output.
expect_output() {
	checks=$((checks + 1))
	case "$out" in
	*"$1"*) pass "$2" ;;
	*) fail "$2: output did not contain '$1'" "$out" ;;
	esac
}

# expect_no_output <substring> <name>
expect_no_output() {
	checks=$((checks + 1))
	case "$out" in
	*"$1"*) fail "$2: output unexpectedly contained '$1'" "$out" ;;
	*) pass "$2" ;;
	esac
}

# write_pkg <repo> — a source file whose line numbers match the fixtures below.
write_pkg() {
	cat > "$1/pkg/a.go" << 'EOF'
package pkg

import "errors"

func Do(fail bool) error {
	if fail {
		return errors.New("boom")
	}
	return nil
}

func Other(fail bool) error {
	if fail {
		return errors.New("boom")
	}
	return nil
}
EOF
}

# ── Report mode ───────────────────────────────────────────────────────────────

repo=$(new_repo)
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:10.20,12.3 2 1
example/pkg/a.go:14.16,16.3 1 1
EOF
expect_status 0 "a profile with every block executed passes" "$repo"
expect_output "0 of 2 blocks never executed" "a fully covered profile reports zero unexecuted blocks"

repo=$(new_repo)
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:10.20,12.3 2 1
example/pkg/a.go:14.16,16.3 1 0
EOF
expect_status 0 "report mode never fails, even with an unexecuted block" "$repo"
expect_output "pkg/a.go:14" "report mode names the unexecuted block"

# The correctness core: the same block from several test binaries, summing to
# a nonzero count. Reading each raw line on its own would flag this.
repo=$(new_repo)
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:10.20,12.3 2 0
example/pkg/a.go:10.20,12.3 2 1
example/pkg/a.go:10.20,12.3 2 0
EOF
expect_status 0 "duplicate entries summing to nonzero pass" "$repo"
expect_output "0 of 1 blocks never executed" "a block executed by one package's tests is not flagged"

# ... and the same block summing to zero is one finding, not three.
repo=$(new_repo)
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:10.20,12.3 2 0
example/pkg/a.go:10.20,12.3 2 0
example/pkg/a.go:10.20,12.3 2 0
EOF
expect_status 0 "duplicate entries summing to zero pass in report mode" "$repo"
expect_output "1 of 1 blocks never executed" "a duplicated unexecuted block is counted once"

# `-covermode=count` profiles carry real counts, not just 0/1.
repo=$(new_repo)
cat > "$repo/coverage.out" << 'EOF'
mode: count
example/pkg/a.go:10.20,12.3 2 47
example/pkg/a.go:14.16,16.3 1 0
EOF
expect_status 0 "a count-mode profile is read the same way" "$repo"
expect_output "1 of 2 blocks never executed" "count mode: only the zero-count block is reported"

repo=$(new_repo)
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:10.20,12.3 2 1
example/pkg/a.go:14.16,16.3 1 0
EOF
expect_status 0 "--summary succeeds" "$repo" --summary
expect_output "coverage-blocks:" "--summary prints a one-line summary"
expect_no_output "pkg/a.go:14" "--summary does not list individual blocks"

# A base ref in the environment must not turn --summary into the gate. The
# shipped CI job sets COVERAGE_BLOCKS_BASE at step level and runs
# `make coverage-blocks`, whose prerequisite `coverage-go` ends with
# `--summary`: with the base winning, that line runs the gate a second time,
# and on a branch with a finding it aborts coverage-go, so the failure is
# reported against the coverage-percentage target instead of this one.
repo=$(new_repo)
write_pkg "$repo"
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:6.11,8.3 1 0
EOF
(cd "$repo" && git add pkg/a.go && git commit -qm change)
checks=$((checks + 1))
out=$(cd "$repo" && COVERAGE_BLOCKS_BASE=main sh "$checker" --summary 2>&1)
status=$?
if [ "$status" -eq 0 ]; then
	pass "--summary stays a summary when a base ref is in the environment"
else
	fail "--summary stays a summary when a base ref is in the environment: got exit $status" "$out"
fi
expect_output "1 of 1 blocks never executed" "--summary with a base ref prints the summary, not the gate"
expect_no_output "changed" "--summary with a base ref does not run the gate"

# ── Could not check ───────────────────────────────────────────────────────────
#
# Principle 4 / decision D4's "could not check is a failure, never a skip": a
# check that reports green having measured nothing is the failure mode being a
# required check exists to rule out.

repo=$(new_repo)
expect_status 1 "a missing profile fails rather than passing quietly" "$repo"
expect_output "coverage.out" "the missing-profile message names the file"

repo=$(new_repo)
printf 'mode: set\n' > "$repo/coverage.out"
expect_status 1 "a profile with no blocks at all fails" "$repo"

# ── Gate mode ─────────────────────────────────────────────────────────────────

# A block that never executed, on a line this branch changed.
repo=$(new_repo)
write_pkg "$repo"
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:5.28,6.11 1 1
example/pkg/a.go:6.11,8.3 1 0
EOF
(cd "$repo" && git add pkg/a.go && git commit -qm change)
expect_status 1 "an unexecuted block on a changed line fails the gate" "$repo" --base main
expect_output "pkg/a.go:6" "the gate names the file and line of the finding"

# ... and the same finding must survive being run from a subdirectory. `git diff
# --name-only` prints repository-relative paths wherever it runs, but a pathspec
# is resolved against the caller's cwd, so a mismatch there makes every file's
# touched-line set come back empty and the gate report "nothing" having measured
# nothing.
repo=$(new_repo)
write_pkg "$repo"
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:5.28,6.11 1 1
example/pkg/a.go:6.11,8.3 1 0
EOF
(cd "$repo" && git add pkg/a.go && git commit -qm change)
expect_status_in pkg 1 "the same finding is reported when run from a subdirectory" \
	"$repo" --base main --profile "$repo/coverage.out"
expect_output "pkg/a.go:6" "the subdirectory run names the same block"

# The same profile, with the change confined to a file the block is not in.
repo=$(new_repo)
write_pkg "$repo"
commit_on_main "$repo" add-source
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:5.28,6.11 1 1
example/pkg/a.go:6.11,8.3 1 0
EOF
(cd "$repo" && printf 'package pkg\n' > pkg/b.go && git add pkg/b.go && git commit -qm other)
expect_status 0 "a pre-existing gap in an untouched file does not fail the gate" "$repo" --base main
expect_output "nothing to report" "the gate says it had nothing to report"

# A changed line inside a block that DID execute.
repo=$(new_repo)
write_pkg "$repo"
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:5.28,6.11 1 1
example/pkg/a.go:6.11,8.3 1 1
EOF
(cd "$repo" && git add pkg/a.go && git commit -qm change)
expect_status 0 "a covered block on a changed line passes" "$repo" --base main

# The changed line is in the MIDDLE of an unexecuted block, not at its start.
repo=$(new_repo)
write_pkg "$repo"
commit_on_main "$repo" add-source
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:5.28,9.12 4 0
EOF
rewrite "$repo/pkg/a.go" 'errors.New("boom")' 'errors.New("bang")'
(cd "$repo" && git add pkg/a.go && git commit -qm tweak)
expect_status 1 "a changed line inside an unexecuted block fails the gate" "$repo" --base main

# Test files are not implementation: changing one is not a reason to demand
# coverage of a block elsewhere, and test files never appear in a profile.
repo=$(new_repo)
write_pkg "$repo"
commit_on_main "$repo" add-source
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:6.11,8.3 1 0
EOF
(cd "$repo" && printf 'package pkg\n' > pkg/a_test.go && git add pkg/a_test.go && git commit -qm test-only)
expect_status 0 "a branch that changes only test files has nothing to check" "$repo" --base main
expect_output "nothing to check" "a test-only branch is told apart from a clean gate run"

# COVERAGE_PKGS deliberately excludes packages (internal/session's real PTY /
# SSH / tmux transports among them), so a changed file can be absent from the
# profile entirely. Reporting that as "every block was executed" is a claim the
# tool has no basis for — the gate measured nothing about that file.
repo=$(new_repo)
mkdir -p "$repo/unmeasured"
cat > "$repo/unmeasured/a.go" << 'EOF'
package unmeasured

import "errors"

func Do(fail bool) error {
	if fail {
		return errors.New("boom")
	}
	return nil
}
EOF
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:10.20,12.3 2 1
EOF
(cd "$repo" && git add unmeasured/a.go && git commit -qm change)
expect_status 0 "a changed file outside the profile does not fail the gate" "$repo" --base main
expect_output "unmeasured/a.go" "the gate names the changed file it could not measure"
expect_no_output "was executed" "the gate does not claim an unmeasured file was covered"

# ── The escape hatch ──────────────────────────────────────────────────────────

# marked_pkg <repo> <marker-line-6> <marker-line-7> — same shape as write_pkg,
# with the two lines around the `if` under the caller's control.
marked_pkg() {
	{
		echo 'package pkg'
		echo
		echo 'import "errors"'
		echo
		echo 'func Do(fail bool) error {'
		printf '%s\n' "$2"
		printf '%s\n' "$3"
		echo '	}'
		echo '	return nil'
		echo '}'
	} > "$1/pkg/a.go"
}

# Marker on the block's own opening line.
repo=$(new_repo)
marked_pkg "$repo" '	if fail { //coverage:exempt errors.New cannot fail' '		return errors.New("boom")'
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:6.11,8.3 1 0
EOF
(cd "$repo" && git add pkg/a.go && git commit -qm change)
expect_status 0 "a marker on the block's opening line exempts it" "$repo" --base main
expect_output "1 exempt" "the gate reports how many blocks were exempted"
expect_output "Exempt by //coverage:exempt:" "the gate lists the exemptions it applied"
expect_output "pkg/a.go:6-8" "the exemption list names the block"

# Marker on the line directly above.
repo=$(new_repo)
marked_pkg "$repo" '	//coverage:exempt errors.New cannot fail' '	if fail {'
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:7.11,9.3 1 0
EOF
(cd "$repo" && git add pkg/a.go && git commit -qm change)
expect_status 0 "a marker on the line above exempts the block" "$repo" --base main

# A marker on one block's opening line must not waive the block that opens on
# the NEXT line — which is what a nested `if`, an `else` on the following line,
# or a second statement inside a one-line body all look like. The inner block
# there was never exempted by anyone and no reason was ever written for it.
repo=$(new_repo)
cat > "$repo/pkg/a.go" << 'EOF'
package pkg

func Do(a, b bool) int {
	if a { //coverage:exempt the outer condition cannot happen
		if b {
			return 1
		}
	}
	return 0
}
EOF
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:4.9,5.8 1 0
example/pkg/a.go:5.9,7.4 1 0
EOF
(cd "$repo" && git add pkg/a.go && git commit -qm change)
expect_status 1 "a marker on the enclosing block's opening line does not exempt a nested block" "$repo" --base main
expect_output "1 block(s) this branch changed never executed" "the nested block is reported, not exempted"

# A marker with no reason is not a marker. The repository's rule for the
# coverage exclusion list in the Makefile is the same one: an exclusion carries
# a reason or it does not exist.
repo=$(new_repo)
marked_pkg "$repo" '	if fail { //coverage:exempt' '		return errors.New("boom")'
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:6.11,8.3 1 0
EOF
(cd "$repo" && git add pkg/a.go && git commit -qm change)
expect_status 1 "a marker with no reason does not exempt anything" "$repo" --base main
expect_output "reason" "the gate says the marker needs a reason"

# The branch-wide hatch, matching efficacy's EFFICACY_EXEMPT / label pair.
repo=$(new_repo)
write_pkg "$repo"
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:6.11,8.3 1 0
EOF
(cd "$repo" && git add pkg/a.go && git commit -qm change)
checks=$((checks + 1))
out=$(cd "$repo" && COVERAGE_BLOCKS_EXEMPT=1 sh "$checker" --base main 2>&1)
status=$?
if [ "$status" -eq 0 ]; then
	pass "COVERAGE_BLOCKS_EXEMPT=1 exempts the whole branch"
else
	fail "COVERAGE_BLOCKS_EXEMPT=1 exempts the whole branch: got exit $status" "$out"
fi
expect_output "exempt" "the branch-wide exemption says so in the output"

# ── Gate mode: could not check ────────────────────────────────────────────────

repo=$(new_repo)
write_pkg "$repo"
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:6.11,8.3 1 0
EOF
(cd "$repo" && git add pkg/a.go && git commit -qm change)
expect_status 1 "a base ref that does not exist fails rather than skipping" "$repo" --base origin/nope
expect_output "does not exist" "the missing-base message says what is wrong"

# A ref that exists but shares no history with HEAD. `rev-parse --verify` passes
# there, so the check above does not cover it — and the realistic trigger is not
# an orphan branch but a clone whose history does not reach the merge base,
# which is the same lost `fetch-depth: 0` that check exists for. Failing open
# here would report green having measured nothing.
repo=$(new_repo)
write_pkg "$repo"
cat > "$repo/coverage.out" << 'EOF'
mode: set
example/pkg/a.go:6.11,8.3 1 0
EOF
(
	cd "$repo" || exit 1
	git add pkg/a.go && git commit -qm change
	git checkout -q --orphan unrelated
	git rm -q -rf . > /dev/null 2>&1
	printf 'unrelated\n' > README
	git add README && git commit -qm unrelated
	git checkout -q feature
) > /dev/null 2>&1
expect_status 1 "a base ref with no shared history fails rather than passing quietly" "$repo" --base unrelated
expect_output "merge base" "the no-merge-base message says what is wrong"

# ── Summary ───────────────────────────────────────────────────────────────────

echo
if [ "$failures" -eq 0 ]; then
	echo "coverage-blocks tests: $checks checks, all passed"
	exit 0
fi
echo "coverage-blocks tests: $failures of $checks checks failed"
exit 1
