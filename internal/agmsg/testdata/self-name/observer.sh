#!/usr/bin/env bash
set -eu
test "${AGMSG_SELF_NAME:-}" = off
test "${BASH_ENV:-}${ENV:-}" = ''
test "${AGMSG_STORAGE_PATH:-}" = /workspace/sample-store
for variable in TMUX TMUX_PANE HERDR_ENV HERDR_SOCKET_PATH HERDR_PANE_ID \
  ORCA_TERMINAL_HANDLE CLAUDE_CODE_ENTRYPOINT CODEX_THREAD_ID; do
  test -z "${!variable:-}"
done
if [ "${1:-}" != get ] && [ -n "${PANEMUX_TEST_CAPTURE:-}" ]; then
  printf '%s\0' "$@" > "$PANEMUX_TEST_CAPTURE"
fi
