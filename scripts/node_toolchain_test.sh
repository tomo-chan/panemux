#!/bin/sh
set -eu
initial_path=$PATH
initial_node_options=${NODE_OPTIONS:-}
initial_node_options_set=${NODE_OPTIONS+x}
scripts_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir -p "$work/mock" "$work/repo" "$work/archive/node-v24.21.0-linux-x64/bin"
printf '24.21.0\n' > "$work/repo/.node-version"
export NODE_TEST_VERSION=24.21.0 NODE_TEST_SYSTEM=Linux NODE_TEST_ARCH=x86_64
export NODE_TEST_TRACE="$work/trace" NODE_TEST_ARCHIVE="$work/node.tar.gz"
cat > "$work/archive/node-v24.21.0-linux-x64/bin/node" <<'MOCK'
#!/bin/sh
case "$*" in --version) echo "v${NODE_TEST_VERSION}" ;; *npm*--version*) echo 11.19.0 ;; *) printf '%s\n' "${NODE_OPTIONS:-}" ;; esac
MOCK
printf '#!/bin/sh\necho bundled-npm\n' > "$work/archive/node-v24.21.0-linux-x64/bin/npm"
printf '#!/bin/sh\necho bundled-npx\n' > "$work/archive/node-v24.21.0-linux-x64/bin/npx"
chmod +x "$work/archive/node-v24.21.0-linux-x64/bin/"*
tar -czf "$NODE_TEST_ARCHIVE" -C "$work/archive" node-v24.21.0-linux-x64
cat > "$work/mock/node" <<'MOCK'
#!/bin/sh
echo "v${NODE_TEST_PATH_VERSION:-26.8.1}"
MOCK
cat > "$work/mock/uname" <<'MOCK'
#!/bin/sh
case "$1" in -s) echo "$NODE_TEST_SYSTEM" ;; -m) echo "$NODE_TEST_ARCH" ;; esac
MOCK
cat > "$work/mock/curl" <<'MOCK'
#!/bin/sh
printf '%s\n' "$*" >> "$NODE_TEST_TRACE"
[ "${NODE_TEST_DOWNLOAD_FAIL:-0}" = 0 ] || exit 22
url='' out=''
while [ $# -gt 0 ]; do case "$1" in -o) out=$2; shift 2 ;; https:*) url=$1; shift ;; *) shift ;; esac; done
case "$url" in
*SHASUMS256.txt) if [ "${NODE_TEST_CORRUPT:-0}" = 1 ]; then echo 'bad checksum' > "$out"; else hash=$(shasum -a 256 "$NODE_TEST_ARCHIVE" | awk '{print $1}'); printf '%s  node-v24.21.0-linux-x64.tar.gz\n' "$hash" > "$out"; fi ;;
*node-v24.21.0-linux-x64.tar.gz) sleep "${NODE_TEST_DELAY:-0}"; cp "$NODE_TEST_ARCHIVE" "$out"; if [ "${NODE_TEST_TAMPER:-0}" = 1 ]; then echo tampered >> "$out"; fi ;;
*) exit 23 ;;
esac
MOCK
chmod +x "$work/mock/"*
export PATH="$work/mock:$PATH"
run_node() { sh -c '. "$1"; panemux_node_toolchain "$2" || exit; node --version; npm --version; sh -c "node --version"' sh "$scripts_dir/node-toolchain.sh" "$work/repo"; }
expected=$(printf 'v24.21.0\nbundled-npm\nv24.21.0')
[ "$(run_node)" = "$expected" ]
[ "$(wc -l < "$NODE_TEST_TRACE" | tr -d ' ')" = 2 ]
echo 'ok newer PATH selects downloaded exact Node and bundled npm for children'
export NODE_TEST_DOWNLOAD_FAIL=1 NODE_OPTIONS='--stack-trace-limit=30'
[ "$(run_node)" = "$expected" ]
[ "$(sh -c '. "$1"; panemux_node_toolchain "$2" || exit; node probe' sh "$scripts_dir/node-toolchain.sh" "$work/repo")" = "$NODE_OPTIONS" ]
echo 'ok offline cache reuse preserves user NODE_OPTIONS'
for mode in download corrupt tamper version platform arch invalid; do
 rm -rf "$work/repo/.cache"
 export NODE_TEST_DOWNLOAD_FAIL=0 NODE_TEST_CORRUPT=0 NODE_TEST_TAMPER=0 NODE_TEST_VERSION=24.21.0 NODE_TEST_SYSTEM=Linux NODE_TEST_ARCH=x86_64
 printf '24.21.0\n' > "$work/repo/.node-version"
 case "$mode" in download) export NODE_TEST_DOWNLOAD_FAIL=1 ;; corrupt) export NODE_TEST_CORRUPT=1 ;; tamper) export NODE_TEST_TAMPER=1 ;; version) export NODE_TEST_VERSION=24.20.0 ;; platform) export NODE_TEST_SYSTEM=Windows_NT ;; arch) export NODE_TEST_ARCH=unknown ;; invalid) printf '24.21.0;bad\n' > "$work/repo/.node-version" ;; esac
 if run_node > "$work/output" 2>&1; then echo "FAIL $mode passed"; exit 1; fi
 [ ! -d "$work/repo/.cache/runtimes/node-v24.21.0-linux-x64" ]
 echo "ok $mode fails closed without publishing SDK"
done
export NODE_TEST_VERSION=24.21.0 NODE_TEST_SYSTEM=Linux NODE_TEST_ARCH=x86_64
printf '24.21.0\n' > "$work/repo/.node-version"
export NODE_TEST_DELAY=1
: > "$NODE_TEST_TRACE"
run_node > "$work/one" & one=$!
run_node > "$work/two" & two=$!
wait "$one"; wait "$two"
[ "$(cat "$work/one")" = "$expected" ]; [ "$(cat "$work/two")" = "$expected" ]
[ "$(wc -l < "$NODE_TEST_TRACE" | tr -d ' ')" = 2 ]
echo 'ok concurrent selectors install once and observe atomic complete SDK'

# Unmanaged exact SDKs with their npm/npx are reusable without network.
rm -rf "$work/repo/.cache"
export NODE_TEST_DOWNLOAD_FAIL=1 NODE_TEST_DELAY=0
: > "$NODE_TEST_TRACE"
(PATH="$work/archive/node-v24.21.0-linux-x64/bin:$PATH" run_node) > "$work/existing"
[ "$(cat "$work/existing")" = "$expected" ]; [ ! -s "$NODE_TEST_TRACE" ]
echo 'ok existing exact SDK and bundled npm need no download'
for wrong in 20.20.2 24.13.0 26.8.1; do
 rm -rf "$work/repo/.cache"
 export NODE_TEST_PATH_VERSION=$wrong NODE_TEST_DOWNLOAD_FAIL=0
 [ "$(run_node)" = "$expected" ]
 echo "ok PATH $wrong cannot change the exact version"
done
export NODE_TEST_PATH_VERSION=26.8.1
if sh "$scripts_dir/check-node.sh" > "$work/output" 2>&1; then echo 'FAIL direct npm guard accepted wrong Node'; exit 1; fi
PATH="$work/archive/node-v24.21.0-linux-x64/bin:$PATH" npm_node_execpath="$work/mock/node" sh "$scripts_dir/check-node.sh" > "$work/output" 2>&1 && { echo 'FAIL npm parent mismatch passed'; exit 1; }
echo 'ok direct npm guard rejects mismatched shell and npm parent Node'
# Corrupt cached SDKs must not silently fall back or be overwritten.
export NODE_TEST_VERSION=24.20.0
if run_node > "$work/output" 2>&1; then echo 'FAIL corrupt cache passed'; exit 1; fi
grep -q 'cached SDK is unusable' "$work/output"
echo 'ok corrupt cached SDK fails closed'

# Exercise the public npm forwarding contract with the installed frontend tools.
PATH=$initial_path
export PATH
if [ "$initial_node_options_set" = x ]; then
 NODE_OPTIONS=$initial_node_options
 export NODE_OPTIONS
else
 unset NODE_OPTIONS
fi
output=$("$scripts_dir/npm.sh" --prefix "$scripts_dir/../frontend" run test -- --help)
printf '%s\n' "$output" | grep -q 'Usage:'
echo 'ok npm run forwards additional arguments instead of running the suite'
