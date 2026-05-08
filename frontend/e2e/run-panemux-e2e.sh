#!/bin/sh
set -eu

cd "$(dirname "$0")/../.."

export GOCACHE="${TMPDIR:-/tmp}/panemux-playwright-gocache"
mkdir -p "$GOCACHE"

make build-frontend >/dev/null
exec go run . --config frontend/e2e/workspace-switch.yml --port 4173
