#!/bin/sh
#
# Fails when a pull request changes something the documentation screenshots
# show but does not retake them.
#
# Given a base revision, it reads the changed files from
# `git diff --name-only --no-renames <base> HEAD` (what the CI job does);
# without one, from stdin, one path per line. Rename detection stays off
# because `--name-only` would otherwise print only a moved file's new path,
# and a component moved out of components/ and restyled in the same pull
# request would pass unseen. A change "the images show" is a
# non-test file under the paths below; "retaking them" is any change under
# docs/images/, which is where `make screenshots` writes.
#
# The trigger is deliberately the presentation layer only: components, the
# app shell, stylesheets, the page shell, and the capture itself. hooks/,
# schemas/ and utils/ can change what is on screen too, but far more often do
# not, and a gate that fires on changes it has no opinion about gets bypassed
# (design principle 4). DEVELOPMENT.md's rule still applies to them.
#
# Run with: sh scripts/screenshots_check.sh <base>

set -eu

if [ $# -gt 0 ]; then
	changed=$(git diff --name-only --no-renames "$1" HEAD)
else
	changed=$(cat)
fi

shown=$(printf '%s\n' "$changed" \
	| grep -E '^(frontend/src/components/|frontend/src/styles/|frontend/src/App\.tsx$|frontend/index\.html$|frontend/screenshots/)' \
	| grep -vE '\.test\.(ts|tsx)$' || true)

if [ -z "$shown" ]; then
	echo "Nothing the documentation screenshots show changed."
	exit 0
fi

if printf '%s\n' "$changed" | grep -q '^docs/images/'; then
	echo "docs/images/ was updated alongside:"
	printf '%s\n' "$shown" | sed 's/^/  /'
	exit 0
fi

echo "This pull request changes what the documentation screenshots show but does not retake them:"
printf '%s\n' "$shown" | sed 's/^/  /'
echo
echo "Run 'make screenshots', look at the images it writes to docs/images/, and commit them."
echo
echo "If this change does not alter anything the screenshots show, apply the 'screenshots-exempt' label."
exit 1
