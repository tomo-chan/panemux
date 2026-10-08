#!/bin/sh
# panemux_runtime_scripts is assigned by the caller to this checkout's scripts.
. "$panemux_runtime_scripts/go-toolchain.sh"
. "$panemux_runtime_scripts/node-toolchain.sh"
panemux_runtime() {
 panemux_go_toolchain "$1" || return 1
 panemux_node_toolchain "$1" || return 1
}
