#!/usr/bin/env bash
# Regenerates this directory's fixtures from a REAL agmsg install, so their
# provenance is reproducible rather than asserted. See README.md.
#
#   internal/agmsg/testdata/agmsg-v1.2.0/capture.sh <agmsg-source-checkout> <commit-sha>
#
# The arguments are an agmsg checkout and its verified commit SHA.
# No tag is checked out. The install goes into a throwaway HOME, never the
# caller's own ~/.agents.
#
# Every value written here is a placeholder chosen for the fixture
# (/tmp/sample-project, team "panemux", pane-a/pane-b) — DEVELOPMENT.md's
# path-sanitization rule forbids capturing a real working directory.
set -euo pipefail

src="${1:?usage: capture.sh <agmsg-source-checkout> <commit-sha>}"
sha="${2:?expected contract-tested commit SHA}"
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || exit 1
[ "$(git -C "$src" rev-parse HEAD)" = "$sha" ] || exit 1
out="${3:-$(cd "$(dirname "$0")" && pwd)}"
tag="$(basename "$out" | sed 's/^agmsg-v/v/')"

command -v sqlite3 >/dev/null || { echo "sqlite3 is required" >&2; exit 1; }

work="$(mktemp -d "${TMPDIR:-/tmp}/panemux-agmsg-capture.XXXXXX")" || exit 1
trap 'rm -r -- "$work"' EXIT
unset XDG_CONFIG_HOME XDG_DATA_HOME XDG_STATE_HOME XDG_CACHE_HOME
unset CLAUDE_CONFIG_DIR CODEX_HOME AGMSG_STORAGE_PATH
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export HOME="$work/home"
mkdir -p "$HOME" "$work/sample-project"

(cd "$src" && ./install.sh --cmd agmsg >/dev/null)

s="$HOME/.agents/skills/agmsg/scripts"
project="$work/sample-project"
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

# Sanitize the isolated project path; never record a runner path.
python3 - "$out" "$project" <<'PYTHON'
import pathlib, sys
project = pathlib.Path(sys.argv[2])
variants = {sys.argv[2], str(project), str(project.resolve())}
for path in pathlib.Path(sys.argv[1]).glob("*.jsonl"):
    text = path.read_text()
    for value in sorted(variants, key=len, reverse=True):
        text = text.replace(value, "/tmp/sample-project")
    path.write_text(text)
PYTHON
printf '%s\n' "$sha" > "$out/COMMIT.txt"
echo "captured $tag ($sha) into $out"
