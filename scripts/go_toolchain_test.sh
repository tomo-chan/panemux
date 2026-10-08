#!/bin/sh
set -eu
scripts_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-go-toolchain-test.XXXXXX") || exit 1
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir -p "$work/bin" "$work/sdk/bin" "$work/repo/scripts"
cp "$scripts_dir/../Makefile" "$work/repo/Makefile"
cp "$scripts_dir/go-toolchain.sh" "$scripts_dir/go-shell.sh" "$scripts_dir/runtime-shell.sh" "$scripts_dir/runtime-env.sh" "$scripts_dir/node-toolchain.sh" "$work/repo/scripts/"
printf '24.21.0\n' > "$work/repo/.node-version"
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
cat > "$work/sdk/bin/node" <<'MOCK'
#!/bin/sh
case "$*" in --version) echo v24.21.0 ;; *) echo 11.19.0 ;; esac
MOCK
printf '#!/bin/sh\necho 11.19.0\n' > "$work/sdk/bin/npm"
cp "$work/sdk/bin/npm" "$work/sdk/bin/npx"
chmod +x "$work/sdk/bin/node" "$work/sdk/bin/npm" "$work/sdk/bin/npx"
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
mkdir -p "$work/sideeffects"
for command in rm mkdir cp chmod date; do
 printf '#!/bin/sh\necho side-effect >> "$SIDE_EFFECT_TRACE"\nexit 99\n' > "$work/sideeffects/$command"
 chmod +x "$work/sideeffects/$command"
done
export SIDE_EFFECT_TRACE="$work/sideeffects.trace"
for entry in scripts/efficacy.sh scripts/mutation.sh .claude/hooks/post-edit-check.sh .claude/hooks/stop-check.sh frontend/e2e/run-panemux-e2e.sh frontend/e2e/run-panemux-agent-board-e2e.sh frontend/e2e/run-panemux-command-center-e2e.sh frontend/e2e/run-panemux-task-dashboard-e2e.sh frontend/screenshots/run-panemux-screenshots.sh; do
 status=0
 PATH="$work/sideeffects:$PATH" sh "$scripts_dir/../$entry" > "$work/output" 2>&1 || status=$?
 case "$entry" in .claude/hooks/*) expected=2 ;; *) expected=1 ;; esac
 [ "$status" -eq "$expected" ] || { echo "FAIL $entry returned $status, expected $expected"; exit 1; }
 grep -q 'go-toolchain: cannot start required' "$work/output"
 echo "ok direct $entry fails before side effects"
done
[ ! -e "$SIDE_EFFECT_TRACE" ]

# The Stop retry guard must precede both acquisition and cached-SDK startup.
if command -v jq >/dev/null 2>&1; then
 mkdir -p "$work/repo/.claude/hooks"
 cp "$scripts_dir/../.claude/hooks/stop-check.sh" "$work/repo/.claude/hooks/"
 printf 'module sample\n\ngo 1.25.0\n' > "$work/repo/go.mod"
 case "$(uname -s)" in Darwin) os=darwin ;; Linux) os=linux ;; esac
 case "$(uname -m)" in arm64|aarch64) arch=arm64 ;; *) arch=x64 ;; esac
 cached="$work/repo/.cache/runtimes/node-v24.21.0-$os-$arch/bin"
 mkdir -p "$cached"
 printf '#!/bin/sh\necho node-probe >> "$TRACE"\necho v26.8.1\n' > "$cached/node"
 cp "$work/sdk/bin/npm" "$cached/npm"
 cp "$work/sdk/bin/npx" "$cached/npx"
 chmod +x "$cached/node"
 for failure in unavailable corrupt-cache; do
  case "$failure" in unavailable) export FAIL_GO=1 ;; *) export FAIL_GO=0 ACTUAL_VERSION=go1.25.0 ;; esac
  : > "$TRACE"
  status=0
  printf '{"stop_hook_active":true}' | "$work/repo/.claude/hooks/stop-check.sh" > "$work/output" 2>&1 || status=$?
  [ "$status" -eq 0 ] && [ ! -s "$TRACE" ] || { echo "FAIL retry $failure probed SDK or blocked"; exit 1; }
  status=0
  printf '{"stop_hook_active":false}' | "$work/repo/.claude/hooks/stop-check.sh" > "$work/output" 2>&1 || status=$?
  [ "$status" -eq 2 ] && [ -s "$TRACE" ] || { echo "FAIL initial $failure did not block"; exit 1; }
  echo "ok Stop retry skips $failure while first invocation blocks"
 done
fi
