#!/bin/sh
set -eu
panemux_runtime_scripts=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
. "$panemux_runtime_scripts/runtime-env.sh"
panemux_runtime "$panemux_runtime_scripts/.."
exec npm "$@"
