#!/bin/sh
# Stub stand-in for agmsg's own scripts/api.sh, used only by panemux's e2e
# harness. agmsg (github.com/fujibee/agmsg) is a separate, operator-installed
# tool that panemux never installs itself (see docs/security.md), so a
# machine running this suite has no real installation to read from. This stub
# implements just the one call internal/board's LocalAgmsgClient makes:
#
#   api.sh get teams <team> messages --limit <n>
#
# It prints the JSONL message store send.sh appends to. Everything below the
# script boundary — exec, argv construction, JSONL parsing, cursor filtering,
# the relay, the cache, the HTTP API and the dashboard — is the real
# implementation, not a stub.
#
# The verb/limit arguments are deliberately ignored: the store is seeded and
# written only by this harness, and always stays far below any limit the
# relay asks for.
set -eu

STORE="$(dirname "$0")/../messages.jsonl"
[ -f "$STORE" ] || exit 0
cat "$STORE"
