#!/bin/sh
#
# red-check: gate G4(b) in docs/quality-gateway.md.
#
# The rule this enforces is decision D4. DEVELOPMENT.md requires tests to be
# written first and confirmed failing, and that rule is stuck at rung L0 of the
# enforcement ladder because it cannot be checked after the fact — nothing in a
# merged branch records the order its lines were written in. The *result* can be
# checked, though, and it is the part that actually matters:
#
#   A test this branch changed must FAIL when this branch's implementation
#   diff is reverted.
#
# That is the strongest possible mutation — remove the implementation entirely —
# and it catches both shapes this repository's existing gates are blind to: a
# tautological test (one that would pass with the implementation deleted) and a
# test written after the fact to describe code that already worked.
#
# Every changed test is checked ON ITS OWN, in two phases: it must PASS at HEAD,
# then FAIL with the implementation reverted. Both halves are load-bearing.
# Checking a whole set in one command and asking only "did the command go red"
# is not the same rule: one genuinely-red test makes the whole invocation red
# and hides every tautology beside it. And without the HEAD phase, a test that
# was never going to run — or a package that was never going to build in the
# scratch worktree — reports as "red" for a reason that has nothing to do with
# the revert, which is a pass the gate has not earned.
#
# Deliberately NOT part of `make check`: it needs the base branch, a scratch
# worktree, and a second test run, which is a pull-request-shaped cost rather
# than an every-turn one. Same reasoning that keeps mutation testing (D2) and
# the agmsg contract out of the local gate.
#
# Usage:
#   make efficacy                       # against origin/main
#   EFFICACY_BASE=origin/main make efficacy
#   scripts/efficacy.sh --changed-tests # print what it would check, and stop
#
# Exit codes: 0 = the gate passed, or had nothing to check; 1 = a changed test
# survived its implementation being reverted, or the gate could not run.

set -u

base=${EFFICACY_BASE:-origin/main}
repo_root=$(git rev-parse --show-toplevel)
mode=${1:-run}

# "Could not check" is a failure, never a skip. A required check that goes green
# when it checked nothing is worse than no check at all: it reports a property
# nobody is measuring any more, and it does it silently. The deliberate skips
# further down ("no implementation changed", "no test changed") are the opposite
# case — there the gate has genuinely nothing to say.
if ! git rev-parse --verify --quiet "$base" > /dev/null; then
	echo "efficacy: ERROR — base ref '$base' does not exist."
	echo "  Without it there is no diff to take, so the gate cannot run at all."
	echo "  This exits 1 rather than skipping: see Principle 4 in"
	echo "  docs/quality-gateway.md. In CI this usually means the checkout lost"
	echo "  'fetch-depth: 0'. Locally, 'git fetch origin main' or set"
	echo "  EFFICACY_BASE to a ref you have."
	exit 1
fi

merge_base=$(git merge-base "$base" HEAD)
changed=$(git diff --name-only "$merge_base" HEAD)

go_tests=""
go_impl=""
fe_tests=""
fe_impl=""

for f in $changed; do
	case "$f" in
	*_test.go) go_tests="$go_tests $f" ;;
	*.go) go_impl="$go_impl $f" ;;
	frontend/*.test.ts | frontend/*.test.tsx) fe_tests="$fe_tests $f" ;;
	frontend/src/*.ts | frontend/src/*.tsx) fe_impl="$fe_impl $f" ;;
	esac
done

tmp=$(mktemp -d)
worktree=""
cleanup() {
	[ -n "$worktree" ] && git -C "$repo_root" worktree remove --force "$worktree" > /dev/null 2>&1
	rm -rf "$tmp"
}
trap cleanup EXIT

tab=$(printf '\t')

# touched_lines <file> — the line numbers this branch added or modified in
# <file>, one per line, against the file as it stands now. -U0 keeps the hunk
# headers exact so no untouched neighbour is swept in.
touched_lines() {
	git diff -U0 "$merge_base" HEAD -- "$1" |
		awk '
			/^@@/ {
				# @@ -a,b +c,d @@ — take c and d.
				plus = $3
				sub(/^\+/, "", plus)
				n = split(plus, parts, ",")
				start = parts[1] + 0
				count = (n > 1) ? parts[2] + 0 : 1
				for (i = 0; i < count; i++) print start + i
			}
		'
}

# changed_test_funcs <file> — the Go test functions this branch added or
# modified in <file>. A function counts as changed when any line the diff
# touched falls inside it, so editing an assertion counts, not only adding a
# whole function. Reverting the implementation under a test that was merely
# renamed proves nothing, which is why the mapping is by line and not by
# "+func Test".
#
# Benchmarks are deliberately excluded. `-run` does not select them, and a
# benchmark asserts nothing about behaviour, so "must go red when the
# implementation is reverted" is not a rule it can meaningfully be held to.
changed_test_funcs() {
	file=$1

	touched_lines "$file" > "$tmp/touched"
	[ -s "$tmp/touched" ] || return 0

	# Every top-level test function in the file as it stands now, with the
	# line range it spans. Top-level `func` at column 0 is the boundary; Go
	# gofmt guarantees that shape, and gofmt is already gate G1.
	#
	# The blank line and the doc comment BETWEEN two functions belong to the
	# one below, not the one above. Getting that wrong is not cosmetic: an
	# append at the end of a file touches the blank line before the new
	# function, which would otherwise pull the previous, unchanged test into
	# scope — and an unrelated test is exactly what this gate must not spend
	# its verdict on.
	awk '
		{ line[NR] = $0 }
		END {
			n = NR
			cnt = 0
			for (i = 1; i <= n; i++) {
				if (line[i] !~ /^func /) continue
				cnt++
				fstart[cnt] = i
				fname[cnt] = ""
				if (line[i] ~ /^func (Test|Fuzz|Example)[A-Za-z0-9_]*\(/) {
					rest = substr(line[i], 6)
					p = index(rest, "(")
					fname[cnt] = substr(rest, 1, p - 1)
				}
			}
			for (k = 1; k <= cnt; k++) {
				if (fname[k] == "") continue
				s = fstart[k]
				j = s - 1
				while (j >= 1 && line[j] ~ /^\/\//) { s = j; j-- }
				e = (k < cnt) ? fstart[k + 1] - 1 : n
				while (e > s && (line[e] ~ /^[ \t]*$/ || line[e] ~ /^\/\//)) e--
				print fname[k], s, e
			}
		}
	' "$file" > "$tmp/funcs"

	awk 'NR == FNR { touched[$1] = 1; next }
		{
			for (i = $2; i <= $3; i++) if (touched[i]) { print $1; break }
		}' "$tmp/touched" "$tmp/funcs"
}

# changed_fe_test_names <file> — the vitest cases this branch added or modified
# in <file>, one name per line, mapped by touched line exactly as the Go half
# is. Prints the single token __UNMAPPED__ when it cannot narrow safely — no
# `it(...)`/`test(...)` declaration in the file at all, no touched line inside
# any case, or a name that cannot be read literally (a template literal,
# `it.each`, a computed name). The caller then runs the whole file, which is
# over-wide but never wrong.
#
# Touched lines that fall outside every case but beside at least one that was
# hit are ignored rather than treated as unmappable. They are imports, describe
# scaffolding and shared helpers, and adding a case almost always edits the
# import line above it — treating that as unmappable would send every ordinary
# frontend change straight back to whole-file scope.
changed_fe_test_names() {
	file=$1

	touched_lines "$file" > "$tmp/fe_touched"
	[ -s "$tmp/fe_touched" ] || return 0

	awk '
		function qname(s,   p, rest, ch, e, out) {
			p = index(s, "(")
			if (p == 0) return ""
			rest = substr(s, p + 1)
			sub(/^[ \t]*/, "", rest)
			ch = substr(rest, 1, 1)
			if (ch != "\"" && ch != "'"'"'") return ""
			rest = substr(rest, 2)
			e = index(rest, ch)
			if (e == 0) return ""
			out = substr(rest, 1, e - 1)
			if (out == "") return ""
			return out
		}
		NR == FNR { touched[$1 + 0] = 1; next }
		{ line[FNR] = $0 }
		END {
			n = FNR
			cnt = 0
			for (i = 1; i <= n; i++) {
				if (line[i] !~ /^[ \t]*(it|test)(\.[A-Za-z]+)?[ \t]*\(/) continue
				cnt++
				dstart[cnt] = i
				dname[cnt] = qname(line[i])
			}
			if (cnt == 0) { print "__UNMAPPED__"; exit }

			for (k = 1; k <= cnt; k++) {
				s = dstart[k]
				j = s - 1
				while (j >= 1 && line[j] ~ /^[ \t]*\/\//) { s = j; j-- }
				bstart[k] = s
				e = (k < cnt) ? dstart[k + 1] - 1 : n
				while (e > s && (line[e] ~ /^[ \t]*$/ || line[e] ~ /^[ \t]*\/\//)) e--
				bend[k] = e
			}

			any = 0
			for (i = 1; i <= n; i++) {
				if (!touched[i]) continue
				for (k = 1; k <= cnt; k++) {
					if (i >= bstart[k] && i <= bend[k]) { hit[k] = 1; any = 1; break }
				}
			}
			if (!any) { print "__UNMAPPED__"; exit }
			for (k = 1; k <= cnt; k++) {
				if (!hit[k]) continue
				if (dname[k] == "") { print "__UNMAPPED__"; exit }
				print dname[k]
			}
		}
	' "$tmp/fe_touched" "$file"
}

# ── What this branch changed, as a list of individually-checkable targets ─────

: > "$tmp/go_targets"
for f in $go_tests; do
	[ -f "$f" ] || continue
	d=$(dirname "$f")
	for n in $(changed_test_funcs "$f"); do
		printf './%s%s%s\n' "$d" "$tab" "$n" >> "$tmp/go_targets"
	done
done
sort -u "$tmp/go_targets" -o "$tmp/go_targets"

: > "$tmp/fe_targets"
for f in $fe_tests; do
	[ -f "$f" ] || continue
	rel=${f#frontend/}
	extracted=$(changed_fe_test_names "$f")
	case "$extracted" in
	"" | *__UNMAPPED__*) printf '%s%s\n' "$rel" "$tab" >> "$tmp/fe_targets" ;;
	*)
		printf '%s\n' "$extracted" | while IFS= read -r n; do
			[ -n "$n" ] || continue
			printf '%s%s%s\n' "$rel" "$tab" "$n" >> "$tmp/fe_targets"
		done
		;;
	esac
done
sort -u "$tmp/fe_targets" -o "$tmp/fe_targets"

go_count=$(wc -l < "$tmp/go_targets" | tr -d ' ')
fe_count=$(wc -l < "$tmp/fe_targets" | tr -d ' ')

if [ "$mode" = "--changed-tests" ]; then
	echo "base:            $base ($merge_base)"
	echo "go impl:        $go_impl"
	echo "go tests:       $go_tests"
	echo "go test funcs:  $(cut -f2 "$tmp/go_targets" | sort -u | tr '\n' ' ' | sed 's/ *$//')"
	echo "go packages:    $(cut -f1 "$tmp/go_targets" | sort -u | tr '\n' ' ' | sed 's/ *$//')"
	echo "frontend impl:  $fe_impl"
	echo "frontend tests: $fe_tests"
	echo "frontend cases:"
	sed 's/^/  /; s/'"$tab"'$/'"$tab"'(whole file)/' "$tmp/fe_targets"
	exit 0
fi

if [ "${EFFICACY_EXEMPT:-}" = "1" ]; then
	echo "efficacy: skipped — EFFICACY_EXEMPT=1."
	echo "  Use this only for a change whose tests are not expected to go red without it:"
	echo "  a pure refactor, or a test-only rename. Say which in the pull request."
	exit 0
fi

# Each stack is judged only against its own reverted implementation. A branch
# that changes Go code and also touches a frontend test has nothing to revert
# under that frontend test, so failing it there would be a verdict on a mutation
# that never happened.
[ -n "$go_impl" ] || go_count=0
[ -n "$fe_impl" ] || fe_count=0

if [ -z "$go_impl" ] && [ -z "$fe_impl" ]; then
	echo "efficacy: no implementation changed against $base — nothing to revert. Skipping."
	exit 0
fi

if [ "$go_count" -eq 0 ] && [ "$fe_count" -eq 0 ]; then
	echo "efficacy: WARNING — this branch changes implementation but no test that"
	echo "  can be red-checked against it."
	[ -n "$go_impl" ] && [ -s "$tmp/fe_targets" ] &&
		echo "  (The frontend tests it changes are not covered: no frontend implementation changed.)"
	[ -n "$fe_impl" ] && [ -s "$tmp/go_targets" ] &&
		echo "  (The Go tests it changes are not covered: no Go implementation changed.)"
	echo "  There is nothing to red-check. DEVELOPMENT.md requires tests first;"
	echo "  this gate can only check the result, and here there is no result to check."
	exit 0
fi

# ── A scratch worktree at HEAD ───────────────────────────────────────────────
#
# The real checkout is never touched: this runs on a pull request, where a
# half-reverted working tree would be a nasty thing to leave behind on a
# failure.
worktree=$(mktemp -d)
rm -rf "$worktree"
if ! git worktree add --detach "$worktree" HEAD > /dev/null 2>&1; then
	worktree=""
	echo "efficacy: ERROR — could not create a scratch worktree, so the gate could"
	echo "  not run. Exiting 1 rather than skipping, for the same reason as the"
	echo "  missing-base-ref case above."
	exit 1
fi

# main.go embeds frontend/dist, which is gitignored and therefore absent from a
# fresh worktree checkout. Without this placeholder the root package fails to
# build before a single file has been reverted, and every root-package test
# would "go red" for a reason that has nothing to do with the revert. Phase 1
# below would catch that as an error rather than let it pass as a red, but a
# placeholder is the actual fix — the gate is about behaviour, and no test here
# reads the embedded assets.
#
# The placeholder has to be a real file with a real name: `go:embed` on a
# directory skips dotfiles, so an empty dir or a lone `.keep` still fails with
# "contains no embeddable files".
mkdir -p "$worktree/frontend/dist"
if [ -z "$(ls -A "$worktree/frontend/dist" 2> /dev/null)" ]; then
	printf '<!doctype html>\n' > "$worktree/frontend/dist/index.html"
fi

if [ "$fe_count" -gt 0 ]; then
	if [ -d "$repo_root/frontend/node_modules" ]; then
		ln -s "$repo_root/frontend/node_modules" "$worktree/frontend/node_modules" 2> /dev/null
	else
		echo "efficacy: ERROR — frontend/node_modules is missing, so the changed"
		echo "  frontend tests cannot be run and this branch's frontend half cannot"
		echo "  be red-checked. Run 'make install-deps' (or 'npm ci' in frontend/)."
		exit 1
	fi
fi

# ── Phase 1: at HEAD, unmodified ─────────────────────────────────────────────

status=0
unchecked=0
run=0

# go_verdict <pkg> <func> <log> — pass | fail | skip | notrun | broken.
# "notrun" and "broken" are distinct on purpose: `go test` exits 0 when -run
# selects nothing, so exit status alone cannot tell "it passed" from "it was
# never there".
go_verdict() {
	if (cd "$worktree" && go test -v -count=1 "$1" -run "^$2\$") > "$3" 2>&1; then
		gv_rc=0
	else
		gv_rc=1
	fi
	if grep -q "^--- PASS: $2 " "$3"; then echo pass
	elif grep -q "^--- FAIL: $2 " "$3"; then echo fail
	elif grep -q "^--- SKIP: $2 " "$3"; then echo skip
	elif [ "$gv_rc" -ne 0 ]; then echo broken
	else echo notrun
	fi
}

# fe_verdict <relfile> <name|""> <log> — pass | fail | notrun | broken.
fe_verdict() {
	if [ -n "$2" ]; then
		fv_pat=$(printf '%s' "$2" | sed 's/[][\\.*+?^$(){}|\/]/\\&/g')
		(cd "$worktree/frontend" && NO_COLOR=1 npx --no-install vitest run "$1" -t "$fv_pat") > "$3" 2>&1
	else
		(cd "$worktree/frontend" && NO_COLOR=1 npx --no-install vitest run "$1") > "$3" 2>&1
	fi
	fv_rc=$?
	fv_line=$(sed -n 's/^ *Tests  //p' "$3" | tail -n 1)
	case "$fv_line" in
	*failed*) echo fail ;;
	*passed*) echo pass ;;
	"") [ "$fv_rc" -ne 0 ] && echo broken || echo notrun ;;
	*) echo notrun ;;
	esac
}

: > "$tmp/checkable_go"
: > "$tmp/checkable_fe"

if [ "$go_count" -gt 0 ] || [ "$fe_count" -gt 0 ]; then
	echo "efficacy: phase 1 — confirming each changed test passes at HEAD"
fi

if [ "$go_count" -gt 0 ]; then
	while IFS="$tab" read -r pkg fn; do
		[ -n "$fn" ] || continue
		case $(go_verdict "$pkg" "$fn" "$tmp/base.log") in
		pass)
			printf '%s%s%s\n' "$pkg" "$tab" "$fn" >> "$tmp/checkable_go"
			;;
		fail)
			echo "  ERROR: $pkg $fn already fails at HEAD."
			echo "         The gate cannot tell a test that reverting broke from one"
			echo "         that was broken to begin with. Fix the branch first."
			sed 's/^/         /' "$tmp/base.log"
			status=1
			;;
		skip)
			echo "  skipped at HEAD, so not red-checked: $pkg $fn"
			unchecked=$((unchecked + 1))
			;;
		notrun)
			echo "  did not run at HEAD, so not red-checked: $pkg $fn"
			echo "    (a build tag, or an Example with no '// Output:' comment)"
			unchecked=$((unchecked + 1))
			;;
		broken)
			echo "  ERROR: $pkg does not build at HEAD in a scratch worktree,"
			echo "         with nothing reverted. Whatever $fn does after the revert,"
			echo "         it would not be evidence about the revert."
			sed 's/^/         /' "$tmp/base.log"
			status=1
			;;
		esac
	done < "$tmp/go_targets"
fi

if [ "$fe_count" -gt 0 ]; then
	while IFS="$tab" read -r fefile fename; do
		[ -n "$fefile" ] || continue
		label="$fefile${fename:+ -t \"$fename\"}"
		case $(fe_verdict "$fefile" "$fename" "$tmp/base.log") in
		pass)
			printf '%s%s%s\n' "$fefile" "$tab" "$fename" >> "$tmp/checkable_fe"
			;;
		fail)
			echo "  ERROR: $label already fails at HEAD. Fix the branch first."
			sed 's/^/         /' "$tmp/base.log"
			status=1
			;;
		notrun)
			echo "  matched no test at HEAD, so not red-checked: $label"
			unchecked=$((unchecked + 1))
			;;
		broken)
			echo "  ERROR: $label could not be run at HEAD."
			sed 's/^/         /' "$tmp/base.log"
			status=1
			;;
		esac
	done < "$tmp/fe_targets"
fi

if [ "$status" -ne 0 ]; then
	echo
	echo "efficacy: the branch is not in a state this gate can judge. See above."
	exit 1
fi

# ── Phase 2: the same tests, with the implementation reverted ────────────────

revert() {
	for f in $1; do
		if git -C "$worktree" cat-file -e "$merge_base:$f" 2> /dev/null; then
			git -C "$worktree" checkout "$merge_base" -- "$f"
		else
			# Added by this branch: reverting means it never existed.
			rm -f "$worktree/$f"
		fi
	done
}

[ "$go_count" -gt 0 ] && revert "$go_impl"
[ "$fe_count" -gt 0 ] && revert "$fe_impl"

echo "efficacy: phase 2 — the same tests, with this branch's implementation reverted"

survivors=0

while IFS="$tab" read -r pkg fn; do
	[ -n "$fn" ] || continue
	run=$((run + 1))
	case $(go_verdict "$pkg" "$fn" "$tmp/rev.log") in
	fail | broken)
		echo "  red: $pkg $fn"
		;;
	pass)
		echo
		echo "SURVIVOR: $pkg $fn still passes with this branch's implementation reverted."
		echo "      A test that passes without the code it covers protects nothing."
		echo "      Either it asserts something the old implementation already did,"
		echo "      or it asserts nothing at all. See decision D4 in docs/quality-gateway.md."
		echo
		sed 's/^/      /' "$tmp/rev.log"
		survivors=$((survivors + 1))
		;;
	*)
		echo
		echo "SURVIVOR: $pkg $fn ran at HEAD but selected nothing after the revert,"
		echo "      so nothing about it went red."
		echo
		sed 's/^/      /' "$tmp/rev.log"
		survivors=$((survivors + 1))
		;;
	esac
done < "$tmp/checkable_go"

while IFS="$tab" read -r fefile fename; do
	[ -n "$fefile" ] || continue
	run=$((run + 1))
	label="$fefile${fename:+ -t \"$fename\"}"
	case $(fe_verdict "$fefile" "$fename" "$tmp/rev.log") in
	fail | broken)
		echo "  red: $label"
		;;
	*)
		echo
		echo "SURVIVOR: $label still passes with this branch's implementation reverted."
		echo
		sed 's/^/      /' "$tmp/rev.log"
		survivors=$((survivors + 1))
		;;
	esac
done < "$tmp/checkable_fe"

echo
if [ "$survivors" -gt 0 ]; then
	echo "efficacy: FAIL — $survivors of $run changed test(s) survived the revert."
	exit 1
fi

if [ "$run" -eq 0 ]; then
	echo "efficacy: nothing was red-checked — all $unchecked changed test(s) were"
	echo "  unrunnable here. See the reasons above."
	exit 0
fi

echo "efficacy: ok — all $run changed test(s) went red, as a test written before"
echo "  its implementation must."
[ "$unchecked" -gt 0 ] && echo "  ($unchecked could not be red-checked; see above.)"
exit 0
