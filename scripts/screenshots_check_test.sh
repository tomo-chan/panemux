#!/bin/sh
#
# Tests for scripts/screenshots_check.sh.
#
# The checker decides, from a pull request's changed files, whether the
# documentation screenshots had to be retaken. Both directions matter: a
# change the images show that leaves docs/images/ untouched must fail, and a
# change they cannot show must pass, because a gate that fires on changes it
# has no opinion about gets bypassed (design principle 4).
#
# Run with: make test-screenshots-check

set -u

scripts_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
checker="$scripts_dir/screenshots_check.sh"

failures=0
checks=0

fail() {
	failures=$((failures + 1))
	echo "FAIL: $1"
	[ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/      /'
}
pass() { echo "ok   $1"; }

# expect <want-exit> <name> <changed files, one per line> [substring the output must contain]
expect() {
	want=$1
	name=$2
	changed=$3
	needle=${4:-}
	checks=$((checks + 1))

	output=$(printf '%s' "$changed" | sh "$checker" 2>&1)
	got=$?
	if [ "$got" -ne "$want" ]; then
		fail "$name: wanted exit $want, got $got" "$output"
		return
	fi
	if [ -n "$needle" ]; then
		case $output in
		*"$needle"*) ;;
		*)
			fail "$name: output does not mention '$needle'" "$output"
			return
			;;
		esac
	fi
	pass "$name"
}

nl='
'

# ── Changes the images show, without new images: fail ───────────────────────

expect 1 'a component change without new images' \
	"frontend/src/components/PaneHeader.tsx${nl}" 'frontend/src/components/PaneHeader.tsx'
expect 1 'the app shell' "frontend/src/App.tsx${nl}" 'frontend/src/App.tsx'
expect 1 'a stylesheet' "frontend/src/styles/taskDashboard.css${nl}" 'frontend/src/styles/taskDashboard.css'
expect 1 'the page shell' "frontend/index.html${nl}" 'frontend/index.html'
expect 1 'the capture itself' "frontend/screenshots/showcase.yml${nl}" 'frontend/screenshots/showcase.yml'
expect 1 'the remedy is named' "frontend/src/App.tsx${nl}" 'make screenshots'
expect 1 'the escape hatch is named' "frontend/src/App.tsx${nl}" 'screenshots-exempt'
expect 1 'a change elsewhere in docs/ is not new images' \
	"frontend/src/App.tsx${nl}docs/ui-design.md${nl}README.md${nl}"
expect 1 'only the triggering files are listed' \
	"frontend/src/App.tsx${nl}internal/api/server.go${nl}" 'frontend/src/App.tsx'

# ── Changes the images show, with new images: pass ──────────────────────────

expect 0 'a component change with new images' \
	"frontend/src/components/PaneHeader.tsx${nl}docs/images/workspace.png${nl}"
expect 0 'any one image is enough' \
	"frontend/src/App.tsx${nl}frontend/src/styles/attention.css${nl}docs/images/task-dashboard.png${nl}"

# ── Changes the images cannot show: pass ────────────────────────────────────

expect 0 'no files at all' ''
expect 0 'backend only' "internal/api/server.go${nl}internal/config/validate.go${nl}"
expect 0 'a component test' "frontend/src/components/PaneHeader.test.tsx${nl}"
expect 0 'an app test' "frontend/src/App.test.tsx${nl}"
expect 0 'hooks, schemas and utils hold no presentation' \
	"frontend/src/hooks/useTasks.ts${nl}frontend/src/schemas/index.ts${nl}frontend/src/utils/layoutTree.ts${nl}"
expect 0 'the e2e suite' "frontend/e2e/core-multiplexer.spec.ts${nl}"
expect 0 'documentation only' "README.md${nl}docs/ui-design.md${nl}"
expect 0 'a path that only contains a trigger as a substring' \
	"docs/frontend/src/components/note.md${nl}vendor/frontend/index.html${nl}"


# ── A rename is read as both of its paths ───────────────────────────────────

expect 1 'a component moved out of components/ lists its old path' \
	"frontend/src/components/PaneHeader.tsx${nl}frontend/src/panes/PaneHeader.tsx${nl}" \
	'frontend/src/components/PaneHeader.tsx'

# Given a base revision, the checker reads the diff itself, and must do so
# with rename detection off: `git diff --name-only` otherwise prints only a
# moved file's new path, and a component moved out of components/ and
# restyled in the same pull request would pass unseen.
checks=$((checks + 1))
repo=$(mktemp -d)
(
	set -e
	cd "$repo"
	git init -q
	git -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m base
	mkdir -p frontend/src/components
	printf 'export const Foo = () => null\n%.0s' 1 2 3 4 5 6 7 8 >frontend/src/components/Foo.tsx
	git add . && git -c user.name=t -c user.email=t@example.com commit -q -m add
	mkdir -p frontend/src/panes
	git mv frontend/src/components/Foo.tsx frontend/src/panes/Foo.tsx
	echo '// color: red' >>frontend/src/panes/Foo.tsx
	git -c user.name=t -c user.email=t@example.com commit -qam move
) >/dev/null 2>&1
output=$(cd "$repo" && sh "$checker" HEAD~1 </dev/null 2>&1)
got=$?
rm -rf "$repo"
case "$got:$output" in
1:*frontend/src/components/Foo.tsx*) pass 'a base revision diffs without rename detection' ;;
*) fail "a base revision diffs without rename detection: wanted exit 1 naming the old path, got $got" "$output" ;;
esac


if [ "$failures" -ne 0 ]; then
	echo "screenshots-check tests: $failures of $checks failed"
	exit 1
fi
echo "screenshots-check tests: all $checks passed"
