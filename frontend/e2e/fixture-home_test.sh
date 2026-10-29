#!/bin/sh
# Exercise the real fixture wrappers with a stand-in Go process, on success and failure.
set -eu
here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-fixture-home-test.XXXXXX") || exit 1
trap 'find "$work" -depth -delete' EXIT
mkdir -p "$work/repo/frontend/e2e/fixtures/agmsg/scripts" "$work/repo/scripts" "$work/bin" "$work/tmp"
cp "$here/run-panemux-e2e.sh" "$here/run-panemux-agent-board-e2e.sh" "$here/stand-ins.sh" "$work/repo/frontend/e2e/"
cp "$here/fixtures/agmsg/scripts/"*.sh "$work/repo/frontend/e2e/fixtures/agmsg/scripts/"
printf 'panemux_runtime() { return 0; }\n' > "$work/repo/scripts/runtime-env.sh"
printf '#!/bin/sh\nexit 0\n' > "$work/bin/make"
cat > "$work/bin/go" <<'SH'
#!/bin/sh
case "$1:${2:-}" in
 env:GOPATH) printf /tmp/fixture-gopath ;;
 env:GOMODCACHE) printf /tmp/fixture-modcache ;;
 *) printf '%s\n%s\n' "$HOME" "$GOCACHE" > "$FIXTURE_RECORD"
    if [ "$FIXTURE_EXIT" = hold ]; then
      trap 'exit 0' TERM INT
      while :; do sleep 1; done
    fi
    exit "$FIXTURE_EXIT" ;;
esac
SH
chmod +x "$work/bin/go" "$work/bin/make"
for config in agent-board.yml agent-board-agmsg.yml command-center.yml; do
 printf 'fixture\n' > "$work/repo/frontend/e2e/$config"
done
for wrapper in run-panemux-e2e.sh run-panemux-agent-board-e2e.sh; do
 for code in 0 7; do
  status=0
  env -u E2E_HOME PATH="$work/bin:$PATH" TMPDIR="$work/tmp" FIXTURE_RECORD="$work/record" FIXTURE_EXIT="$code" \
   sh "$work/repo/frontend/e2e/$wrapper" agent-board.yml 4174 > "$work/output" 2>&1 || status=$?
  [ "$status" -eq "$code" ] || { cat "$work/output"; echo "wrong exit status: $status"; exit 1; }
  home=$(sed -n '1p' "$work/record")
  cache=$(sed -n '2p' "$work/record")
  [ ! -e "$home" ] || { echo "fake HOME was not cleaned: $wrapper ($code)"; exit 1; }
  [ "$cache" = "$work/tmp/panemux-playwright-gocache" ] || { echo 'build cache moved into fake HOME'; exit 1; }
 done
done
echo 'fixture-home: success/failure cleanup and shared GOCACHE pass'

if command -v python3 >/dev/null 2>&1; then
    (cd "$here/../.." && scripts/runtime-shell.sh --run node --experimental-strip-types --input-type=module -e '
import config from "./frontend/playwright.config.ts";
process.stdout.write(JSON.stringify(config.webServer));
') > "$work/servers.json"
    python3 "$here/fixture-home-shutdown_test.py" "$work"
else
    echo 'skip fixture process-group shutdown checks: python3 unavailable'
fi
