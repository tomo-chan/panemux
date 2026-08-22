#!/bin/sh
# Stub stand-in for agmsg's own scripts/send.sh — see api.sh in this
# directory for why the harness ships a stub at all. Mirrors the real
# script's argument shape, which internal/board's LocalAgmsgClient depends
# on:
#
#   send.sh <team> <from> <to> <body> [--force]
#
# It appends one message_sent JSONL record to the same store api.sh reads.
# IDs come from a counter file seeded with the install-time epoch second, so
# a later run's IDs always sort after an earlier run's — panemux persists its
# relay cursor across restarts (~/.config/panemux/board-cursors.json), and a
# store that restarted its IDs at 1 would be filtered out entirely as
# already-seen.
set -eu

team="$1"
from="$2"
to="$3"
body="$4"

STORE_DIR="$(dirname "$0")/.."
STORE="$STORE_DIR/messages.jsonl"
ID_FILE="$STORE_DIR/next_id"

id="$(cat "$ID_FILE")"
echo "$((id + 1))" >"$ID_FILE"

# Only the two characters that can break a JSON string literal are escaped.
# That is enough for this harness's own bodies (plain text and the status
# self-report JSON payload); it is not a general-purpose JSON encoder.
escaped_body="$(printf '%s' "$body" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g')"

printf '{"type":"message_sent","id":"%s","team":"%s","from":"%s","to":"%s","body":"%s","at":"%s"}\n' \
    "$id" "$team" "$from" "$to" "$escaped_body" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$STORE"
