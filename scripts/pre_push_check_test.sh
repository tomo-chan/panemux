#!/bin/sh
# Tests for scripts/pre_push_check.sh, which .githooks/pre-push runs.
#
# What it protects: the pre-push hook runs only the checks the pushed change
# touches — the difference between each local branch and the same branch on
# origin, or, for a branch origin does not have yet, between it and origin's
# main (issue #335). Two failures matter, and they pull in opposite
# directions:
#
#   - checking too little: a changed package, script or document whose check is
#     never selected, which reports the push as verified while verifying
#     nothing. Every case where the range cannot be worked out, or the change
#     cannot be narrowed (go.mod, the Makefile, the runtime pins), falls back
#     to `make check` rather than to nothing.
#   - checking too much: a change already on origin, or one that only exists on
#     main, selected again — the whole reason the hook stopped running
#     `make check`.
#
# Most cases read the plan (`--plan`), which names each check without running
# it. The last ones run the checks for real against a throwaway Go module.
#
# Run with: make test-pre-push

set -u

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
script="$here/pre_push_check.sh"

work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-pre-push-test.XXXXXX") || exit 1
trap 'rm -rf "$work"' EXIT
cd "$work" || exit 1

# The hook-time environment would point git at the caller's repository.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES

failures=0
checks=0
fail() {
	failures=$((failures + 1))
	echo "FAIL: $1"
	[ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/      /'
}
pass() { echo "ok   $1"; }

zero=0000000000000000000000000000000000000000

# fixture — a clone of a fresh bare origin whose main holds two Go packages
# (a, b), a script with its test, a document and a frontend module, all
# pushed. Prints the clone's path.
fixture() {
	base=$(mktemp -d "$work/case.XXXXXX") || exit 1
	git init -q --bare "$base/origin.git" || exit 1
	git init -q -b main "$base/repo" || exit 1
	(
		cd "$base/repo" || exit 1
		git config user.email "test@example.invalid"
		git config user.name "pre-push test"
		printf 'module sample\n\ngo 1.25\n' > go.mod
		mkdir -p a b a/testdata scripts docs frontend/src testdata/api-contract
		printf 'package a\n\nfunc A() int {\n\treturn 1\n}\n' > a/a.go
		printf 'package b\n\nfunc B() int {\n\treturn 1\n}\n' > b/b.go
		printf 'input\n' > a/testdata/in.txt
		printf 'package main\n\nfunc main() {}\n' > main.go
		printf '#!/bin/sh\n' > scripts/tool.sh
		printf '#!/bin/sh\n' > scripts/tool_test.sh
		printf '#!/bin/sh\n' > scripts/lonely.sh
		printf '# Doc\n' > docs/guide.md
		printf '# Scenarios\n' > docs/scenarios.md
		printf 'export const x = 1\n' > frontend/src/x.ts
		printf '{}\n' > testdata/api-contract/health.json
		# The tests the plan selects by name, present as they are here.
		mkdir -p .claude/hooks frontend/screenshots frontend/src/schemas
		for t in scripts/tmpdir_guard_test.sh scripts/screenshots_check_test.sh \
			.claude/hooks/hooks_test.sh frontend/screenshots/screenshots-env_test.sh; do
			printf '#!/bin/sh\n' > "$t"
		done
		printf 'export {}\n' > frontend/src/schemas/contract.test.ts
		git add -A
		git commit -q -m base
		git remote add origin "$base/origin.git"
		git push -q origin main
	) > /dev/null 2>&1 || exit 1
	echo "$base/repo"
}

# commit <repo> <message> <file> <content> — writes one file and commits it.
commit() {
	mkdir -p "$1/$(dirname "$3")"
	printf '%s\n' "$4" > "$1/$3"
	git -C "$1" add -A && git -C "$1" commit -q -m "$2"
}

sha() { git -C "$1" rev-parse "$2"; }

# plan <repo> <stdin> — the plan for one push, as pre-push receives it.
plan() {
	(cd "$1" && printf '%s\n' "$2" | sh "$script" --plan origin 2>&1)
}

# expect <name> <plan output> <line>... — every line must be in the plan.
# A line prefixed with ! must not be.
expect() {
	name=$1
	out=$2
	shift 2
	checks=$((checks + 1))
	for want in "$@"; do
		case "$want" in
		!*)
			if printf '%s\n' "$out" | grep -qxF -- "${want#!}"; then
				fail "$name: plan has \"${want#!}\"" "$out"
				return
			fi
			;;
		*)
			if ! printf '%s\n' "$out" | grep -qxF -- "$want"; then
				fail "$name: plan lacks \"$want\"" "$out"
				return
			fi
			;;
		esac
	done
	pass "$name"
}

# --- the range ----------------------------------------------------------------

# A branch origin already has: only what origin does not have yet is checked.
r=$(fixture) || exit 1
git -C "$r" switch -q -c feature
commit "$r" one a/a.go 'package a

func A() int {
	return 2
}'
git -C "$r" push -q origin feature 2> /dev/null
pushed=$(sha "$r" HEAD)
commit "$r" two b/b.go 'package b

func B() int {
	return 2
}'
out=$(plan "$r" "refs/heads/feature $(sha "$r" HEAD) refs/heads/feature $pushed")
expect "an existing branch checks only what origin lacks" "$out" \
	"go-test ./b" "go-vet ./b" "golangci-lint ./b" "gofmt b/b.go" "!go-test ./a" "!golangci-lint ./a" "!full"

# A branch origin does not have: everything since it left origin's main, and
# nothing main gained after it branched: only the merge base counts.
r=$(fixture) || exit 1
git -C "$r" switch -q -c feature
commit "$r" one a/a.go 'package a

func A() int {
	return 2
}'
git -C "$r" switch -q main
commit "$r" main-moves b/b.go 'package b

func B() int {
	return 3
}'
git -C "$r" push -q origin main 2> /dev/null
git -C "$r" switch -q feature
out=$(plan "$r" "refs/heads/feature $(sha "$r" HEAD) refs/heads/feature $zero")
expect "a new branch checks what it adds to origin's main" "$out" \
	"go-test ./a" "!go-test ./b" "!full"

# Deleting a branch pushes no code.
r=$(fixture) || exit 1
out=$(plan "$r" "(delete) $zero refs/heads/old $(sha "$r" HEAD)")
expect "deleting a branch checks nothing" "$out" "nothing" "!full"

# Nothing new: pushing what origin already has.
out=$(plan "$r" "refs/heads/main $(sha "$r" HEAD) refs/heads/main $(sha "$r" HEAD)")
expect "a push with no difference checks nothing" "$out" "nothing"

# Several refs in one push: the union.
r=$(fixture) || exit 1
git -C "$r" switch -q -c one
commit "$r" one a/a.go 'package a

func A() int {
	return 2
}'
git -C "$r" switch -q main
git -C "$r" switch -q -c two
commit "$r" two b/b.go 'package b

func B() int {
	return 2
}'
out=$(plan "$r" "refs/heads/one $(sha "$r" one) refs/heads/one $zero
refs/heads/two $(sha "$r" two) refs/heads/two $zero")
expect "several refs in one push check the union" "$out" "go-test ./a" "go-test ./b"

# A force push over a commit this clone never fetched cannot be diffed.
r=$(fixture) || exit 1
out=$(plan "$r" "refs/heads/main $(sha "$r" HEAD) refs/heads/main 1234567890123456789012345678901234567890")
expect "an unknown remote commit falls back to make check" "$out" "full"

# A force push over a commit this clone does have: the difference between the
# two trees, which is what origin's branch will change by.
r=$(fixture) || exit 1
git -C "$r" switch -q -c feature
commit "$r" one a/a.go 'package a

func A() int {
	return 2
}'
old=$(sha "$r" HEAD)
git -C "$r" reset -q --hard main
commit "$r" two b/b.go 'package b

func B() int {
	return 2
}'
out=$(plan "$r" "refs/heads/feature $(sha "$r" HEAD) refs/heads/feature $old")
expect "a force push checks the difference between the two trees" "$out" \
	"go-test ./a" "go-test ./b" "!full"

# A new branch with no origin/main to measure from.
r=$(fixture) || exit 1
git -C "$r" update-ref -d refs/remotes/origin/main
git -C "$r" switch -q -c feature
commit "$r" one a/a.go 'package a'
out=$(plan "$r" "refs/heads/feature $(sha "$r" HEAD) refs/heads/feature $zero")
expect "a new branch without origin/main falls back to make check" "$out" "full"

# Malformed input is not a reason to check nothing.
r=$(fixture) || exit 1
out=$(plan "$r" "refs/heads/main")
expect "a malformed pre-push line falls back to make check" "$out" "full"

# --- what each kind of file selects -------------------------------------------

# planfor <path> <content> — the plan for a new branch adding one commit that
# writes <path>.
planfor() {
	r=$(fixture) || exit 1
	git -C "$r" switch -q -c feature
	commit "$r" change "$1" "$2"
	plan "$r" "refs/heads/feature $(sha "$r" HEAD) refs/heads/feature $zero"
}

expect "a package's testdata selects the package" \
	"$(planfor a/testdata/in.txt changed)" "go-test ./a" "!gofmt a/testdata/in.txt"

expect "the root package is tested, after building the embedded frontend" \
	"$(planfor main.go 'package main

func main() { _ = 1 }')" "go-test ./." "build-frontend"

expect "a path with a space is not split" \
	"$(planfor 'a/my file.go' 'package a')" "gofmt a/my file.go" "go-test ./a"

expect "a frontend module selects tsc and its related tests" \
	"$(planfor frontend/src/x.ts 'export const x = 2')" "tsc" "vitest src/x.ts" "!go-test ./."

expect "a contract fixture selects the frontend contract test" \
	"$(planfor testdata/api-contract/health.json '{"ok":true}')" "vitest src/schemas/contract.test.ts" "!go-test ./."

expect "a script selects its own test" \
	"$(planfor scripts/tool.sh 'echo changed')" "shell scripts/tool_test.sh" "shell scripts/tmpdir_guard_test.sh"

expect "a script's test selects itself" \
	"$(planfor scripts/tool_test.sh 'echo changed')" "shell scripts/tool_test.sh"

expect "a script with no test is still checked for its temporary files" \
	"$(planfor scripts/lonely.sh 'echo changed')" "shell scripts/tmpdir_guard_test.sh" "!full"

expect "a Claude Code hook selects the hook tests" \
	"$(planfor .claude/hooks/stop-check.sh 'echo changed')" "shell .claude/hooks/hooks_test.sh"

expect "the screenshot capture selects its staging tests" \
	"$(planfor frontend/screenshots/screenshots-env.sh 'echo changed')" \
	"shell scripts/screenshots_check_test.sh" "shell frontend/screenshots/screenshots-env_test.sh"

expect "a document selects the link check, not the scenario ledger" \
	"$(planfor docs/guide.md '# Changed')" "docs-links" "!scenarios"

expect "the scenario ledger selects its check" \
	"$(planfor docs/scenarios.md '# Changed')" "docs-links" "scenarios"

for f in go.mod go.sum Makefile .golangci.yml .node-version frontend/package.json \
	frontend/package-lock.json frontend/tsconfig.json frontend/vite.config.ts \
	scripts/runtime-env.sh scripts/go-toolchain.sh scripts/node-toolchain.sh .githooks/pre-push; do
	expect "$f cannot be narrowed and falls back to make check" "$(planfor "$f" changed)" "full"
done

# A deleted package has nothing left to test; testing it would fail the push
# with "directory not found" for a change CI is the right place to judge.
r=$(fixture) || exit 1
git -C "$r" switch -q -c feature
git -C "$r" rm -q -r b
git -C "$r" commit -q -m "drop b"
out=$(plan "$r" "refs/heads/feature $(sha "$r" HEAD) refs/heads/feature $zero")
expect "a deleted package is not tested" "$out" "!go-test ./b" "!full"

# --- running the plan ---------------------------------------------------------
#
# The plan above is only worth something if the hook acts on it.

if command -v go > /dev/null 2>&1; then
	run_push() {
		(cd "$1" && printf '%s\n' "$2" | sh "$script" origin > "$work/run.out" 2>&1)
	}

	r=$(fixture) || exit 1
	git -C "$r" switch -q -c feature
	commit "$r" test a/a_test.go 'package a

import "testing"

func TestA(t *testing.T) {
	if A() != 2 {
		t.Fatal("A() is not 2")
	}
}'
	checks=$((checks + 1))
	if run_push "$r" "refs/heads/feature $(sha "$r" HEAD) refs/heads/feature $zero"; then
		fail "a failing test in a changed package blocks the push" "$(cat "$work/run.out")"
	elif grep -q 'A() is not 2' "$work/run.out"; then
		pass "a failing test in a changed package blocks the push"
	else
		fail "a failing test in a changed package blocks the push: blocked for another reason" "$(cat "$work/run.out")"
	fi

	commit "$r" fix a/a.go 'package a

func A() int {
	return 2
}'
	checks=$((checks + 1))
	if run_push "$r" "refs/heads/feature $(sha "$r" HEAD) refs/heads/feature $zero"; then
		pass "a passing change pushes"
	else
		fail "a passing change pushes" "$(cat "$work/run.out")"
	fi

	commit "$r" unformatted b/b.go 'package b

func B() int {
    return 1
}'
	checks=$((checks + 1))
	if run_push "$r" "refs/heads/feature $(sha "$r" HEAD) refs/heads/feature $zero"; then
		fail "an unformatted Go file blocks the push" "$(cat "$work/run.out")"
	elif grep -q 'b/b.go' "$work/run.out"; then
		pass "an unformatted Go file blocks the push"
	else
		fail "an unformatted Go file blocks the push: blocked for another reason" "$(cat "$work/run.out")"
	fi

	# golangci-lint runs on the changed packages: an unchecked error is
	# something neither gofmt, go vet nor go test reports.
	r=$(fixture) || exit 1
	git -C "$r" switch -q -c feature
	commit "$r" unchecked a/e.go 'package a

import "os"

// E drops the error os.Remove returns.
func E() { os.Remove("x") }'
	checks=$((checks + 1))
	if run_push "$r" "refs/heads/feature $(sha "$r" HEAD) refs/heads/feature $zero"; then
		fail "a golangci-lint finding blocks the push" "$(cat "$work/run.out")"
	elif grep -q 'errcheck' "$work/run.out"; then
		pass "a golangci-lint finding blocks the push"
	else
		fail "a golangci-lint finding blocks the push: blocked for another reason" "$(cat "$work/run.out")"
	fi
else
	echo "skip go not installed — the checks that run the plan are skipped"
fi

# --- falling back to make check -----------------------------------------------
#
# The fallback must report make check's own status, and leave nothing behind:
# `exec make check` once replaced the shell before its EXIT trap ran, leaving a
# scratch directory under $TMPDIR on every such push.

# fallback_run <recipe> — pushes a Makefile change whose `check` target runs
# <recipe>, with a fresh $TMPDIR. Prints that $TMPDIR; the status is the hook's.
fallback_run() {
	r=$(fixture) || exit 1
	git -C "$r" switch -q -c feature
	printf 'check:\n\t@%s\n' "$1" > "$r/Makefile"
	git -C "$r" add -A && git -C "$r" commit -q -m makefile
	tmp=$(mktemp -d "$work/tmp.XXXXXX") || exit 1
	echo "$tmp"
	(cd "$r" && printf '%s\n' "refs/heads/feature $(sha "$r" HEAD) refs/heads/feature $zero" |
		TMPDIR="$tmp" sh "$script" origin > "$work/run.out" 2>&1)
}

for recipe in true false; do
	checks=$((checks + 1))
	tmp=$(fallback_run "$recipe")
	status=$?
	if [ "$recipe" = true ] && [ "$status" -ne 0 ]; then
		fail "a passing make check fallback passes" "$(cat "$work/run.out")"
	elif [ "$recipe" = false ] && [ "$status" -eq 0 ]; then
		fail "a failing make check fallback blocks the push" "$(cat "$work/run.out")"
	elif [ -n "$(ls -A "$tmp")" ]; then
		fail "the make check fallback ($recipe) leaves its scratch directory behind" "$(ls -A "$tmp")"
	else
		pass "the make check fallback ($recipe) reports its status and cleans up"
	fi
done

# --- the hook itself ----------------------------------------------------------

hook="$here/../.githooks/pre-push"
checks=$((checks + 1))
if sed 's/#.*//' "$hook" | grep -q 'scripts/pre_push_check.sh' &&
	! sed 's/#.*//' "$hook" | grep -qE 'make[[:space:]]+check'; then
	pass "the pre-push hook runs the selector, not make check"
else
	fail "the pre-push hook runs the selector, not make check" "$(cat "$hook")"
fi

echo
if [ "$failures" -eq 0 ]; then
	echo "all $checks pre-push checks passed"
	exit 0
fi
echo "$failures of $checks pre-push checks failed"
exit 1
