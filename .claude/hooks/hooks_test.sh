#!/bin/sh
#
# Tests for the G1/G2 hook scripts in this directory.
#
# The hooks themselves are the enforcement mechanism for
# docs/quality-gateway.md's gates G1 (edit) and G2 (unit), so a hook that
# silently does nothing is worse than no hook at all — it reports the
# discipline as enforced while enforcing nothing. Design principle 4 (no false
# positives) cuts the other way just as hard: a hook that blocks a healthy
# change gets bypassed, and then so does every other hook.
#
# Both directions are asserted below. Run with:
#
#   make test-hooks

set -u

hooks_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$hooks_dir/../.." && pwd)

failures=0
checks=0

fail() {
	failures=$((failures + 1))
	echo "FAIL: $1"
}

pass() {
	echo "ok   $1"
}

# expect_status <want> <name> <command...>
expect_status() {
	want=$1
	name=$2
	shift 2
	checks=$((checks + 1))

	output=$("$@" 2>&1)
	got=$?
	if [ "$got" -eq "$want" ]; then
		pass "$name"
		return
	fi
	fail "$name: wanted exit $want, got $got"
	[ -n "$output" ] && printf '     output: %s\n' "$output"
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# A Go file gofmt -s would rewrite: the indentation is spaces, not a tab.
cat > "$work/unformatted.go" <<'GO'
package sample

func Sample() int {
    return 1
}
GO

cat > "$work/formatted.go" <<'GO'
package sample

func Sample() int {
	return 1
}
GO

echo "# not code" > "$work/notes.md"

edit_hook="$hooks_dir/post-edit-check.sh"

# --- G1: the edit gate -------------------------------------------------------

expect_status 2 "an unformatted Go file is blocked" \
	"$edit_hook" --file "$work/unformatted.go"

expect_status 0 "a formatted Go file passes" \
	"$edit_hook" --file "$work/formatted.go"

# Principle 4 in both directions: a file the gate has no opinion about must
# never block, and neither must a path that no longer exists (Claude Code can
# report an edit to a file a later step removed).
expect_status 0 "a non-code file is ignored" \
	"$edit_hook" --file "$work/notes.md"

expect_status 0 "a missing file is ignored" \
	"$edit_hook" --file "$work/gone.go"

expect_status 0 "no file argument at all is ignored" \
	"$edit_hook" --file ""

# The real invocation is Claude Code piping hook JSON on stdin, not --file.
# Reading that JSON is the part most likely to break silently, so it is
# asserted against the same fixtures rather than trusted.
stdin_status() {
	printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' "$1" | "$edit_hook"
}

expect_status 2 "hook JSON on stdin reaches the same verdict (unformatted)" \
	stdin_status "$work/unformatted.go"

expect_status 0 "hook JSON on stdin reaches the same verdict (formatted)" \
	stdin_status "$work/formatted.go"

# Claude Code reports the written path under tool_response.filePath for some
# tools and tool_input.file_path for others; both spellings must work.
printf '{"tool_name":"Write","tool_response":{"filePath":"%s"}}' "$work/unformatted.go" \
	> "$work/response-shape.json"
response_shape_status() {
	"$edit_hook" < "$work/response-shape.json"
}
expect_status 2 "the tool_response.filePath spelling is read too" response_shape_status

# Malformed input is not a reason to block a turn.
malformed_status() {
	printf 'not json at all' | "$edit_hook"
}
expect_status 0 "malformed hook input does not block" malformed_status

# --- G2: the stop gate -------------------------------------------------------

stop_hook="$hooks_dir/stop-check.sh"

# A tree with nothing changed has nothing to verify, and must not spend a
# second pretending otherwise.
clean_tree_status() {
	clean=$(mktemp -d)
	(
		cd "$clean" || exit 1
		git init -q .
		git config user.email "test@example.invalid"
		git config user.name "hook test"
		"$stop_hook"
	)
	status=$?
	rm -rf "$clean"
	return $status
}
expect_status 0 "a clean tree stops immediately" clean_tree_status

# The stop gate refuses to end a turn that leaves unformatted Go behind.
dirty_tree_status() {
	dirty=$(mktemp -d)
	(
		cd "$dirty" || exit 1
		git init -q .
		git config user.email "test@example.invalid"
		git config user.name "hook test"
		printf 'module sample\n\ngo 1.25\n' > go.mod
		cp "$work/unformatted.go" sample.go
		"$stop_hook"
	)
	status=$?
	rm -rf "$dirty"
	return $status
}
expect_status 2 "unformatted Go blocks the turn from ending" dirty_tree_status

# ...and lets a healthy one through. This is the case that decides whether the
# gate keeps its credibility.
healthy_tree_status() {
	healthy=$(mktemp -d)
	(
		cd "$healthy" || exit 1
		git init -q .
		git config user.email "test@example.invalid"
		git config user.name "hook test"
		printf 'module sample\n\ngo 1.25\n' > go.mod
		cp "$work/formatted.go" sample.go
		"$stop_hook"
	)
	status=$?
	rm -rf "$healthy"
	return $status
}
expect_status 0 "a healthy change does not block the turn" healthy_tree_status

# --- D6: the stop gate is not make check -------------------------------------
#
# Putting all of make check in the Stop hook would run the frontend build and
# the whole -race suite on every turn. Decision D6 says explicitly not to, and
# a gate that sacrifices fast feedback gets bypassed — so the absence is
# asserted rather than left to reviewer memory.
# Comment lines are stripped first: the script's own header explains why it is
# not make check, and matching that sentence would make this a false positive —
# the exact failure mode principle 4 warns about, in the test that guards it.
checks=$((checks + 1))
if sed 's/#.*//' "$stop_hook" | grep -qE 'make[[:space:]]+(check|build|test-e2e)\b'; then
	fail "the Stop hook must not run make check / make build / make test-e2e (decision D6)"
else
	pass "the Stop hook stays inside G1+G2 (decision D6)"
fi

# --- settings.json wiring ----------------------------------------------------
#
# The scripts above are only a gate if settings.json actually points at them.
settings="$repo_root/.claude/settings.json"

checks=$((checks + 1))
if jq -e . "$settings" > /dev/null 2>&1; then
	pass "settings.json is valid JSON"
else
	fail "settings.json is not valid JSON — a malformed file silently disables every setting in it"
fi

assert_wired() {
	event=$1
	script=$2
	checks=$((checks + 1))
	if jq -e --arg s "$script" \
		".hooks.$event[].hooks[] | select(.command | contains(\$s))" \
		"$settings" > /dev/null 2>&1; then
		pass "$event is wired to $script"
	else
		fail "$event is not wired to $script"
	fi
}

assert_wired PostToolUse post-edit-check.sh
assert_wired Stop stop-check.sh

echo
if [ "$failures" -eq 0 ]; then
	echo "all $checks hook checks passed"
	exit 0
fi
echo "$failures of $checks hook checks failed"
exit 1
