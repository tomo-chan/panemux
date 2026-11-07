#!/usr/bin/env bash
set -eu
# Exercise the public upstream hook with fake dependencies only. None of these
# functions consults an agmsg store, terminal, driver process, or user record.
source "$PANEMUX_TEST_RENAME_HOOK"
agmsg_role_session_get() { printf 'codex\n'; }
agmsg_type_get() { [ "$2" != rename_cmd ] || printf '/rename\n'; return 0; }
agmsg_terminal_self_env() {
  [ -n "${TMUX:-}${TMUX_PANE:-}${HERDR_ENV:-}${ORCA_TERMINAL_HANDLE:-}${CLAUDE_CODE_ENTRYPOINT:-}" ] || return 0
  printf 'tmux\t%%7\t123\n'
}
agmsg_terminal_ref() { printf '%s:%s\n' "$1" "$2"; }
agmsg_terminal_load() { return 0; }
agmsg_terminal_ref_qualify() { printf '%s\n' "$1"; }
agmsg_role_session_renamed() { [ ! -f "$PANEMUX_TEST_MARK" ] || cat "$PANEMUX_TEST_MARK"; }
agmsg_role_session_mark_renamed() { printf '%s\t%s\t%s\n' "$3" "$4" "$5" > "$PANEMUX_TEST_MARK"; }
agmsg_team_observe_loaded() { return 0; }
_agmsg_self_rename_observed() { printf 'old-name\n'; }
agmsg_self_proof() { printf 'proved\ttmux:%%7\n'; }
agmsg_safe_poke() { printf 'fake-poke\n' > "$PANEMUX_TEST_POKE"; }
agmsg_self_rename_on_action "$1" "$2"
