#!/bin/sh
# Source and call with the checkout root. Downloads use only official Node URLs.
panemux_node_sdk_usable() {
 [ -x "$1/node" ] && [ -x "$1/npm" ] && [ -x "$1/npx" ] || return 1
 [ "$("$1/node" --version 2>/dev/null)" = "v$panemux_node_version" ] || return 1
 "$1/node" "$1/npm" --version >/dev/null 2>&1
}

panemux_node_download() (
 set -eu
 cache=$1
 destination=$2
 archive_name=$3
 mkdir -p "$cache" || exit 1
 lock="$destination.lock"
 attempts=0
 while ! mkdir "$lock" 2>/dev/null; do
  if [ -d "$destination" ]; then
   panemux_node_sdk_usable "$destination/bin" || exit 1
   exit 0
  fi
  attempts=$((attempts + 1))
  if [ "$attempts" -ge 30 ]; then
   echo 'node-toolchain: runtime install lock unavailable; inspect the lock before retrying' >&2
   exit 1
  fi
  sleep 1
 done
 stage=''
 trap '[ -z "$stage" ] || rm -rf "$stage"; rmdir "$lock"' EXIT
 trap 'exit 1' HUP INT TERM
 # Another installer may have completed before we acquired the lock.
 if [ -d "$destination" ]; then
  panemux_node_sdk_usable "$destination/bin" || exit 1
  exit 0
 fi
 stage=$(mktemp -d "$cache/.node-install.XXXXXX") || exit 1
 base="https://nodejs.org/dist/v$panemux_node_version"
 curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location --connect-timeout 15 --max-time 180 "$base/SHASUMS256.txt" -o "$stage/checksums" || exit 1
 checksum=$(awk -v name="$archive_name.tar.gz" '$2 == name { if (seen++) exit 1; hash=$1 } END { if (seen != 1 || length(hash) != 64 || hash !~ /^[0-9a-f]+$/) exit 1; print hash }' "$stage/checksums") || {
  echo 'node-toolchain: missing or invalid official archive checksum' >&2
  exit 1
 }
 curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location --connect-timeout 15 --max-time 180 "$base/$archive_name.tar.gz" -o "$stage/archive.tar.gz" || exit 1
 if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$stage/archive.tar.gz" | awk '{print $1}')
 else
  actual=$(shasum -a 256 "$stage/archive.tar.gz" | awk '{print $1}')
 fi
 [ "$actual" = "$checksum" ] || {
  echo 'node-toolchain: archive checksum mismatch' >&2
  exit 1
 }
 tar -xzf "$stage/archive.tar.gz" -C "$stage" || exit 1
 panemux_node_sdk_usable "$stage/$archive_name/bin" || {
  echo 'node-toolchain: downloaded SDK does not start with the required version and npm' >&2
  exit 1
 }
 mv "$stage/$archive_name" "$destination" || exit 1
)

panemux_node_toolchain() {
 panemux_node_version=$(awk 'NR == 1 && /^[0-9]+\.[0-9]+\.[0-9]+$/ { version=$0; next } { bad=1 } END { if (bad || !version) exit 1; print version }' "$1/.node-version") || {
  echo 'node-toolchain: .node-version must contain one exact major.minor.patch version' >&2
  return 1
 }
 case "$(uname -s)" in
  Darwin) panemux_node_os=darwin ;;
  Linux) panemux_node_os=linux ;;
  *) echo 'node-toolchain: unsupported platform; Windows uses WSL2' >&2; return 1 ;;
 esac
 case "$(uname -m)" in
  x86_64|amd64) panemux_node_arch=x64 ;;
  arm64|aarch64) panemux_node_arch=arm64 ;;
  *) echo 'node-toolchain: unsupported architecture' >&2; return 1 ;;
 esac
 panemux_node_archive="node-v$panemux_node_version-$panemux_node_os-$panemux_node_arch"
 panemux_node_destination="$1/.cache/runtimes/$panemux_node_archive"
 panemux_node_bin=''
 if [ -d "$panemux_node_destination" ]; then
  panemux_node_sdk_usable "$panemux_node_destination/bin" || {
   echo 'node-toolchain: cached SDK is unusable; inspect it before retrying' >&2
   return 1
  }
  panemux_node_bin="$panemux_node_destination/bin"
 fi
 if [ -z "$panemux_node_bin" ]; then
  panemux_node_on_path=$(command -v node 2>/dev/null || true)
  if [ -n "$panemux_node_on_path" ] && panemux_node_sdk_usable "$(dirname -- "$panemux_node_on_path")"; then
   panemux_node_bin=$(CDPATH='' cd -- "$(dirname -- "$panemux_node_on_path")" && pwd)
  elif panemux_node_sdk_usable "${NVM_DIR:-${HOME:-}/.nvm}/versions/node/v$panemux_node_version/bin"; then
   panemux_node_bin="${NVM_DIR:-${HOME:-}/.nvm}/versions/node/v$panemux_node_version/bin"
  else
   panemux_node_download "$1/.cache/runtimes" "$panemux_node_destination" "$panemux_node_archive" || {
    echo "node-toolchain: cannot obtain required Node $panemux_node_version" >&2
    return 1
   }
   panemux_node_bin="$panemux_node_destination/bin"
  fi
 fi
 PATH="$panemux_node_bin:$PATH"
 export PATH
}
