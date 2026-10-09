#!/bin/sh
# The checks .githooks/pre-push runs: only those the pushed change touches
# (issue #335). The whole suite is CI's job; `make check` remains the way to
# run all of it locally.
#
# The change is, for each pushed ref, the difference between the local branch
# and the same branch on the remote — `<remote sha>..<local sha>` from the lines
# git hands pre-push on stdin. A branch the remote does not have yet is
# measured from its merge base with <remote>/main, so every branch's first push
# checks what the branch adds and nothing main gained since. Deleting a branch
# checks nothing.
#
# What the changed files select:
#
#   .go files           gofmt -s on each; go vet, golangci-lint and go test
#                       (no -race) on the packages that hold them, never on
#                       their importers
#   <pkg>/testdata/...  that package's go vet, golangci-lint and go test
#   frontend/src TS     tsc --noEmit, vitest related on the changed modules
#   testdata/api-contract  the frontend contract test that parses those fixtures
#   scripts/<x>.sh      scripts/<x>_test.sh, and the tmpdir guard for any .sh
#   .claude/            the hook tests
#   frontend/screenshots, frontend/e2e/*.sh  the screenshot fixtures' tests
#   *.md                the documentation link check
#   docs/scenarios.md   the scenario ledger check
#
# Falls back to `make check` — never to checking nothing — when a change cannot
# be narrowed (go.mod, the Makefile, the runtime pins, the hook itself; see
# full_reason below) or the range cannot be worked out (an unknown remote
# commit, no <remote>/main, a malformed line).
#
#   sh scripts/pre_push_check.sh [--plan] [remote] < pre-push-lines
#
# --plan prints what would run, one check per line, and runs nothing.

set -u

plan_only=no
if [ "${1:-}" = --plan ]; then
	plan_only=yes
	shift
fi
remote=${1:-origin}

toolchain_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
repo_root=$(git rev-parse --show-toplevel) || exit 1
cd "$repo_root" || exit 1

zero=0000000000000000000000000000000000000000
work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-pre-push.XXXXXX") || exit 1
trap 'rm -rf "$work"' EXIT
files="$work/files"
: > "$files"
full=""

# full_reason <path> — prints why a change to <path> cannot be narrowed, or
# nothing when it can.
full_reason() {
	case "$1" in
	go.mod | go.sum) echo "the Go module changed" ;;
	Makefile) echo "the Makefile changed" ;;
	.golangci.yml) echo "the Go lint configuration changed" ;;
	.node-version) echo "the Node pin changed" ;;
	frontend/package.json | frontend/package-lock.json | frontend/tsconfig*.json | \
		frontend/vite.config.* | frontend/vitest.config.*)
		echo "the frontend toolchain configuration changed" ;;
	scripts/runtime-* | scripts/go-toolchain.sh | scripts/go-shell.sh | \
		scripts/node-toolchain.sh | scripts/npm.sh | scripts/check-node.sh)
		echo "the runtime selection changed" ;;
	.githooks/*) echo "the Git hooks changed" ;;
	esac
}

# Collect the changed paths of every pushed ref into $files, one per line.
while IFS=' ' read -r local_ref local_sha remote_ref remote_sha; do
	[ -n "${local_ref:-}" ] || continue
	if [ -z "${remote_sha:-}" ]; then
		full="a pre-push line could not be read"
		break
	fi
	[ "$local_sha" = "$zero" ] && continue
	if [ "$remote_sha" = "$zero" ]; then
		if ! from=$(git merge-base "refs/remotes/$remote/main" "$local_sha" 2> /dev/null); then
			full="$local_ref is new and there is no $remote/main to measure it from"
			break
		fi
	elif git cat-file -e "$remote_sha^{commit}" 2> /dev/null; then
		from=$remote_sha
	else
		full="$remote_ref on $remote is at a commit this clone does not have"
		break
	fi
	# -z: a path with a space or a non-ASCII character comes through verbatim
	# rather than quoted. --no-renames: a rename's old path is a change too.
	if ! git diff --name-only --no-renames -z "$from" "$local_sha" >> "$files"; then
		full="the change in $local_ref could not be listed"
		break
	fi
done
tr '\0' '\n' < "$files" | sort -u > "$files.sorted"

if [ -z "$full" ]; then
	while IFS= read -r f; do
		reason=$(full_reason "$f")
		if [ -n "$reason" ]; then
			full="$reason ($f)"
			break
		fi
	done < "$files.sorted"
fi

plan="$work/plan"
: > "$plan"
add() { printf '%s\n' "$1" >> "$plan"; }

# is_go_pkg <dir> — the directory still exists and holds Go files.
is_go_pkg() {
	[ -d "$1" ] || return 1
	for g in "$1"/*.go; do
		[ -f "$g" ] && return 0
	done
	return 1
}

add_go_pkg() {
	is_go_pkg "$1" || return 0
	add "go-vet ./$1"
	add "golangci-lint ./$1"
	add "go-test ./$1"
	[ "$1" = . ] && add build-frontend
	return 0
}

add_shell_test() {
	[ -f "$1" ] && add "shell $1"
	return 0
}

if [ -n "$full" ]; then
	add "full"
else
	while IFS= read -r f; do
		[ -n "$f" ] || continue
		case "$f" in
		testdata/api-contract/*)
			add "vitest src/schemas/contract.test.ts"
			add tsc
			;;
		*.go)
			[ -f "$f" ] && add "gofmt $f"
			add_go_pkg "$(dirname "$f")"
			;;
		testdata/* | */testdata/*)
			case "$f" in
			testdata/*) add_go_pkg . ;;
			*) add_go_pkg "${f%%/testdata/*}" ;;
			esac
			;;
		frontend/src/*.ts | frontend/src/*.tsx)
			add tsc
			[ -f "$f" ] && add "vitest ${f#frontend/}"
			;;
		esac
		case "$f" in
		scripts/*_test.sh) add_shell_test "$f" ;;
		scripts/*.sh) add_shell_test "${f%.sh}_test.sh" ;;
		esac
		case "$f" in
		.claude/*) add_shell_test .claude/hooks/hooks_test.sh ;;
		frontend/screenshots/* | frontend/e2e/*.sh)
			add_shell_test scripts/screenshots_check_test.sh
			add_shell_test frontend/screenshots/screenshots-env_test.sh
			;;
		esac
		case "$f" in
		*.sh) add_shell_test scripts/tmpdir_guard_test.sh ;;
		esac
		case "$f" in
		*.md) add docs-links ;;
		esac
		[ "$f" = docs/scenarios.md ] && add scenarios
	done < "$files.sorted"
fi

sort -u "$plan" > "$plan.sorted"
[ -s "$plan.sorted" ] || echo nothing > "$plan.sorted"

if [ "$plan_only" = yes ]; then
	[ -n "$full" ] && echo "# $full" >&2
	cat "$plan.sorted"
	exit 0
fi

if [ -n "$full" ]; then
	echo "pre-push: $full; running make check"
	# Not exec: that would replace this shell before its EXIT trap removes
	# $work, leaving a scratch directory behind on every such push.
	make check
	exit $?
fi
if grep -qx nothing "$plan.sorted"; then
	echo "pre-push: nothing to check"
	exit 0
fi

panemux_runtime_scripts="$toolchain_root/scripts"
. "$panemux_runtime_scripts/runtime-env.sh"
panemux_runtime "$toolchain_root" || exit 1

# The checks are listed by kind; gather each kind's arguments.
args() { sed -n "s/^$1 //p" "$plan.sorted"; }

failed=""
run() {
	label=$1
	shift
	echo "pre-push: $label"
	if ! "$@"; then
		failed="$failed
  $label"
	fi
}

gofmt_check() {
	out=$(args gofmt | tr '\n' '\0' | xargs -0 gofmt -s -l) || return 1
	[ -z "$out" ] && return 0
	printf 'not gofmt -s clean (run: make fmt):\n%s\n' "$out"
	return 1
}

# golangci_lint <pkg>... — the pinned golangci-lint `make lint-go` runs, with
# this checkout's own cache, over the given packages only.
golangci_lint() {
	make -s -C "$toolchain_root" lint-go-deps || return 1
	lint_bin=$(go env GOBIN)
	[ -n "$lint_bin" ] || lint_bin="$(go env GOPATH)/bin"
	lint_cache=$(sh "$toolchain_root/scripts/golangci_lint_cache.sh") || return 1
	GOLANGCI_LINT_CACHE="$lint_cache" "$lint_bin/golangci-lint" run "$@"
}

go_pkgs=$(args go-test | tr '\n' ' ')
lint_pkgs=$(args golangci-lint | tr '\n' ' ')
frontend_tests=$(args vitest | tr '\n' ' ')

if grep -qx build-frontend "$plan.sorted" && [ ! -f frontend/dist/index.html ]; then
	run "build the embedded frontend" make build-frontend
fi
if grep -q '^gofmt ' "$plan.sorted"; then
	run "gofmt -s" gofmt_check
fi
if [ -n "$go_pkgs" ]; then
	# shellcheck disable=SC2086 # one argument per package
	run "go vet $go_pkgs" go vet $go_pkgs
	# shellcheck disable=SC2086 # one argument per package
	run "golangci-lint run $lint_pkgs" golangci_lint $lint_pkgs
	# shellcheck disable=SC2086 # one argument per package
	run "go test $go_pkgs" go test $go_pkgs
fi
if grep -qx tsc "$plan.sorted"; then
	run "tsc --noEmit" sh -c 'cd frontend && npx --no-install tsc --noEmit'
fi
if [ -n "$frontend_tests" ]; then
	# --passWithNoTests: a module no test imports is not a failure.
	# shellcheck disable=SC2086 # one argument per module
	run "vitest related $frontend_tests" sh -c 'cd frontend && npx --no-install vitest related --run --passWithNoTests "$@"' sh $frontend_tests
fi
for t in $(args shell); do
	run "sh $t" sh "$t"
done
if grep -qx docs-links "$plan.sorted"; then
	run "documentation links" sh scripts/docs_links_check.sh
fi
if grep -qx scenarios "$plan.sorted"; then
	run "scenario ledger" sh scripts/scenarios_check.sh
fi

if [ -n "$failed" ]; then
	printf '\npre-push: these checks failed:%s\n' "$failed"
	echo "pre-push: the whole suite is \`make check\`; CI runs it on the pull request."
	exit 1
fi
echo "pre-push: the checks for this change passed (CI runs the whole suite)"
