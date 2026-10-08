#!/bin/sh
# Source this file, then call panemux_go_toolchain with the repository root.
# The go directive is an exact development/CI contract, not a minimum version.
panemux_go_toolchain() {
 panemux_go_version=$(awk '$1 == "go" { if (NF != 2 || seen++) exit 1; version=$2 } END { if (seen != 1 || version !~ /^[0-9]+\.[0-9]+\.[0-9]+$/) exit 1; print version }' "$1/go.mod") || {
  echo 'go-toolchain: go.mod must contain one exact major.minor.patch go directive' >&2
  return 1
 }
 GOTOOLCHAIN="go$panemux_go_version"
 export GOTOOLCHAIN
 panemux_go_info=$(cd "$1" && go env GOVERSION GOROOT) || {
  echo "go-toolchain: cannot start required $GOTOOLCHAIN" >&2
  return 1
 }
 panemux_go_actual=$(printf '%s\n' "$panemux_go_info" | sed -n '1p')
 panemux_go_root=$(printf '%s\n' "$panemux_go_info" | sed -n '2p')
 if [ "$panemux_go_actual" != "$GOTOOLCHAIN" ] || [ ! -x "$panemux_go_root/bin/go" ]; then
  echo "go-toolchain: required $GOTOOLCHAIN, got $panemux_go_actual or unusable GOROOT" >&2
  return 1
 fi
 # Include gofmt and ensure tools that spawn bare go inherit the selected SDK.
 PATH="$panemux_go_root/bin:$PATH"
 export PATH
}
