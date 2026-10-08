#!/bin/sh
set -eu
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
version=$(cat "$root/.node-version")
actual=$(node --version)
parent="$actual"
if [ -n "${npm_node_execpath:-}" ]; then parent=$("$npm_node_execpath" --version); fi
if [ "$actual" != "v$version" ] || [ "$parent" != "v$version" ]; then
 echo "Node $version is required for npm install/ci. Run make install-deps or scripts/npm.sh from frontend." >&2
 exit 1
fi
