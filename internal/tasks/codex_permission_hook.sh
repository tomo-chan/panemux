#!/bin/sh
# panemux: a codex PermissionRequest command hook (issue #384). It records
# that the session's turn asked for an approval, for the task dashboard to
# show the session as waiting. It keeps the session ID, the turn ID and the
# time, nothing of the command or the conversation, answers nothing (codex
# decides as it would without the hook), and always exits 0.
#
# Register it in ~/.codex/hooks.json; see docs/behavior/tasks.md.
in=$(cat)
id() {
	printf '%s\n' "$in" | grep -o "\"$1\": *\"[0-9a-f-]*\"" | head -n 1 | sed 's/.*"\([0-9a-f-]*\)"$/\1/'
}
sid=$(id session_id)
tid=$(id turn_id)
if [ -n "$sid" ] && [ -n "$tid" ]; then
	dir="$HOME/.local/state/panemux/codex-permission"
	umask 077
	mkdir -p "$dir" &&
		printf '%s %s %s\n' "$sid" "$tid" "$(date +%s)" >"$dir/.$sid.$$" &&
		mv -f "$dir/.$sid.$$" "$dir/$sid"
fi >/dev/null 2>&1
exit 0
