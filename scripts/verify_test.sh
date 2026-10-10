#!/bin/sh
set -eu
unset EFFICACY_BASE MUTATION_BASE COVERAGE_BLOCKS_BASE
here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-verify-test.XXXXXX") || exit 1
work=$(CDPATH='' cd -- "$work" && pwd)
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir -p "$work/repo/scripts" "$work/mock"
cp "$here/verify.sh" "$work/repo/scripts/verify.sh"
git -C "$work/repo" init -qb main
git -C "$work/repo" -c user.name=Demo -c user.email=demo@example.com commit --allow-empty -qm initial
git -C "$work/repo" branch base
git -C "$work/repo" -c user.name=Demo -c user.email=demo@example.com commit --allow-empty -qm feature
cat > "$work/mock/make" <<'MOCK'
#!/bin/sh
printf '%s\n' "$PWD|$*|${GOCACHE:-}|${npm_config_cache:-}|${EFFICACY_BASE:-}|${MUTATION_BASE:-}|${COVERAGE_BLOCKS_BASE:-}|${TMPDIR:-}" >> "$TRACE"
seq 1 150
echo 'ERROR: first actual error' >&2
seq 151 300
echo 'last output'
exit "${MOCK_STATUS:-0}"
MOCK
chmod +x "$work/mock/make"
export PATH="$work/mock:$PATH" TRACE="$work/trace"
run() { sh "$work/repo/scripts/verify.sh" "$@"; }
expect_status() {
 expected=$1; shift
 status=0; run "$@" > "$work/output" 2>&1 || status=$?
 [ "$status" -eq "$expected" ] || { cat "$work/output"; echo "FAIL expected $expected, got $status"; exit 1; }
}
for target in install-deps test-go lint-go check; do
 expect_status 0 "$target"
 grep -q "verify: $target exit=0" "$work/output"
 [ "$(wc -l < "$work/output")" -le 5 ]
done
[ "$(find "$work/repo/.cache/verify" -name output.log | wc -l)" -eq 4 ]
grep -q 'first actual error' "$work/repo/.cache/verify/"*/output.log
grep -q 'last output' "$work/repo/.cache/verify/"*/output.log
grep -q "$work/repo/.cache/go-build|$work/repo/.cache/npm" "$TRACE"
grep -Fq "|${TMPDIR:-}" "$TRACE"
echo 'ok fixed targets retain full stdout/stderr with bounded summaries and local caches'
export MOCK_STATUS=7
expect_status 7 check
grep -q 'first actual error' "$work/output"
[ "$(wc -l < "$work/output")" -le 105 ]
unset MOCK_STATUS
before=$(wc -l < "$TRACE")
expect_status 2
expect_status 2 --set-base
for args in unknown '--eval=bad' 'check extra' 'check;echo'; do
 # Intentional word splitting tests extra arguments.
 expect_status 2 $args
done
[ "$(wc -l < "$TRACE")" -eq "$before" ]
echo 'ok errors preserve exit status; unknown targets and extra operands never run make'
for target in efficacy coverage-blocks mutation; do expect_status 1 "$target"; done
expect_status 1 --set-base absent
expect_status 1 --set-base --help
expect_status 0 --set-base base
for target in efficacy coverage-blocks mutation; do expect_status 0 "$target"; done
grep -q '|base|base|base|' "$TRACE"
(EFFICACY_BASE=HEAD MUTATION_BASE=HEAD COVERAGE_BLOCKS_BASE=HEAD expect_status 0 mutation)
grep -q '|HEAD|HEAD|HEAD|' "$TRACE"
for gate in efficacy mutation coverage-blocks; do
 case "$gate" in
 efficacy) (EFFICACY_BASE=HEAD expect_status 0 efficacy); grep -q '|HEAD|base|base|' "$TRACE" ;;
 mutation) (MUTATION_BASE=HEAD expect_status 0 mutation); grep -q '|base|HEAD|base|' "$TRACE" ;;
 coverage-blocks) (COVERAGE_BLOCKS_BASE=HEAD expect_status 0 coverage-blocks); grep -q '|base|base|HEAD|' "$TRACE" ;;
 esac
done
# Ref content is an operand, never an executable expression.
expect_status 1 --set-base '$(touch injected)'
[ ! -e "$work/repo/injected" ]
blob=$(printf sample | git -C "$work/repo" hash-object -w --stdin)
expect_status 1 --set-base "$blob"
# A stacked PR uses its parent, not the root/default base.
git -C "$work/repo" branch parent
git -C "$work/repo" -c user.name=Demo -c user.email=demo@example.com commit --allow-empty -qm child
expect_status 0 --set-base parent
for target in efficacy coverage-blocks mutation; do expect_status 0 "$target"; done
grep -q '|parent|parent|parent|' "$TRACE"
parent_commit=$(git -C "$work/repo" rev-parse parent)
grep -q "base=parent commit=$parent_commit merge-base=$parent_commit" "$work/output"
expect_status 0 --set-base base
git -C "$work/repo" branch -D base >/dev/null
expect_status 1 mutation
expect_status 1 --set-base absent
[ "$(cat "$work/repo/.cache/verify/base-ref")" = base ]
echo 'ok explicit checkout base is required, overrides work, deleted refs fail closed'
git -C "$work/repo" checkout -q --orphan unrelated
git -C "$work/repo" -c user.name=Demo -c user.email=demo@example.com commit --allow-empty -qm unrelated
expect_status 1 --set-base main
# A second checkout must not reuse the first checkout base or logs.
git -C "$work/repo" worktree add -q --detach "$work/other" HEAD
mkdir -p "$work/other/scripts"
cp "$here/verify.sh" "$work/other/scripts/verify.sh"
if sh "$work/other/scripts/verify.sh" mutation > "$work/output" 2>&1; then echo 'FAIL worktree reused base'; exit 1; fi
sh "$work/other/scripts/verify.sh" check > "$work/output" 2>&1
grep -q "$work/other/.cache/go-build|$work/other/.cache/npm" "$TRACE"
echo 'ok unrelated history fails; worktrees isolate settings, logs and caches'
# A failed temporary allocation must never launch a check.
cat > "$work/mock/mktemp" <<'MOCK'
#!/bin/sh
exit 1
MOCK
chmod +x "$work/mock/mktemp"
before=$(wc -l < "$TRACE")
expect_status 1 check
[ "$(wc -l < "$TRACE")" -eq "$before" ]
echo 'ok temporary allocation failure stops before make'
