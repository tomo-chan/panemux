#!/usr/bin/env bash
# Regenerates this directory's fixtures from a REAL agmsg install, so their
# provenance is reproducible rather than asserted. See README.md.
#
#   internal/board/testdata/agmsg-v1.2.0/capture.sh <agmsg-source-checkout>
#
# The argument is a checkout of github.com/fujibee/agmsg at the tag matching
# this directory's name. The install goes into a throwaway HOME, never the
# caller's own ~/.agents.
#
# Every value written here is a placeholder chosen for the fixture
# (/tmp/sample-project, team "panemux", pane-a/pane-b) — DEVELOPMENT.md's
# path-sanitization rule forbids capturing a real working directory.
set -euo pipefail

src="${1:?usage: capture.sh <agmsg-source-checkout>}"
out="$(cd "$(dirname "$0")" && pwd)"
tag="$(basename "$out" | sed 's/^agmsg-v/v/')"

command -v sqlite3 >/dev/null || { echo "sqlite3 is required" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export HOME="$work/home"
mkdir -p "$HOME" "$work/sample-project"

git -C "$src" checkout --quiet "$tag"
(cd "$src" && ./install.sh --cmd agmsg >/dev/null)

s="$HOME/.agents/skills/agmsg/scripts"
project=/tmp/sample-project
mkdir -p "$project"

"$s/join.sh" panemux pane-a claude-code "$project" --force >/dev/null
"$s/join.sh" panemux pane-b claude-code "$project" --force >/dev/null

# The four rows Tier 1 asserts on, in the order they must appear: an
# ordinary message, a board_status report, a body that is valid JSON with a
# "state" field but no "kind" discriminator, and another ordinary message.
"$s/send.sh" panemux pane-a pane-b "please review my latest commit" --force >/dev/null
"$s/send.sh" panemux pane-a _system '{"kind":"board_status","state":"working","cwd":"/tmp/sample-project","branch":"feature/x","repo":"owner/repo","pr_url":"https://github.com/owner/repo/pull/123","last_tool":"Edit internal/api/handler.go","summary":"fixing failing tests"}' --force >/dev/null
"$s/send.sh" panemux pane-b _system '{"state":"looks like status but has no kind field"}' --force >/dev/null
"$s/send.sh" panemux pane-b pane-a "lgtm, merging now" --force >/dev/null

"$s/api.sh" get teams                      > "$out/get_teams.jsonl"
"$s/api.sh" get teams panemux members      > "$out/get_team_members.jsonl"
"$s/api.sh" get teams panemux messages --limit 100 > "$out/get_team_messages.jsonl"

"$s/version.sh" > "$out/VERSION.txt" 2>/dev/null || cat "$HOME/.agents/skills/agmsg/VERSION" > "$out/VERSION.txt"

echo "captured $tag into $out"
