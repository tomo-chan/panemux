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
# Starting and resuming a task (issue #257) runs the real launch script and
# real tmux, with `claude` resolved from launch-bin, which is put first on
# PATH so a claude installed on the machine is never started. That stand-in
# records a running session the way Claude Code does — a state file naming
# the session the launch passed it — for a child process ps names claude.
#
# tmux is optional, the way jq is for make test-hooks: without it the
# inside-tmux session is simply not created, and task-dashboard.spec.ts skips
# the tests that need it.
set -eu

E2E_DIR="$(cd "$(dirname "$0")" && pwd)"

GOPATH="$(go env GOPATH)"
GOMODCACHE="$(go env GOMODCACHE)"
export GOPATH GOMODCACHE

E2E_HOME="${TMPDIR:-/tmp}/panemux-e2e-task-dashboard-home"
# The stand-ins a previous run started or resumed outlive it. With their
# state files gone they would list as unknown claude processes and claim the
# conversation logs in their directories, hiding the session to resume.
if command -v pkill >/dev/null 2>&1; then
    pkill -f "$E2E_HOME/fake/claude/sleep" 2>/dev/null || true
fi
rm -rf "$E2E_HOME"
mkdir -p "$E2E_HOME/.claude/sessions" "$E2E_HOME/.claude/projects/-tmp-e2e-stopped" \
    "$E2E_HOME/.claude/projects/-e2e-resumable" "$E2E_HOME/bin" "$E2E_HOME/launch-bin" "$E2E_HOME/fake/claude" \
    "$E2E_HOME/resumable"
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

# A stopped session the dashboard can resume: its ID is a UUID.
E2E_RESUMABLE=5d7e3a90-1b2c-4d3e-8f40-51627384a5b6
printf '{"cwd":"%s"}\n' "$E2E_HOME/resumable" >"$E2E_HOME/.claude/projects/-e2e-resumable/$E2E_RESUMABLE.jsonl"

ln -s "$(command -v sleep)" "$E2E_HOME/fake/claude/sleep"
cat >"$E2E_HOME/launch-bin/claude" <<'FAKE'
#!/bin/sh
sid=
for arg do
    case $arg in
    --session-id=*) sid=${arg#--session-id=} ;;
    --resume=*) sid=${arg#--resume=} ;;
    esac
done
"$HOME/fake/claude/sleep" 900 &
printf '{"pid":%s,"sessionId":"%s","cwd":"%s","status":"idle","statusUpdatedAt":%s000}\n' \
    "$!" "$sid" "$PWD" "$(date +%s)" >"$HOME/.claude/sessions/$!.json"
wait
FAKE
chmod +x "$E2E_HOME/launch-bin/claude"
export PATH="$E2E_HOME/launch-bin:$PATH"

if command -v tmux >/dev/null 2>&1; then
    tmux kill-session -t e2e-task-dashboard 2>/dev/null || true
    # A previous run's resumed session would make this run's resume refuse.
    tmux kill-session -t "=task-${E2E_RESUMABLE%%-*}" 2>/dev/null || true
    tmux new-session -d -s e2e-task-dashboard "exec '$E2E_HOME/bin/claude' 900"
    write_state "$(tmux list-panes -t e2e-task-dashboard -F '#{pane_pid}')" e2e-in-tmux waiting
fi

exec sh "$E2E_DIR/run-panemux-e2e.sh" task-dashboard.yml 4179
