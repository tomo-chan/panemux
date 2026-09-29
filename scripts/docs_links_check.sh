#!/bin/sh
#
# Gate G0's documentation half: every relative link in this repository's
# markdown must reach something that exists, and its label must not name a
# different file than it opens.
#
# This exists because a manual pass did not hold. PR #248 split five documents
# into directories, stated that every link and anchor across all 53 markdown
# files had been checked, and shipped two broken anchors in the paragraph
# immediately below that claim — plus 41 links whose visible text still named
# the file the content used to live in. The repository gates mutation, per-block
# coverage, red-check and TLC, and left the one property a documentation split
# rests on to someone remembering to look.
#
# Three things are checked, and the third is the one a link checker usually
# leaves out:
#
#   1. The path resolves. A relative target must name a file or directory that
#      exists, resolved against the linking file's own directory.
#   2. The anchor resolves. A `#fragment` on a markdown target must match a
#      heading in that file, slugged the way GitHub slugs it.
#   3. The label does not lie. `[security.md](security/auth.md#...)` tells a
#      reader to open one file and opens another. After a split that moves
#      sections between files, this is the failure mode: the rewrite changes
#      targets and leaves the labels behind, and nothing about the rendered
#      page looks wrong.
#
# Hermetic, needs nothing but sh/awk/find, and runs in under a second, so it
# sits inside `make check`.
#
# Usage:
#   make check-docs-links
#   scripts/docs_links_check.sh path/to/tree   # used by the tests

set -u

repo_root=$(git rev-parse --show-toplevel 2> /dev/null || pwd)
root=${1:-$repo_root}

if [ ! -d "$root" ]; then
	echo "docs-links: $root not found"
	exit 1
fi

root=$(CDPATH='' cd -- "$root" && pwd)

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Everything that exists, so the awk program can answer "does this path
# resolve" without a `test -e` subprocess per link. Directories are included:
# `[docs/security/](security/)` is a legitimate link to a directory listing.
#
# -prune, not -not -path: a filter still descends into frontend/node_modules,
# which is tens of thousands of stat calls in a target that runs on every push.
(
	cd "$root" || exit 1
	find . \( -name node_modules -o -name .git -o -name dist \) -prune -o -print
) | sed 's|^\./||' > "$tmp/exists"

grep '\.md$' "$tmp/exists" > "$tmp/md" || :

if [ ! -s "$tmp/md" ]; then
	echo "docs-links: no markdown files under $root — the check did NOT run."
	exit 1
fi

md_count=$(wc -l < "$tmp/md" | tr -d ' ')

echo "docs-links: checking every relative link in $root's markdown"

# The whole check is one awk program reading files itself, rather than awk's
# own file iteration, because a link's label may wrap across a newline — that
# is exactly what hid the two broken anchors #248 shipped — so each file has to
# be matched as one string, not line by line.
awk -v root="$root" -v existsfile="$tmp/exists" -v mdfile="$tmp/md" '
# GitHub slugs a heading by lowercasing it, deleting everything that is not a
# letter, digit, space, underscore or hyphen, and replacing spaces with
# hyphens. Deleting rather than replacing is the part that is easy to get
# wrong and the part that bit #248: `internal/session` becomes
# "internalsession", never "internal-session". This is byte-based, so a
# heading with non-ASCII letters would slug differently here than on GitHub;
# this repository has none, and a wrong answer there fails closed (a reported
# finding), not open.
function slug(t,   s) {
	s = tolower(t)
	gsub(/[^a-z0-9 _-]/, "", s)
	gsub(/ /, "-", s)
	return s
}

function dirname(p,   i) {
	i = length(p)
	while (i > 0 && substr(p, i, 1) != "/") i--
	return i > 0 ? substr(p, 1, i - 1) : ""
}

function basename(p,   i) {
	i = length(p)
	while (i > 0 && substr(p, i, 1) != "/") i--
	return i > 0 ? substr(p, i + 1) : p
}

# Collapse "." and ".." without touching the filesystem, so the result can be
# looked up in the set of existing paths.
function norm(p,   n, parts, i, k, out, s) {
	n = split(p, parts, "/")
	k = 0
	for (i = 1; i <= n; i++) {
		if (parts[i] == "" || parts[i] == ".") continue
		if (parts[i] == "..") { if (k > 0) k--; continue }
		out[++k] = parts[i]
	}
	s = ""
	for (i = 1; i <= k; i++) s = s (i > 1 ? "/" : "") out[i]
	return s
}

# A fenced block is blanked rather than dropped so reported line numbers still
# match the file. Its contents are not this repository s links: the code blocks
# in docs/ quote Go comments that carry real paths, and resolving those would
# report the same finding twice, once for the code and once for the comment.
function readfile(f,   path, line, out, fenced) {
	path = root "/" f
	out = ""
	fenced = 0
	while ((getline line < path) > 0) {
		if (line ~ FENCE) { fenced = 1 - fenced; out = out "\n"; continue }
		if (fenced) { out = out "\n"; continue }
		out = out line "\n"
	}
	close(path)
	return out
}

function collect(f,   text, n, lines, i, h, a, c) {
	text = readfile(f)
	n = split(text, lines, "\n")
	for (i = 1; i <= n; i++) {
		if (lines[i] !~ /^#+ /) continue
		h = lines[i]
		sub(/^#+ /, "", h)
		a = slug(h)
		c = count[f SUBSEP a]++
		anchor[f SUBSEP (c == 0 ? a : a "-" c)] = 1
	}
}

function lineof(text, pos,   pre) {
	pre = substr(text, 1, pos - 1)
	return gsub(/\n/, "\n", pre) + 1
}

function report(f, line, msg) {
	findings[++nfindings] = sprintf("  %s:%d %s", f, line, msg)
}

function check(f,   text, rest, offset, m, i, label, target, p, path, anc,
                   dir, resolved, flat, ln, lbl) {
	text = readfile(f)
	rest = text
	offset = 0
	dir = dirname(f)
	while (match(rest, LINK)) {
		m = substr(rest, RSTART, RLENGTH)
		ln = lineof(text, offset + RSTART)
		offset += RSTART + RLENGTH - 1
		rest = substr(rest, RSTART + RLENGTH)

		i = index(m, "](")
		label = substr(m, 2, i - 2)
		target = substr(m, i + 2, length(m) - i - 2)

		# Not this repository s to resolve. A site-absolute target is skipped
		# for the same reason: it is resolved by whatever serves the page, not
		# by this tree.
		if (target ~ /^(https?:\/\/|mailto:|\/)/) continue

		p = index(target, "#")
		if (p > 0) {
			path = substr(target, 1, p - 1)
			anc = substr(target, p + 1)
		} else {
			path = target
			anc = ""
		}

		examined++

		if (path == "") {
			resolved = f
		} else {
			resolved = norm(dir == "" ? path : dir "/" path)
			if (!(resolved in exists)) {
				report(f, ln, "links to " target ", which does not exist")
				continue
			}
		}

		if (anc != "" && resolved ~ /\.md$/ && (resolved in scanned)) {
			if (!((resolved SUBSEP anc) in anchor)) {
				report(f, ln, "links to " target ", and that file has no such heading")
				continue
			}
		}

		# A label that names a markdown file is a claim about where the link
		# goes. Prose labels are not: only a token that looks like a filename
		# is read as one.
		if (path != "") {
			flat = label
			gsub(/\n/, " ", flat)
			if (match(flat, /[A-Za-z0-9_.-]+\.md/)) {
				lbl = substr(flat, RSTART, RLENGTH)
				if (lbl != basename(path)) {
					report(f, ln, "is labelled " lbl " but opens " target)
				}
			}
		}
	}
}

BEGIN {
	# Both patterns are built as strings rather than written as /regex/
	# literals so the bracket expressions can hold a real tab and newline.
	# `\t` and `\n` inside a bracket expression are undefined in a POSIX ERE —
	# gawk and mawk accept them, the awk macOS ships is the one that might
	# not — but awk s STRING lexer turns them into the characters themselves
	# before the regex engine ever sees them, which every awk does the same
	# way. [:space:] would have been the other answer and has the same
	# portability question.
	FENCE = "^[ \t]*```"
	LINK = "\\[[^]]*\\]\\([^) \t\n]+\\)"

	while ((getline line < existsfile) > 0) exists[line] = 1
	close(existsfile)

	nmd = 0
	while ((getline line < mdfile) > 0) { md[++nmd] = line; scanned[line] = 1 }
	close(mdfile)

	for (i = 1; i <= nmd; i++) collect(md[i])
	for (i = 1; i <= nmd; i++) check(md[i])

	for (i = 1; i <= nfindings; i++) print findings[i]

	print "__EXAMINED__" examined
	exit nfindings > 0 ? 1 : 0
}
' < /dev/null > "$tmp/out"
status=$?

examined=$(sed -n 's/^__EXAMINED__//p' "$tmp/out")
grep -v '^__EXAMINED__' "$tmp/out" || :

if [ "$status" -ne 0 ]; then
	echo
	echo "docs-links: the links above do not resolve, or name a file they do not open."
	echo "            A label is part of the link: a reader told to open security.md"
	echo "            and landed in security/auth.md has been misdirected as surely"
	echo "            as by a 404."
	exit 1
fi

# The fail-open this check has, and the one most likely to happen by accident.
# A tree of markdown with no links examined means the link pattern stopped
# matching — a change to how links are written, or an awk whose bracket
# expressions behave differently — and the script's way of saying "all clear"
# is indistinguishable from its way of saying "I found nothing to look at".
if [ -z "$examined" ] || [ "$examined" -eq 0 ]; then
	echo "docs-links: $md_count markdown files, and not one relative link was examined."
	echo "            The link pattern must have stopped matching — the check did NOT run."
	exit 1
fi

echo "  ok — $examined relative links across $md_count markdown files resolve"
exit 0
