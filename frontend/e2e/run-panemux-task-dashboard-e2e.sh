#!/bin/sh
# Boots the task-dashboard fixture (:4179) with an isolated HOME holding the
# agent sessions the dashboard should find.
#
# The collection itself is the real one: the fixed script runs `ps` and
# `tmux` on this machine and reads ~/.claude under HOME. What is fake is only
# what an agent would have written and the agent process itself — `claude`
# here is a symlink to `sleep`, so `ps` lists a process named claude that
# does nothing. Each one lives for 15 minutes, which outlasts the suite and
# takes the tmux session down with it afterwards.
#
# tmux is optional, the way jq is for make test-hooks: without it the
# inside-tmux session is simply not created, and task-dashboard.spec.ts skips
# the one test that needs it.
set -eu

E2E_DIR="$(cd "$(dirname "$0")" && pwd)"

GOPATH="$(go env GOPATH)"
GOMODCACHE="$(go env GOMODCACHE)"
export GOPATH GOMODCACHE

E2E_HOME="${TMPDIR:-/tmp}/panemux-e2e-task-dashboard-home"
rm -rf "$E2E_HOME"
mkdir -p "$E2E_HOME/.claude/sessions" "$E2E_HOME/.claude/projects/-tmp-e2e-stopped" "$E2E_HOME/bin"
export HOME="$E2E_HOME"

ln -s "$(command -v sleep)" "$E2E_HOME/bin/claude"
now_ms="$(date +%s)000"

write_state() {
    printf '{"pid":%s,"sessionId":"%s","cwd":"/tmp","status":"%s","waitingFor":"input needed","statusUpdatedAt":%s}\n' \
        "$1" "$2" "$3" "$now_ms" >"$E2E_HOME/.claude/sessions/$1.json"
}

"$E2E_HOME/bin/claude" 900 >/dev/null 2>&1 &
write_state "$!" e2e-outside busy

printf '{"cwd":"/tmp/e2e-stopped"}\n' >"$E2E_HOME/.claude/projects/-tmp-e2e-stopped/e2e-stopped.jsonl"

if command -v tmux >/dev/null 2>&1; then
    tmux kill-session -t e2e-task-dashboard 2>/dev/null || true
    tmux new-session -d -s e2e-task-dashboard "exec '$E2E_HOME/bin/claude' 900"
    write_state "$(tmux list-panes -t e2e-task-dashboard -F '#{pane_pid}')" e2e-in-tmux waiting
fi

exec sh "$E2E_DIR/run-panemux-e2e.sh" task-dashboard.yml 4179
