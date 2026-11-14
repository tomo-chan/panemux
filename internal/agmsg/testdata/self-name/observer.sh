#!/usr/bin/env bash
set -eu
test "${AGMSG_SELF_NAME:-}" = off
test "${BASH_ENV:-}${ENV:-}" = ''
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in ''|'#'*) continue ;; esac
  variable=${line%%=*}
  expected=${line#*=}
  test "${!variable+x}" = x
  test "${!variable-}" = "$expected"
done < "$PANEMUX_TEST_EXPECT_SETTINGS"
test "${HOME:-}" = "$PANEMUX_TEST_EXPECT_HOME"
test "${PATH:-}" = "$PANEMUX_TEST_EXPECT_PATH"
for variable in TMUX TMUX_PANE HERDR_ENV HERDR_SOCKET_PATH HERDR_PANE_ID \
  ORCA_TERMINAL_HANDLE CLAUDE_CODE_ENTRYPOINT CODEX_THREAD_ID; do
  test -z "${!variable:-}"
done
if [ "${1:-}" != get ] && [ -n "${PANEMUX_TEST_CAPTURE:-}" ]; then
  printf '%s\0' "$@" > "$PANEMUX_TEST_CAPTURE"
fi
