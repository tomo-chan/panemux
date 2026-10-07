#!/bin/sh
# Boots the command-center fixture (:4176) with a stub `claude` first on
# PATH and an isolated HOME.
#
# PATH rather than a config option: the Runner resolves its binary through
# PATH exactly as it does in production, so nothing about the shipped
# resolution path is bypassed for the test.
#
# HOME rather than the developer's own: the command center persists its
# session id and query history under ~/.config/panemux/command-center/, and
# a test run must not write into the real one. GOPATH/GOMODCACHE are pinned
# to their pre-override values first, or `go run` would re-download every
# module into the throwaway home.
set -eu

# Pin direct execution and every Go subprocess to this checkout's go.mod.
panemux_toolchain_root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
. "$panemux_toolchain_root/scripts/go-toolchain.sh"
panemux_go_toolchain "$panemux_toolchain_root" || exit 1

E2E_DIR="$(cd "$(dirname "$0")" && pwd)"

GOPATH="$(go env GOPATH)"
GOMODCACHE="$(go env GOMODCACHE)"
export GOPATH GOMODCACHE

E2E_HOME="${TMPDIR:-/tmp}/panemux-e2e-command-center-home"
rm -rf "$E2E_HOME"
mkdir -p "$E2E_HOME"
export HOME="$E2E_HOME"

PATH="$E2E_DIR/fixtures/claude-stub:$PATH"
export PATH

exec sh "$E2E_DIR/run-panemux-e2e.sh" command-center.yml 4176
