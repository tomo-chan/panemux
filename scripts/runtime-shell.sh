#!/bin/sh
set -eu
panemux_runtime_scripts=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
. "$panemux_runtime_scripts/runtime-env.sh"
panemux_runtime "$panemux_runtime_scripts/.."
if [ "${1:-}" = --run ]; then
 shift
 exec "$@"
fi
exec /bin/sh "$@"
