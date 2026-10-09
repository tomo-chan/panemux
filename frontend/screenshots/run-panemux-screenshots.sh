#!/bin/sh
# Boots the showcase.yml fixture (:4180) that screenshots.spec.ts captures
# the documentation images from.
#
# Everything the panes can show is staged here so the images carry nothing
# from the machine that generates them (DEVELOPMENT.md's path-sanitization
# rule):
#
# - HOME is a throwaway directory, and the XDG and git configuration
#   variables are cleared (screenshots-env.sh's shot_isolate_env), so
#   nothing reads the developer's own configuration through them. bash reads
#   the fake HOME's .bashrc, whose prompt names no real user or host; tmux
#   reads only its .tmux.conf, whose status line drops the default hostname;
#   git reads only its .gitconfig. macOS's bash is told not to print its
#   zsh notice (shot_shell_env).
# - The panes' working directory is /tmp/sample-project, a git repository
#   created here with placeholder commits and a github.com/example remote.
# - tmux runs on a private socket (TMUX_TMPDIR), so a tmux server the
#   developer already has is neither shown nor touched. The server
#   daemonizes and so outlives panemux; global-teardown.ts stops it.
# - The Agent Board reads a stub agmsg installation (the e2e suite's own
#   fixture scripts) seeded with placeholder messages.
#
# /tmp/sample-project is a fixed path because the images show it, and
# /tmp/panemux-screenshots-agmsg because showcase.yml names it. Both are
# links into the run's root, $TMPDIR/panemux-screenshots, which is emptied
# only when it carries the marker this script leaves in it; anything at those
# paths this script did not leave stops the run untouched (screenshots-env.sh's
# shot_link_dir and shot_clear). A lock keeps two runs from emptying each
# other's.
#
# The task dashboard is not staged here: the collection lists every claude
# process of the user running this script, a developer's real sessions
# included, so screenshots.spec.ts serves /api/tasks itself.
set -eu

# Pin direct execution and every Go subprocess to this checkout's go.mod.
panemux_toolchain_root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
panemux_runtime_scripts="$panemux_toolchain_root/scripts"
. "$panemux_runtime_scripts/runtime-env.sh"
panemux_runtime "$panemux_toolchain_root" || exit 1
unset PANEMUX_PANE_ID TMUX

SHOT_DIR="$(cd "$(dirname "$0")" && pwd)"
E2E_DIR="$SHOT_DIR/../e2e"

. "$SHOT_DIR/screenshots-env.sh"
. "$E2E_DIR/tmux-env.sh"

SHOT_ROOT="$(shot_root)"
# Before anything is staged: a $TMPDIR too long for tmux's socket fails here,
# saying so, rather than as a tmux error after the build.
tmux_socket_path_check "$SHOT_ROOT/tmux"
SHOT_HOME="$SHOT_ROOT/home"
SHOT_PROJECT=/tmp/sample-project
# Must match showcase.yml's agent_board.agmsg_path.
SHOT_AGMSG_DIR=/tmp/panemux-screenshots-agmsg

# Held until panemux, which this shell execs into, exits.
shot_lock /tmp/panemux-screenshots.lock $$

export TMUX_TMPDIR="$SHOT_ROOT/tmux"
shot_stop_tmux
shot_claim_dir "$SHOT_ROOT"
shot_link_dir "$SHOT_PROJECT" "$SHOT_ROOT/${SHOT_PROJECT##*/}"
shot_link_dir "$SHOT_AGMSG_DIR" "$SHOT_ROOT/${SHOT_AGMSG_DIR##*/}"
mkdir -p "$SHOT_HOME" "$TMUX_TMPDIR"
chmod 700 "$TMUX_TMPDIR"

# Built while HOME is still the real one, so npm and go use their usual
# caches. The fixture config is copied out of the repository because panemux
# rewrites its own config file on some API calls.
(cd "$SHOT_DIR/../.." && make build-frontend >/dev/null && go build -o "$SHOT_ROOT/panemux" .)
cp "$SHOT_DIR/showcase.yml" "$SHOT_ROOT/showcase.yml"

shot_isolate_env "$SHOT_HOME"
shot_shell_env

cat >"$HOME/.bashrc" <<'RC'
PS1='\[\e[32m\]demo@panemux\[\e[0m\]:\[\e[34m\]\w\[\e[0m\]$ '
HISTFILE=/dev/null
RC
cat >"$HOME/.tmux.conf" <<'CONF'
set -g status-left '[sample-server] '
set -g status-left-length 20
set -g status-right ''
set -g automatic-rename off
set -g allow-rename off
set -g status-style 'bg=#264f78,fg=#cccccc'
CONF
cat >"$HOME/.gitconfig" <<'GIT'
[user]
    name = Demo User
    email = demo@example.com
[init]
    defaultBranch = main
[advice]
    detachedHead = false
GIT

# The sample repository. Commit dates are fixed so `git log` reads the same
# on every run. `cd` keeps the link's path in PWD, which panemux and so the
# panes' bash inherit: bash's prompt shows PWD, not the directory it resolves
# to, as long as both name the same directory.
cd "$SHOT_PROJECT"
git init -q
git remote add origin https://github.com/example/sample-project.git
commit() {
    GIT_AUTHOR_DATE="$1" GIT_COMMITTER_DATE="$1" git commit -q --allow-empty -m "$2"
}
printf '# sample-project\n' >README.md
git add README.md
commit 2026-01-05T10:00:00Z 'chore: initial commit'
commit 2026-01-06T11:20:00Z 'feat: add HTTP server skeleton'
commit 2026-01-07T09:45:00Z 'feat: add /api/items endpoint'
commit 2026-01-08T16:10:00Z 'test: cover item validation'
git checkout -q -b feature/search
commit 2026-01-09T13:30:00Z 'feat: add search endpoint'
commit 2026-01-10T15:05:00Z 'fix: escape query in search results'
mkdir -p cmd internal/api internal/search scripts
: >go.mod
: >cmd/main.go
: >internal/api/items.go
: >internal/search/search.go
: >Makefile
cat >scripts/test <<'TEST'
#!/bin/sh
printf 'ok  	sample-project/internal/api	0.412s
'
printf 'ok  	sample-project/internal/items	0.218s
'
printf 'ok  	sample-project/internal/search	0.637s
'
TEST
chmod +x scripts/test

# What the tmux pane shows: a dev server's log, printed once and then left
# running so the session stays up for the whole run.
cat >"$HOME/dev-server" <<'SERVER'
#!/bin/sh
printf '\033[2mbuilding sample-project...\033[0m\n'
printf 'listening on http://127.0.0.1:8080\n'
printf '\033[32mGET \033[0m /api/items          200  3.1ms\n'
printf '\033[32mGET \033[0m /api/items/42       200  1.4ms\n'
printf '\033[33mPOST\033[0m /api/items          201  5.8ms\n'
printf '\033[32mGET \033[0m /api/search?q=panes 200  7.2ms\n'
printf '\033[31mGET \033[0m /api/items/999      404  0.9ms\n'
printf '\033[32mGET \033[0m /api/search?q=tmux  200  6.5ms\n'
exec sleep 900
SERVER
chmod +x "$HOME/dev-server"
if command -v tmux >/dev/null 2>&1; then
    shot_start_tmux -s sample-server -n server -c "$SHOT_PROJECT" "$HOME/dev-server"
fi

# The Agent Board's message store.
mkdir -p "$SHOT_AGMSG_DIR/scripts"
cp "$E2E_DIR/fixtures/agmsg/scripts/api.sh" "$E2E_DIR/fixtures/agmsg/scripts/send.sh" "$SHOT_AGMSG_DIR/scripts/"
chmod +x "$SHOT_AGMSG_DIR/scripts/api.sh" "$SHOT_AGMSG_DIR/scripts/send.sh"
: >"$SHOT_AGMSG_DIR/messages.jsonl"
date +%s >"$SHOT_AGMSG_DIR/next_id"
SHOT_TEAM=panemux-screenshots
"$SHOT_AGMSG_DIR/scripts/send.sh" "$SHOT_TEAM" editor _system \
    '{"kind":"board_status","state":"working","last_tool":"Edit","summary":"Adding pagination to the search endpoint and updating its handler tests"}' --force
"$SHOT_AGMSG_DIR/scripts/send.sh" "$SHOT_TEAM" tests _system \
    '{"kind":"board_status","state":"waiting","last_tool":"Bash","summary":"Search tests pass; waiting for the pagination change before running the full suite"}' --force
"$SHOT_AGMSG_DIR/scripts/send.sh" "$SHOT_TEAM" editor tests \
    'Pagination is in. Please run the search tests again.' --force
"$SHOT_AGMSG_DIR/scripts/send.sh" "$SHOT_TEAM" tests editor \
    'All 24 search tests pass. One flaky timeout in items_test.go, looking now.' --force

cd "$SHOT_PROJECT"
exec "$SHOT_ROOT/panemux" --config "$SHOT_ROOT/showcase.yml" --port 4180
