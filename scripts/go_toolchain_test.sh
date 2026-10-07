#!/bin/sh
set -eu
scripts_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir -p "$work/bin" "$work/sdk/bin" "$work/repo/scripts"
cp "$scripts_dir/../Makefile" "$work/repo/Makefile"
cp "$scripts_dir/go-toolchain.sh" "$scripts_dir/go-shell.sh" "$work/repo/scripts/"
printf 'module sample\n\ngo 1.25.0\n' > "$work/repo/go.mod"
cat > "$work/bin/go" <<'MOCK'
#!/bin/sh
printf '%s %s\n' "$GOTOOLCHAIN" "$*" >> "$TRACE"
[ "${FAIL_GO:-0}" = 0 ] || exit 7
[ "$GOTOOLCHAIN" = go1.25.0 ] || exit 8
case "$*" in
'env GOVERSION GOROOT') printf '%s\n%s\n' "${ACTUAL_VERSION:-go1.25.0}" "$SDK" ;;
'env GOBIN') echo /tmp/sample-tools ;;
*) echo bootstrap-used ;;
esac
MOCK
cat > "$work/sdk/bin/go" <<'MOCK'
#!/bin/sh
printf 'selected %s %s\n' "$GOTOOLCHAIN" "$*" >> "$TRACE"
printf '%s\n' "$GOTOOLCHAIN"
MOCK
chmod +x "$work/bin/go" "$work/sdk/bin/go"
export SDK="$work/sdk" TRACE="$work/trace"
export PATH="$work/bin:$PATH"
run_env() {
 sh -c '. "$1"; panemux_go_toolchain "$2" || exit; go version; sh -c "go version"' sh "$scripts_dir/go-toolchain.sh" "$work/repo"
}
for inherited in auto local go1.26.1; do
 export GOTOOLCHAIN=$inherited
 output=$(run_env)
 [ "$output" = "$(printf 'go1.25.0\ngo1.25.0')" ] || exit 1
 echo "ok inherited $inherited selects exact version for parent and child"
done
printf '\nprobe:\n\t@go version\n' >> "$work/repo/Makefile"
output=$(GOTOOLCHAIN=local make -s -C "$work/repo" SHELL=/bin/sh probe)
[ "$output" = go1.25.0 ]
grep -q 'go1.25.0 env GOBIN' "$TRACE"
echo 'ok make initialization and recipes select exact version'
for mode in failed wrong invalid missing; do
 export FAIL_GO=0 ACTUAL_VERSION=go1.25.0
 printf 'module sample\n\ngo 1.25.0\n' > "$work/repo/go.mod"
 case "$mode" in
 failed) export FAIL_GO=1 ;;
 wrong) export ACTUAL_VERSION=go1.26.1 ;;
 invalid) printf 'module sample\n\ngo 1.25;touch /tmp/sample-unwanted\n' > "$work/repo/go.mod" ;;
 missing) printf 'module sample\n' > "$work/repo/go.mod" ;;
 esac
 if run_env > "$work/output" 2>&1; then echo "FAIL $mode passed"; exit 1; fi
 if make -s -C "$work/repo" probe > "$work/output" 2>&1; then echo "FAIL make $mode passed"; exit 1; fi
 echo "ok $mode fails closed"
done

# Direct entrypoints must stop before checking, building, or launching anything.
export FAIL_GO=1
for entry in scripts/efficacy.sh scripts/mutation.sh .claude/hooks/post-edit-check.sh .claude/hooks/stop-check.sh frontend/e2e/run-panemux-e2e.sh frontend/e2e/run-panemux-command-center-e2e.sh frontend/e2e/run-panemux-task-dashboard-e2e.sh frontend/screenshots/run-panemux-screenshots.sh; do
 status=0
 sh "$scripts_dir/../$entry" > "$work/output" 2>&1 || status=$?
 case "$entry" in .claude/hooks/*) expected=2 ;; *) expected=1 ;; esac
 [ "$status" -eq "$expected" ] || { echo "FAIL $entry returned $status, expected $expected"; exit 1; }
 grep -q 'go-toolchain: cannot start required' "$work/output"
 echo "ok direct $entry fails before side effects"
done
