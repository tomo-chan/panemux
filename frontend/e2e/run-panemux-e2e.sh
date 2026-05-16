#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."

export GOCACHE="${TMPDIR:-/tmp}/panemux-playwright-gocache"
mkdir -p "$GOCACHE"
E2E_CONFIG_DIR="${TMPDIR:-/tmp}/panemux-playwright-config"
mkdir -p "$E2E_CONFIG_DIR"
E2E_CONFIG_PATH="$E2E_CONFIG_DIR/workspace-switch.yml"
cp frontend/e2e/workspace-switch.yml "$E2E_CONFIG_PATH"

make build-frontend >/dev/null
exec go run . --config "$E2E_CONFIG_PATH" --port 4173
