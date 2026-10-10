#!/bin/sh
# Fixed verification entry points. No arbitrary command, shell, or make options.
set -eu
umask 077
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
fail() { echo "verify: $*" >&2; exit 1; }
valid_base() {
 case "$1" in ''|-*) return 1 ;; esac
 git rev-parse --verify --quiet "$1^{commit}" >/dev/null 2>&1 &&
 git merge-base "$1" HEAD >/dev/null 2>&1
}
if [ "$#" -eq 2 ] && [ "$1" = --set-base ]; then
 valid_base "$2" || fail 'base must name an existing commit with shared HEAD history; fetch separately if needed'
 mkdir -p .cache/verify
 stage=$(mktemp -d "$root/.cache/verify/base.XXXXXX") || exit 1
 trap 'rm -rf "$stage"' EXIT HUP INT TERM
 printf '%s\n' "$2" > "$stage/base-ref"
 mv "$stage/base-ref" .cache/verify/base-ref
 echo "verify: saved PR base $2 for this checkout"
 exit 0
fi
[ "$#" -eq 1 ] || { echo 'verify: expected one fixed target, or --set-base <ref>' >&2; exit 2; }
target=$1
case "$target" in
 install-deps|test-go|lint-go|check) ;;
 efficacy|coverage-blocks|mutation)
 saved=''
 if [ -f .cache/verify/base-ref ]; then saved=$(cat .cache/verify/base-ref); fi
 EFFICACY_BASE=${EFFICACY_BASE:-$saved}
 COVERAGE_BLOCKS_BASE=${COVERAGE_BLOCKS_BASE:-$saved}
 MUTATION_BASE=${MUTATION_BASE:-$saved}
 case "$target" in
 efficacy) base=$EFFICACY_BASE ;;
 coverage-blocks) base=$COVERAGE_BLOCKS_BASE ;;
 mutation) base=$MUTATION_BASE ;;
 esac
 valid_base "$base" || fail 'missing/invalid PR base; run sh scripts/verify.sh --set-base <PR-base-ref> once in this checkout'
 export EFFICACY_BASE COVERAGE_BLOCKS_BASE MUTATION_BASE
 echo "verify: $target base=$base commit=$(git rev-parse "$base^{commit}") merge-base=$(git merge-base "$base" HEAD)"
 ;;
 *) echo "verify: unsupported target: $target" >&2; exit 2 ;;
esac
# Only expendable build/npm caches move. Go's shared module/SDK cache and
# tool binaries retain their existing contract and may need initial approval.
GOCACHE="$root/.cache/go-build"
npm_config_cache="$root/.cache/npm"
export GOCACHE npm_config_cache
mkdir -p .cache/verify "$GOCACHE" "$npm_config_cache"
run=$(mktemp -d "$root/.cache/verify/$target.XXXXXX") || exit 1
log="$run/output.log"
echo "verify: running $target; log=$log"
status=0
make "$target" > "$log" 2>&1 || status=$?
echo "verify: $target exit=$status; log=$log"
if [ "$status" -ne 0 ]; then
 # Head/tail plus context around the first recognizable diagnostic.
 # Neither polling nor rereading the full log is needed to obtain completion.
 awk '
  { recent[NR % 6] = $0 }
  !found && /^[[:space:]]*(ERROR:|error:|Error:|fatal:|panic:|FAIL|--- FAIL|npm (ERR!|error))|^make.*Error [0-9]/ {
   print "verify: first diagnostic context"
   first = NR - 5; if (first < 1) first = 1
   for (i = first; i <= NR; i++) print recent[i % 6]
   found = 1; remaining = 10; next
  }
  found { print; if (--remaining == 0) exit }
 ' "$log"
 sed -n '1,40p' "$log"
 lines=$(wc -l < "$log")
 if [ "$lines" -gt 40 ]; then
  start=$((lines - 39))
  [ "$start" -gt 40 ] || start=41
  sed -n "${start},${lines}p" "$log"
 fi
fi
exit "$status"
