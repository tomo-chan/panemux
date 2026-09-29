#!/bin/sh
#
# Tests for scripts/docs_links_check.sh.
#
# The gate exists because a one-time manual pass did not hold: PR #248 split
# five documents, claimed every link and anchor had been checked, and shipped
# two broken anchors in the paragraph immediately below that claim. Both were
# missed by a checker that matched links line by line — the link's own label
# wrapped across a newline — so the multi-line case below is the regression
# this whole file exists for.
#
# The rest of the cases are the false-positive side. Markdown prose is full of
# things shaped like links that must not be resolved: fenced code blocks quote
# Go comments containing real paths, and external URLs are not this
# repository's to verify. A gate that fires on those gets bypassed
# (design principle 4).
#
# Run with: make test-docs-links

set -u

scripts_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
checker="$scripts_dir/docs_links_check.sh"

failures=0
checks=0

fail() {
	failures=$((failures + 1))
	echo "FAIL: $1"
	[ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/      /'
}
pass() { echo "ok   $1"; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# fixture — a fresh empty directory for one case.
#
# `mktemp -d`, not a counter: this is called as `root=$(fixture)`, and command
# substitution runs in a subshell, so a counter incremented here would be lost
# and every case would share one directory. That is not a hypothetical — the
# first version did exactly that, and several cases passed on files another
# case had left behind.
fixture() {
	mktemp -d "$work/caseXXXXXX"
}

# expect <want-exit> <root> <name> [substring the output must contain]
expect() {
	want=$1
	root=$2
	name=$3
	needle=${4:-}
	checks=$((checks + 1))

	output=$("$checker" "$root" 2>&1)
	got=$?
	if [ "$got" -ne "$want" ]; then
		fail "$name: wanted exit $want, got $got" "$output"
		return
	fi
	if [ -n "$needle" ]; then
		case "$output" in
		*"$needle"*) ;;
		*)
			fail "$name: output did not mention '$needle'" "$output"
			return
			;;
		esac
	fi
	pass "$name"
}

# ── The happy path ────────────────────────────────────────────────────────────

root=$(fixture)
cat > "$root/index.md" <<'MD'
# Index

See [detail.md](detail.md) and [its section](detail.md#a-heading).
MD
cat > "$root/detail.md" <<'MD'
# Detail

## A heading

Back to [index.md](index.md).
MD
expect 0 "$root" "a resolvable file link and anchor pass" "ok —"

# ── Broken targets ────────────────────────────────────────────────────────────

root=$(fixture)
cat > "$root/index.md" <<'MD'
# Index

See [gone.md](gone.md).
MD
expect 1 "$root" "a link to a file that does not exist fails" "gone.md"

root=$(fixture)
cat > "$root/index.md" <<'MD'
# Index

## Real heading

See [this](#no-such-heading).
MD
expect 1 "$root" "a link to an anchor that does not exist fails" "no-such-heading"

# The regression this gate was built for: the link's label wraps, so a
# line-by-line matcher never sees the link at all and reports all clear.
root=$(fixture)
cat > "$root/index.md" <<'MD'
# Index

A pane reports its own type (see [`internal/session` capability
interfaces](#internal-session-capability-interfaces)) and moves on.

### `internal/session` capability interfaces

Text.
MD
expect 1 "$root" "a broken anchor in a link whose label wraps fails" "internal-session-capability-interfaces"

# The same link, spelled the way GitHub actually slugs it, passes — so the case
# above is a verdict on the anchor, not on the line break.
root=$(fixture)
cat > "$root/index.md" <<'MD'
# Index

A pane reports its own type (see [`internal/session` capability
interfaces](#internalsession-capability-interfaces)) and moves on.

### `internal/session` capability interfaces

Text.
MD
expect 0 "$root" "a slash in a heading is deleted, not hyphenated" "ok —"

# ── Slugging ──────────────────────────────────────────────────────────────────

root=$(fixture)
cat > "$root/index.md" <<'MD'
# Index

- [browser](#launching-the-operators-browser---open)
- [route](#get-apisession-token)

### Launching the operator's browser (`--open`)

### `GET /api/session-token`
MD
expect 0 "$root" "punctuation is dropped and spaces become hyphens" "ok —"

root=$(fixture)
cat > "$root/index.md" <<'MD'
# Index

[first](#repeated) and [second](#repeated-1).

## Repeated

## Repeated
MD
expect 0 "$root" "a repeated heading gets the -1 suffix GitHub gives it" "ok —"

# ── What must NOT be resolved ─────────────────────────────────────────────────

root=$(fixture)
cat > "$root/index.md" <<'MD'
# Index

A quoted source comment is not a link this repository owns:

```go
// See [gone.md](gone.md) and [this](#nowhere).
```

Nor is [an external page](https://example.com/whatever#fragment), nor
[mail](mailto:nobody@example.com).

One real link, so this case is a verdict on what was skipped rather than on
the empty-run guard: [real.md](real.md).
MD
: > "$root/real.md"
expect 0 "$root" "fenced code and external URLs are left alone" "ok —"

root=$(fixture)
mkdir -p "$root/sub"
cat > "$root/index.md" <<'MD'
# Index

A directory link: [sub/](sub/). A non-markdown file: [model](model.als), and
one with a fragment this checker has no headings for: [part](model.als#part).
MD
: > "$root/model.als"
expect 0 "$root" "directories and non-markdown targets resolve by path alone" "ok —"

# ── Labels that name a file ───────────────────────────────────────────────────

root=$(fixture)
cat > "$root/index.md" <<'MD'
# Index

See [detail.md](detail.md).
MD
: > "$root/detail.md"
expect 0 "$root" "a label naming its own destination passes" "ok —"

root=$(fixture)
mkdir -p "$root/sub"
cat > "$root/index.md" <<'MD'
# Index

See [detail.md](sub/other.md).
MD
: > "$root/sub/other.md"
expect 1 "$root" "a label naming a file other than the destination fails" "detail.md"

# A label that merely mentions the word is not a filename claim.
root=$(fixture)
cat > "$root/index.md" <<'MD'
# Index

See [the security design](detail.md).
MD
: > "$root/detail.md"
expect 0 "$root" "a prose label is not read as a filename claim" "ok —"

# ── Fail-closed ───────────────────────────────────────────────────────────────

root=$(fixture)
expect 1 "$root" "a tree with no markdown at all is a failure, not a pass" "did NOT run"

root=$(fixture)
cat > "$root/index.md" <<'MD'
# Index

Prose with no links in it at all.
MD
expect 1 "$root" "markdown with no links examined is a failure, not a pass" "did NOT run"

root="$work/nope"
expect 1 "$root" "a root that does not exist is a failure" "not found"

# ── Summary ───────────────────────────────────────────────────────────────────

echo
if [ "$failures" -ne 0 ]; then
	echo "docs-links checker tests: $failures of $checks failed"
	exit 1
fi
echo "docs-links checker tests: $checks passed"
exit 0
