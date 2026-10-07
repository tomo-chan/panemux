#!/bin/sh
set -eu
scripts_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
. "$scripts_dir/go-toolchain.sh"
panemux_go_toolchain "$scripts_dir/.."
exec /bin/sh "$@"
