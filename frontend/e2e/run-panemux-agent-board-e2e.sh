#!/bin/sh
# Installs the stub agmsg fixture (see fixtures/agmsg/scripts/api.sh for why
# a stub is needed at all), seeds its message store, then hands off to the
# shared runner for the agent-board-agmsg.yml fixture on :4175.
#
# The store is seeded through the stub's own send.sh rather than written
# directly, so the seeded rows go through exactly the same encoding path a
# broadcast-written row does.
set -eu

panemux_toolchain_root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
panemux_runtime_scripts="$panemux_toolchain_root/scripts"
. "$panemux_runtime_scripts/runtime-env.sh"
panemux_runtime "$panemux_toolchain_root" || exit 1

E2E_DIR="$(cd "$(dirname "$0")" && pwd)"

E2E_HOME=$(mktemp -d "${TMPDIR:-/tmp}/panemux-e2e-agmsg-home.XXXXXX") || exit 1
export E2E_HOME
E2E_SERVER_PID=""
e2e_cleanup() {
    if [ -n "$E2E_SERVER_PID" ]; then
        kill -TERM "$E2E_SERVER_PID" 2>/dev/null || true
        wait "$E2E_SERVER_PID" 2>/dev/null || true
    fi
    find "$E2E_HOME" -depth -delete
}
trap e2e_cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
E2E_AGMSG_DIR="$E2E_HOME/.agents/skills/agmsg"

mkdir -p "$E2E_AGMSG_DIR/scripts"
cp "$E2E_DIR/fixtures/agmsg/scripts/api.sh" "$E2E_AGMSG_DIR/scripts/api.sh"
cp "$E2E_DIR/fixtures/agmsg/scripts/send.sh" "$E2E_AGMSG_DIR/scripts/send.sh"
chmod +x "$E2E_AGMSG_DIR/scripts/api.sh" "$E2E_AGMSG_DIR/scripts/send.sh"
: >"$E2E_AGMSG_DIR/messages.jsonl"
date +%s >"$E2E_AGMSG_DIR/next_id"

E2E_TEAM=panemux-e2e
# The summary is deliberately longer than one 420px line: clipping it was
# the bug the dashboard's wrapping treatment fixes, and a short fixture
# summary would pass either way.
E2E_STATUS_BODY='{"kind":"board_status","state":"working","cwd":"/workspace/user/project","branch":"feature/agent-board","repo":"example/project","pr_url":"https://github.com/example/project/pull/42","last_tool":"Edit","summary":"Wiring the dashboard panel so a pane that never joined the board is still listed rather than silently missing"}'

"$E2E_AGMSG_DIR/scripts/send.sh" "$E2E_TEAM" board-main _system "$E2E_STATUS_BODY" --force
"$E2E_AGMSG_DIR/scripts/send.sh" "$E2E_TEAM" board-main board-worker \
    'Handing the dashboard panel over for review.' --force

sh "$E2E_DIR/run-panemux-e2e.sh" agent-board-agmsg.yml 4175 &
E2E_SERVER_PID=$!
status=0
wait "$E2E_SERVER_PID" || status=$?
E2E_SERVER_PID=""
exit "$status"
