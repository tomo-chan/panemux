#!/bin/sh
# Boots the task-dashboard fixture (:4179) with an isolated HOME holding the
# agent sessions the dashboard should find.
#
# The collection itself is the real one: the fixed script runs `ps` and
# `tmux` on this machine and reads ~/.claude under HOME. What is fake is only
# what an agent would have written and the agent process itself — `claude`
# here is a symlink to `sleep`, so `ps` lists a process named claude that
# does nothing. Each one lives for 15 minutes, which outlasts the suite and
# takes the tmux session down with it afterwards. The fixture also stops its
# private server on exit, including tasks started or resumed by the suite.
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
#
# bin/start-agent is what the spec types into the local pane: an agent
# started from a pane's shell inherits that shell's PANEMUX_PANE_ID. The
# variable is removed from this script's own environment, so a suite run from
# inside a panemux pane does not hand that pane's ID to the other fakes.
set -eu

# Pin direct execution and every Go subprocess to this checkout's go.mod.
panemux_toolchain_root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
panemux_runtime_scripts="$panemux_toolchain_root/scripts"
. "$panemux_runtime_scripts/runtime-env.sh"
panemux_runtime "$panemux_toolchain_root" || exit 1
unset PANEMUX_PANE_ID

E2E_DIR="$(cd "$(dirname "$0")" && pwd)"

. "$E2E_DIR/tmux-env.sh"
e2e_tmux_env
trap e2e_tmux_cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

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
# A state file the dashboard cannot read, whose name carries no pid: it is
# reported as unreadable on every collection, never shown as a task (#313).
printf '{"pid":"not-a-number"}\n' >"$E2E_HOME/.claude/sessions/legacy.json"

ln -s "$(command -v sleep)" "$E2E_HOME/bin/claude"
now_ms="$(date +%s)000"

write_state() {
    printf '{"pid":%s,"sessionId":"%s","cwd":"/tmp","status":"%s","waitingFor":"input needed","statusUpdatedAt":%s}\n' \
        "$1" "$2" "$3" "$now_ms" >"$E2E_HOME/.claude/sessions/$1.json"
}

"$E2E_HOME/bin/claude" 900 >/dev/null 2>&1 &
write_state "$!" e2e-outside busy

cat >"$E2E_HOME/bin/start-agent" <<'AGENT'
#!/bin/sh
"$HOME/bin/claude" 900 >/dev/null 2>&1 &
printf '{"pid":%s,"sessionId":"%s","cwd":"/tmp","status":"busy","statusUpdatedAt":%s000}\n' \
    "$!" "$1" "$(date +%s)" >"$HOME/.claude/sessions/$!.json"
AGENT
chmod 700 "$E2E_HOME/bin/start-agent"

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
    tmux -f /dev/null new-session -d -s e2e-task-dashboard "exec '$E2E_HOME/bin/claude' 900"
    write_state "$(tmux list-panes -t e2e-task-dashboard -F '#{pane_pid}')" e2e-in-tmux waiting
fi

# Keep this wrapper alive so its EXIT trap stops the private tmux server.
sh "$E2E_DIR/run-panemux-e2e.sh" task-dashboard.yml 4179
