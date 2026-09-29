#!/bin/sh
# Boots a real panemux binary for the Playwright suite.
#
#   run-panemux-e2e.sh [fixture-config-name] [port]
#
# Both arguments are optional and default to the original single-server
# harness (workspace-switch.yml on :4173), so playwright.config.ts's other
# webServer entries only have to name what differs. The fixture config is
# copied out of the repository into a temp directory first: panemux rewrites
# its own config file on some API calls, and a tracked fixture must never be
# mutated by a test run.
set -eu

cd "$(dirname "$0")/../.."

E2E_CONFIG_NAME="${1:-workspace-switch.yml}"
E2E_PORT="${2:-4173}"

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

exec go run . --config "$E2E_CONFIG_PATH" --port "$E2E_PORT"
