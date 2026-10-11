#!/bin/sh
#
# Tests for the G1/G2 hook scripts in this directory.
#
# The hooks themselves are the enforcement mechanism for
# docs/quality-gateway.md's gates G1 (edit) and G2 (unit), so a hook that
# silently does nothing is worse than no hook at all — it reports the
# discipline as enforced while enforcing nothing. Design principle 4 (no false
# positives) cuts the other way just as hard: a hook that blocks a healthy
# change gets bypassed, and then so does every other hook.
#
# Both directions are asserted below. Run with:
#
#   make test-hooks

set -u

hooks_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$hooks_dir/../.." && pwd)

failures=0
checks=0

fail() {
	failures=$((failures + 1))
	echo "FAIL: $1"
}

pass() {
	echo "ok   $1"
}

# expect_status <want> <name> <command...>
expect_status() {
	want=$1
	name=$2
	shift 2
	checks=$((checks + 1))

	output=$("$@" 2>&1)
	got=$?
	if [ "$got" -eq "$want" ]; then
		pass "$name"
		return
	fi
	fail "$name: wanted exit $want, got $got"
	[ -n "$output" ] && printf '     output: %s\n' "$output"
}

# Run from inside the scratch directory: a fixture path that somehow came back
# empty then means `cd ""` stays here, never in the checkout under test.
work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-hooks-test.XXXXXX") || exit 1
trap 'rm -rf "$work"' EXIT
cd "$work" || exit 1

# A Go file gofmt -s would rewrite: the indentation is spaces, not a tab.
cat > "$work/unformatted.go" <<'GO'
package sample

func Sample() int {
    return 1
}
GO

cat > "$work/formatted.go" <<'GO'
package sample

func Sample() int {
	return 1
}
GO

echo "# not code" > "$work/notes.md"

edit_hook="$hooks_dir/post-edit-check.sh"

# --- G1: the edit gate -------------------------------------------------------

expect_status 2 "an unformatted Go file is blocked" \
	"$edit_hook" --file "$work/unformatted.go"

expect_status 0 "a formatted Go file passes" \
	"$edit_hook" --file "$work/formatted.go"

# Principle 4 in both directions: a file the gate has no opinion about must
# never block, and neither must a path that no longer exists (Claude Code can
# report an edit to a file a later step removed).
expect_status 0 "a non-code file is ignored" \
	"$edit_hook" --file "$work/notes.md"

expect_status 0 "a missing file is ignored" \
	"$edit_hook" --file "$work/gone.go"

expect_status 0 "no file argument at all is ignored" \
	"$edit_hook" --file ""

# The real invocation is Claude Code piping hook JSON on stdin, not --file.
# Reading that JSON is the part most likely to break silently, so it is
# asserted against the same fixtures rather than trusted.
stdin_status() {
	printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' "$1" | "$edit_hook"
}

# These three parse the hook payload, so they need jq. Without it the edit hook
# deliberately exits 0 (and says so on stderr), which is correct behavior but
# not what these fixtures assert — so they are skipped rather than failed.
if command -v jq > /dev/null 2>&1; then
	expect_status 2 "hook JSON on stdin reaches the same verdict (unformatted)" \
		stdin_status "$work/unformatted.go"

	expect_status 0 "hook JSON on stdin reaches the same verdict (formatted)" \
		stdin_status "$work/formatted.go"

	# Claude Code reports the written path under tool_response.filePath for some
	# tools and tool_input.file_path for others; both spellings must work.
	printf '{"tool_name":"Write","tool_response":{"filePath":"%s"}}' "$work/unformatted.go" \
		> "$work/response-shape.json"
	response_shape_status() {
		"$edit_hook" < "$work/response-shape.json"
	}
	expect_status 2 "the tool_response.filePath spelling is read too" response_shape_status
fi

# Malformed input is not a reason to block a turn.
malformed_status() {
	printf 'not json at all' | "$edit_hook"
}
expect_status 0 "malformed hook input does not block" malformed_status

# --- G2: the stop gate -------------------------------------------------------

stop_hook="$hooks_dir/stop-check.sh"

# write_sub_go writes a Go file declaring `package sub`, either gofmt-clean
# (tab-indented) or not (space-indented).
#
# The package name matters and is why this exists rather than reusing the
# top-level fixtures: those declare `package sample`, and dropping one into
# sub/ produces a package-name mismatch. Several cases below would then have
# gone red for THAT reason rather than the one they are named after — passing
# tests that prove the wrong thing, which is the failure this whole suite is
# about.
write_sub_go() {
	if [ "$3" = formatted ]; then
		printf 'package sub\n\nfunc %s() int {\n\treturn 1\n}\n' "$2" > "$1"
	else
		printf 'package sub\n\nfunc %s() int {\n    return 1\n}\n' "$2" > "$1"
	fi
}

# fixture_repo makes a throwaway git repo with one committed, correctly
# formatted Go package, and prints its path. Every stop-gate case below builds
# on it so the "before" state is always healthy.
fixture_repo() {
	dir=$(mktemp -d "$work/repo.XXXXXX") || exit 1
	(
		cd "$dir" || exit 1
		git init -q .
		git config user.email "test@example.invalid"
		git config user.name "hook test"
		printf 'module sample\n\ngo 1.25\n' > go.mod
		mkdir -p sub
		write_sub_go sub/s.go S formatted
		git add -A
		git commit -q -m base
	)
	echo "$dir"
}

# expect_block_reason <substring> <name> <command...> — asserts exit 2 AND that
# the reason mentions <substring>.
#
# Exit code alone is not enough for the subdirectory case: with the cwd fix
# reverted the hook still returned 2, but because `go test ./sub` was handed a
# path that does not exist from that cwd — the right answer for the wrong
# reason, which would have let the fix regress with the test still green.
expect_block_reason() {
	want=$1
	name=$2
	shift 2
	checks=$((checks + 1))

	output=$("$@" 2>&1)
	got=$?
	if [ "$got" -ne 2 ]; then
		fail "$name: wanted exit 2, got $got"
		return
	fi
	case "$output" in
	*"$want"*) pass "$name" ;;
	*)
		fail "$name: blocked, but not for the expected reason (wanted \"$want\")"
		printf '%s\n' "$output" | sed 's/^/     /'
		;;
	esac
}

# run_stop runs the stop hook inside dir (optionally a subdirectory of it) with
# stdin closed, which is what a hook invocation looks like once Claude Code has
# written its payload. Passing stdin explicitly also keeps the suite fast: the
# script bounds an idle stdin with a 5s timeout, and inheriting the test
# runner's own pipe would pay that on every call.
run_stop() {
	( cd "$1" || exit 1; "$stop_hook" < /dev/null > /dev/null 2>&1 )
}

# Same, but with stderr kept so the blocking reason can be asserted.
run_stop_verbose() {
	( cd "$1" || exit 1; "$stop_hook" < /dev/null 2>&1 )
}

# A tree with nothing changed has nothing to verify.
clean_tree_status() {
	d=$(fixture_repo); run_stop "$d"; status=$?; rm -rf "$d"; return $status
}
expect_status 0 "a clean tree stops immediately" clean_tree_status

# The stop gate refuses to end a turn that leaves unformatted Go behind.
dirty_tree_status() {
	d=$(fixture_repo)
	write_sub_go "$d/sub/s.go" S unformatted
	run_stop "$d"; status=$?; rm -rf "$d"; return $status
}
expect_status 2 "unformatted Go blocks the turn from ending" dirty_tree_status

# ...and lets a healthy one through. This is the case that decides whether the
# gate keeps its credibility.
healthy_tree_status() {
	d=$(fixture_repo)
	write_sub_go "$d/sub/t.go" T formatted
	run_stop "$d"; status=$?; rm -rf "$d"; return $status
}
expect_status 0 "a healthy change does not block the turn" healthy_tree_status

# --- the four ways this gate used to pass while checking nothing -------------
#
# Each of these was a real defect found in review. They share one shape: the
# gate returned 0 having examined no files at all, which is strictly worse than
# having no gate, because it reports the discipline as enforced. They are
# grouped here so that shape stays visible.

# git status --porcelain prints paths relative to the REPOSITORY ROOT, so a
# session whose cwd is a subdirectory used to fail every [ -f ] test and pass
# everything.
subdir_run() {
	d=$(fixture_repo)
	write_sub_go "$d/sub/s.go" S unformatted
	run_stop_verbose "$d/sub"; status=$?; rm -rf "$d"; return $status
}
expect_block_reason "gofmt" "an unformatted file is caught from a subdirectory too" subdir_run

# git collapses a wholly-untracked directory into one "?? newpkg/" entry, so a
# turn that adds an entire package was never checked — the largest kind of
# change, invisible.
new_package_status() {
	d=$(fixture_repo)
	mkdir -p "$d/newpkg"
	printf 'package newpkg\n\nfunc N() int {\n    return 1\n}\n' > "$d/newpkg/n.go"
	run_stop "$d"; status=$?; rm -rf "$d"; return $status
}
expect_status 2 "a brand-new package directory is checked" new_package_status

# A deleted .go file used to drop its package from the test set, even though
# deleting a file is one of the likeliest ways to break a package's build.
deleted_file_status() {
	d=$(fixture_repo)
	(
		cd "$d" || exit 1
		printf 'package sub\n\nfunc Caller() int {\n\treturn Helper()\n}\n' > sub/caller.go
		printf 'package sub\n\nfunc Helper() int {\n\treturn 1\n}\n' > sub/helper.go
		git add -A && git commit -q -m two
		git rm -q sub/helper.go
	)
	run_stop "$d"; status=$?; rm -rf "$d"; return $status
}
expect_status 2 "deleting a .go file still tests its package" deleted_file_status

# git double-quotes paths containing spaces, so field-splitting the porcelain
# output produced a path that did not exist and the file was skipped.
spaced_path_status() {
	d=$(fixture_repo)
	write_sub_go "$d/sub/my file.go" Spaced unformatted
	run_stop "$d"; status=$?; rm -rf "$d"; return $status
}
expect_status 2 "a path containing a space is not silently skipped" spaced_path_status

# --- nested worktrees (issue #350) -------------------------------------------
#
# A session can run in a worktree nested inside the main checkout
# (<repo>/.claude/worktrees/<name>), while Claude Code still runs the MAIN
# checkout's hooks. Both gates must then check the worktree the edited file
# belongs to: the edit gate used to vet ./.claude/worktrees/<name>/<pkg> in
# the main module, and the stop gate wrote gofmt's stderr to
# "$repo_root/.git/...", which in a worktree is a file, not a directory — so
# gofmt never ran and an unformatted file passed.

# fixture_nested_worktree makes a fixture repo with a worktree at
# .claude/worktrees/wt inside it, and prints the repo's path.
fixture_nested_worktree() {
	d=$(fixture_repo)
	git -C "$d" worktree add -q "$d/.claude/worktrees/wt" -b wt > /dev/null 2>&1 || exit 1
	echo "$d"
}

write_vet_bad() {
	printf 'package sub\n\nimport "fmt"\n\n// Bad has a vet diagnostic on purpose.\nfunc Bad() { fmt.Printf("%%%%d\\n", "not an int") }\n' > "$1"
}

if command -v go > /dev/null 2>&1; then
	# The edit gate vets the package in the module the file belongs to, and
	# names it relative to that module — not relative to the hook's checkout.
	nested_vet_run() {
		d=$(fixture_nested_worktree)
		write_vet_bad "$d/.claude/worktrees/wt/sub/bad.go"
		"$edit_hook" --file "$d/.claude/worktrees/wt/sub/bad.go" --repo "$d" 2>&1
		status=$?; rm -rf "$d"; return $status
	}
	expect_block_reason "go vet ./sub failed" \
		"a vet diagnostic in a nested worktree is vetted in that worktree" nested_vet_run

	nested_clean_status() {
		d=$(fixture_nested_worktree)
		write_sub_go "$d/.claude/worktrees/wt/sub/t.go" T formatted
		"$edit_hook" --file "$d/.claude/worktrees/wt/sub/t.go" --repo "$d"
		status=$?; rm -rf "$d"; return $status
	}
	expect_status 0 "a healthy file in a nested worktree passes the edit gate" nested_clean_status

	# The same, for the main checkout that holds the worktree.
	main_checkout_vet_run() {
		d=$(fixture_nested_worktree)
		write_vet_bad "$d/sub/bad.go"
		"$edit_hook" --file "$d/sub/bad.go" --repo "$d" 2>&1
		status=$?; rm -rf "$d"; return $status
	}
	expect_block_reason "go vet ./sub failed" \
		"a vet diagnostic in the main checkout is vetted there" main_checkout_vet_run
fi

# --- checkouts of other repositories ------------------------------------------
#
# The edit gate checks only checkouts of the repository its own hooks belong
# to: the main checkout and any worktree of it. A file in an unrelated git
# repository — an agent's throwaway reproduction under $TMPDIR — is not this
# repository's to vet or type-check. Vetting it blocked on errors that say
# nothing about the change (no go.mod; a go directive newer than the pinned
# toolchain), and the tsc step ran that repository's own node_modules/.bin/tsc.
# --repo is not passed here: the fixtures are compared with the hook's real
# checkout, exactly as Claude Code runs it.

# fixture_foreign_repo makes a git repository unrelated to this one.
fixture_foreign_repo() {
	dir=$(mktemp -d "$work/foreign.XXXXXX") || exit 1
	git -C "$dir" init -q . || exit 1
	echo "$dir"
}

if command -v go > /dev/null 2>&1; then
	foreign_no_module_status() {
		d=$(fixture_foreign_repo)
		printf 'package main\n\nfunc main() {}\n' > "$d/main.go"
		"$edit_hook" --file "$d/main.go"
		status=$?; rm -rf "$d"; return $status
	}
	expect_status 0 "a Go file in an unrelated repository without go.mod is not vetted" \
		foreign_no_module_status

	foreign_new_go_status() {
		d=$(fixture_foreign_repo)
		printf 'module sample\n\ngo 1.99\n' > "$d/go.mod"
		printf 'package main\n\nfunc main() {}\n' > "$d/main.go"
		"$edit_hook" --file "$d/main.go"
		status=$?; rm -rf "$d"; return $status
	}
	expect_status 0 "a Go file in an unrelated module needing a newer Go is not vetted" \
		foreign_new_go_status

	# A worktree of this repository whose go.mod asks for a newer Go than
	# the pinned toolchain (a toolchain-bump branch) cannot be loaded at all;
	# that is reported, not blocked on.
	newer_go_worktree_status() {
		d=$(fixture_repo)
		printf 'module sample\n\ngo 1.99\n' > "$d/go.mod"
		"$edit_hook" --file "$d/sub/s.go" --repo "$d"
		status=$?; rm -rf "$d"; return $status
	}
	expect_status 0 "a module the pinned Go cannot load is not blocked on" \
		newer_go_worktree_status

	# The go command prints progress lines with the same "go: " prefix —
	# "go: downloading ..." on the first vet after a dependency change — and
	# then runs vet normally. A genuine diagnostic behind them must still
	# block. A file:// module proxy and an empty module cache make the
	# download real without the network; python3 builds the module zip.
	if command -v python3 > /dev/null 2>&1; then
		download_then_vet_run() {
			d=$(fixture_repo)
			proxy="$work/proxy"
			modcache=$(mktemp -d "$work/modcache.XXXXXX") || exit 1
			mkdir -p "$proxy/example.com/dep/@v"
			printf 'v1.0.0\n' > "$proxy/example.com/dep/@v/list"
			printf '{"Version":"v1.0.0"}\n' > "$proxy/example.com/dep/@v/v1.0.0.info"
			printf 'module example.com/dep\n\ngo 1.25\n' > "$proxy/example.com/dep/@v/v1.0.0.mod"
			python3 -c 'import sys, zipfile
z = zipfile.ZipFile(sys.argv[1], "w")
z.writestr("example.com/dep@v1.0.0/go.mod", "module example.com/dep\n\ngo 1.25\n")
z.writestr("example.com/dep@v1.0.0/dep.go", "package dep\n\nfunc Name() string { return \"dep\" }\n")
z.close()' "$proxy/example.com/dep/@v/v1.0.0.zip"
			printf 'module sample\n\ngo 1.25\n\nrequire example.com/dep v1.0.0\n' > "$d/go.mod"
			printf 'package sub\n\nimport (\n\t"fmt"\n\n\t"example.com/dep"\n)\n\n// Bad has a vet diagnostic on purpose.\nfunc Bad() { fmt.Printf("%%%%d\\n", dep.Name()) }\n' > "$d/sub/bad.go"
			GOFLAGS=-mod=mod GOPROXY="file://$proxy" GOSUMDB=off GOMODCACHE="$modcache" \
				"$edit_hook" --file "$d/sub/bad.go" --repo "$d" 2>&1
			status=$?
			# The module cache is read-only; only go can remove it.
			GOMODCACHE="$modcache" go clean -modcache > /dev/null 2>&1
			rm -rf "$d" "$proxy" "$modcache"
			return $status
		}
		expect_block_reason "go vet ./sub failed" \
			"a vet diagnostic after go: downloading lines still blocks" download_then_vet_run
	else
		echo "skip python3 not installed — the go: downloading case is skipped"
	fi
fi

# gofmt needs no repository, so it still runs on a foreign file.
foreign_unformatted_run() {
	d=$(fixture_foreign_repo)
	printf 'package main\n\nfunc main() {\n    println()\n}\n' > "$d/main.go"
	"$edit_hook" --file "$d/main.go" 2>&1
	status=$?; rm -rf "$d"; return $status
}
expect_block_reason "not gofmt -s clean" \
	"gofmt still checks a file in an unrelated repository" foreign_unformatted_run

foreign_tsc_status() {
	d=$(fixture_foreign_repo)
	mkdir -p "$d/frontend/node_modules/.bin"
	printf '#!/bin/sh\necho FOREIGN_TSC_RAN >&2\nexit 1\n' > "$d/frontend/node_modules/.bin/tsc"
	chmod +x "$d/frontend/node_modules/.bin/tsc"
	printf '{"name":"sample"}\n' > "$d/frontend/package.json"
	printf 'export const a = 1\n' > "$d/frontend/x.ts"
	output=$("$edit_hook" --file "$d/frontend/x.ts" 2>&1)
	status=$?; rm -rf "$d"
	case "$output" in *FOREIGN_TSC_RAN*) return 3 ;; esac
	return $status
}
expect_status 0 "an unrelated repository's own tsc is never run" foreign_tsc_status

# A worktree of the hook's repository is still checked through --repo, and a
# file outside it is not: the same guard, seen from the fixture side.
if command -v go > /dev/null 2>&1; then
	other_repo_vet_status() {
		d=$(fixture_repo)
		o=$(fixture_foreign_repo)
		cp "$d/go.mod" "$o/go.mod"
		mkdir -p "$o/sub"
		write_vet_bad "$o/sub/bad.go"
		"$edit_hook" --file "$o/sub/bad.go" --repo "$d"
		status=$?; rm -rf "$d" "$o"; return $status
	}
	expect_status 0 "a vet diagnostic outside the hook's repository is not reported" \
		other_repo_vet_status
fi

nested_stop_unformatted_run() {
	d=$(fixture_nested_worktree)
	write_sub_go "$d/.claude/worktrees/wt/sub/s.go" S unformatted
	run_stop_verbose "$d/.claude/worktrees/wt"; status=$?; rm -rf "$d"; return $status
}
expect_block_reason "not gofmt -s clean" \
	"the stop gate runs gofmt in a nested worktree" nested_stop_unformatted_run

nested_stop_parse_run() {
	d=$(fixture_nested_worktree)
	printf 'package sub\n\nfunc Broken( {\n' > "$d/.claude/worktrees/wt/sub/broken.go"
	run_stop_verbose "$d/.claude/worktrees/wt"; status=$?; rm -rf "$d"; return $status
}
expect_block_reason "do not parse" \
	"the stop gate reports a parse error in a nested worktree" nested_stop_parse_run

# A $TMPDIR that cannot hold a temporary file is a problem with the machine,
# not with the change (principle 4). The gofmt check must still run, and the
# turn is blocked only for what it finds.
stop_no_tmpdir_run() {
	d=$(fixture_repo)
	write_sub_go "$d/sub/s.go" S unformatted
	# A regular file, not a missing directory: the toolchain setup creates a
	# missing $TMPDIR, so only a path that cannot become a directory makes
	# mktemp fail.
	: > "$work/tmpdir-is-a-file"
	( cd "$d" || exit 1; TMPDIR="$work/tmpdir-is-a-file" "$stop_hook" < /dev/null 2>&1 )
	status=$?; rm -rf "$d"; return $status
}
expect_block_reason "not gofmt -s clean" \
	"the stop gate still runs gofmt when TMPDIR is unusable" stop_no_tmpdir_run

# --- the loop bound ----------------------------------------------------------
#
# Claude Code sets stop_hook_active when it is already continuing BECAUSE of a
# previous block. Without honouring it, a condition the turn cannot fix — a
# pre-existing dirty file, or the deliberately-red test DEVELOPMENT.md's TDD
# rule asks for — blocks, resumes and blocks again with no bound.
if command -v jq > /dev/null 2>&1; then
	stop_active_status() {
		d=$(fixture_repo)
		write_sub_go "$d/sub/s.go" S unformatted
		( cd "$d" || exit 1; printf '{"stop_hook_active":true}' | "$stop_hook" > /dev/null 2>&1 )
		status=$?; rm -rf "$d"; return $status
	}
	expect_status 0 "stop_hook_active bounds the block instead of looping" stop_active_status

	stop_inactive_status() {
		d=$(fixture_repo)
		write_sub_go "$d/sub/s.go" S unformatted
		( cd "$d" || exit 1; printf '{"stop_hook_active":false}' | "$stop_hook" > /dev/null 2>&1 )
		status=$?; rm -rf "$d"; return $status
	}
	expect_status 2 "the first block still blocks" stop_inactive_status
fi

# A hook that hangs is worse than one that answers wrongly: the turn cannot end
# at all. An inherited, idle stdin must not do that.
if command -v timeout > /dev/null 2>&1; then
	no_hang_status() {
		d=$(fixture_repo)
		( cd "$d" || exit 1; sleep 30 | timeout 20 "$stop_hook" > /dev/null 2>&1 )
		status=$?; rm -rf "$d"
		[ "$status" -eq 124 ] && return 1 # 124 = timeout killed it, i.e. it hung
		return 0
	}
	expect_status 0 "an idle inherited stdin does not hang the hook" no_hang_status
fi

# --- the edit gate's in-repo branches ----------------------------------------
#
# post-edit-check.sh deliberately skips `go vet` for files outside $repo_root
# and `tsc` for files outside $repo_root/frontend, so every fixture above —
# all of which live under mktemp -d — exercises only the gofmt path. Both
# branches worked when checked by hand, which is exactly the problem: a
# regression in the package-path computation, or in either invocation, would
# leave the whole suite green while the gate checked nothing.
#
# These two fixtures therefore live INSIDE the repository, and are removed
# again whatever happens.

vet_fixture_dir="$repo_root/internal/zzhookfixture"
cleanup_repo_fixtures() {
	rm -rf "$vet_fixture_dir"
	rm -f "$repo_root/frontend/src/zz-hook-fixture.ts"
}
trap 'rm -rf "$work"; cleanup_repo_fixtures' EXIT

if command -v go > /dev/null 2>&1; then
	mkdir -p "$vet_fixture_dir"
	# A genuine vet diagnostic in a package that compiles.
	printf 'package zzhookfixture\n\nimport "fmt"\n\n// Bad has a vet diagnostic on purpose.\nfunc Bad() { fmt.Printf("%%%%d\\n", "not an int") }\n' \
		> "$vet_fixture_dir/bad.go"
	expect_status 2 "a real go vet diagnostic blocks the edit" \
		"$edit_hook" --file "$vet_fixture_dir/bad.go"

	# The state DEVELOPMENT.md's TDD rule mandates: a test naming a function
	# that does not exist yet. go vet cannot type-check it, and blocking here
	# would reject every red test at the moment it is written — inverting the
	# rule this gate exists to support.
	rm -f "$vet_fixture_dir/bad.go"
	printf 'package zzhookfixture\n\nfunc Good() int {\n\treturn 1\n}\n' > "$vet_fixture_dir/good.go"
	printf 'package zzhookfixture\n\nimport "testing"\n\nfunc TestNotYet(t *testing.T) {\n\tif NotYetImplemented() != 1 {\n\t\tt.Fatal("red on purpose")\n\t}\n}\n' \
		> "$vet_fixture_dir/good_test.go"
	expect_status 0 "a package that does not compile yet does not block (TDD red)" \
		"$edit_hook" --file "$vet_fixture_dir/good_test.go"

	cleanup_repo_fixtures
fi

if [ -d "$repo_root/frontend/node_modules" ]; then
	printf 'export const broken: number = "not a number"\n' \
		> "$repo_root/frontend/src/zz-hook-fixture.ts"
	expect_status 2 "a TypeScript type error blocks the edit" \
		"$edit_hook" --file "$repo_root/frontend/src/zz-hook-fixture.ts"
	cleanup_repo_fixtures
fi

# --- D6: the stop gate is not make check -------------------------------------
#
# Putting all of make check in the Stop hook would run the frontend build and
# the whole -race suite on every turn. Decision D6 says explicitly not to, and
# a gate that sacrifices fast feedback gets bypassed — so the absence is
# asserted rather than left to reviewer memory.
# Comment lines are stripped first: the script's own header explains why it is
# not make check, and matching that sentence would make this a false positive —
# the exact failure mode principle 4 warns about, in the test that guards it.
checks=$((checks + 1))
if sed 's/#.*//' "$stop_hook" | grep -qE 'make[[:space:]]+(check|build|test-e2e)\b'; then
	fail "the Stop hook must not run make check / make build / make test-e2e (decision D6)"
else
	pass "the Stop hook stays inside G1+G2 (decision D6)"
fi

# --- settings.json wiring ----------------------------------------------------
#
# The scripts above are only a gate if settings.json actually points at them.
settings="$repo_root/.claude/settings.json"

# jq is not installed by `make install-deps`, and this suite runs inside
# `make test` -> `make check`, and the pre-push hook runs it whenever .claude/
# changes. Without the guard, a contributor without jq could not pass either, and the failures blamed a
# perfectly healthy settings.json and claimed the hooks were unwired — a false
# positive of exactly the shape principle 4 warns about, landing in the suite
# that exists to guard against it. Skipped is reported as skipped, never as
# passed: a check that quietly turns into a no-op is the other failure mode.
have_jq=no
if command -v jq > /dev/null 2>&1; then
	have_jq=yes
else
	echo "skip jq not installed — the settings.json and stdin checks below are skipped"
	echo "     (install jq to run them; see DEVELOPMENT.md)"
fi

if [ "$have_jq" = yes ]; then
	checks=$((checks + 1))
	if jq -e . "$settings" > /dev/null 2>&1; then
		pass "settings.json is valid JSON"
	else
		fail "settings.json is not valid JSON — a malformed file silently disables every setting in it"
	fi
fi

assert_wired() {
	event=$1
	script=$2
	[ "$have_jq" = yes ] || return 0
	checks=$((checks + 1))
	if jq -e --arg s "$script" \
		".hooks.$event[].hooks[] | select(.command | contains(\$s))" \
		"$settings" > /dev/null 2>&1; then
		pass "$event is wired to $script"
	else
		fail "$event is not wired to $script"
	fi
}

assert_wired PostToolUse post-edit-check.sh
assert_wired Stop stop-check.sh

echo
if [ "$failures" -eq 0 ]; then
	echo "all $checks hook checks passed"
	exit 0
fi
echo "$failures of $checks hook checks failed"
exit 1
