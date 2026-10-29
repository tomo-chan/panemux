#!/bin/sh
# Boots a real panemux binary for the Playwright suite.
#
#   run-panemux-e2e.sh [fixture-config-name] [port] [build-tags]
#
# Every argument is optional. The first two default to the original
# single-server harness (workspace-switch.yml on :4173), so
# playwright.config.ts's other webServer entries only have to name what
# differs. build-tags, when given, are passed to `go run -tags`: the
# task-dashboard fixture names panemux_fixture_agent, which registers the
# fixture agent (issue #377). E2E_STAND_INS, a fixture's colon-separated
# directories of stand-ins for real CLIs, is put first on PATH right before
# `go run`: the runtime selection above puts the SDK's bin ahead of
# everything, and panemux must find the stand-ins, never a real CLI there. The fixture config is
# copied out of the repository into a temp directory first: panemux rewrites
# its own config file on some API calls, and a tracked fixture must never be
# mutated by a test run.
set -eu

# Pin direct execution and every Go subprocess to this checkout's go.mod.
panemux_toolchain_root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
panemux_runtime_scripts="$panemux_toolchain_root/scripts"
. "$panemux_runtime_scripts/runtime-env.sh"
panemux_runtime "$panemux_toolchain_root" || exit 1

cd "$(dirname "$0")/../.."

E2E_CONFIG_NAME="${1:-workspace-switch.yml}"
E2E_PORT="${2:-4173}"
E2E_TAGS="${3:-}"

export GOCACHE="${TMPDIR:-/tmp}/panemux-playwright-gocache"
mkdir -p "$GOCACHE"
E2E_CONFIG_DIR="${TMPDIR:-/tmp}/panemux-playwright-config"
mkdir -p "$E2E_CONFIG_DIR"
E2E_CONFIG_PATH="$E2E_CONFIG_DIR/$E2E_CONFIG_NAME"
cp "frontend/e2e/$E2E_CONFIG_NAME" "$E2E_CONFIG_PATH"

# Every webServer entry runs this script concurrently, and they would
# otherwise all build into the same frontend/dist directory at once — which
# `go run .` then embeds, possibly mid-write. mkdir is atomic on every POSIX
# filesystem, so it is the portable lock primitive here (flock is not
# available everywhere this suite runs).
E2E_BUILD_LOCK="${TMPDIR:-/tmp}/panemux-playwright-build.lock"
while ! mkdir "$E2E_BUILD_LOCK" 2>/dev/null; do
    sleep 1
done
trap 'rmdir "$E2E_BUILD_LOCK" 2>/dev/null || true' EXIT INT TERM
make build-frontend >/dev/null
rmdir "$E2E_BUILD_LOCK"
trap - EXIT INT TERM

E2E_OWNED_HOME=""

# Board fixtures must not discover the developer's real agmsg installation.
case "$E2E_CONFIG_NAME" in
    agent-board*.yml|command-center.yml)
        export GOPATH="$(go env GOPATH)"
        export GOMODCACHE="$(go env GOMODCACHE)"
        if [ -z "${E2E_HOME:-}" ]; then
            E2E_HOME=$(mktemp -d "${TMPDIR:-/tmp}/panemux-e2e-home.XXXXXX") || exit 1
            E2E_OWNED_HOME="$E2E_HOME"
            trap 'find "$E2E_OWNED_HOME" -depth -delete' EXIT
            trap 'exit 130' INT
            trap 'exit 143' TERM
        fi
        export HOME="$E2E_HOME"
        unset XDG_CONFIG_HOME XDG_DATA_HOME XDG_CACHE_HOME GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM
        ;;
esac

. frontend/e2e/stand-ins.sh
e2e_stand_ins_first "${E2E_STAND_INS:-}" || exit 1
# Keep the owning shell alive so EXIT cleans its HOME after the child exits.
if [ -n "$E2E_OWNED_HOME" ]; then
    go run ${E2E_TAGS:+-tags "$E2E_TAGS"} . --config "$E2E_CONFIG_PATH" --port "$E2E_PORT"
    exit $?
fi
exec go run ${E2E_TAGS:+-tags "$E2E_TAGS"} . --config "$E2E_CONFIG_PATH" --port "$E2E_PORT"
