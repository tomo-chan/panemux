#!/bin/sh
# Tier 2 of issue #168's model-checking split: run TLC over the TLA+ specs in
# spec/agentboard/, then export each one's state graph as the flat transition
# table Tier 1 replays the Go implementation against.
#
# Two failures this is here to catch, and they are different:
#
#   1. The SPEC is wrong -- an invariant or temporal property does not hold
#      over the full state space. TLC reports it and this script exits 1.
#   2. The spec and the COMMITTED TABLE have drifted. The table is what Tier 1
#      actually checks the Go code against, so a spec change that never reaches
#      it silently weakens every hermetic run. This script regenerates the table
#      and diffs it against the committed copy.
#
# Outside `make check` for the reason docs/agent-board.md's agmsg contract Tier 2
# is: it needs an external toolchain (a JDK and tla2tools.jar) that
# `make install-deps` does not install, so a hermetic local run must stay green
# without it.
#
#   TLA_TOOLS_JAR=/path/to/tla2tools.jar make model-check
#   TLA_TOOLS_JAR=/path/to/tla2tools.jar make model-check-write   # update the table
set -eu

write=0
for arg in "$@"; do
  case "$arg" in
    --write) write=1 ;;
    *) echo "model-check: unknown argument: $arg" >&2; exit 2 ;;
  esac
done

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
spec_dir="$repo_root/spec/agentboard"
table_dir="$repo_root/internal/board/testdata"

jar="${TLA_TOOLS_JAR:-}"
if [ -z "$jar" ] || [ ! -f "$jar" ]; then
  cat >&2 <<'MSG'
model-check: TLA_TOOLS_JAR must point at a tla2tools.jar.

  curl -fsSL -o /tmp/tla2tools.jar \
    https://github.com/tlaplus/tlaplus/releases/download/v1.7.4/tla2tools.jar
  TLA_TOOLS_JAR=/tmp/tla2tools.jar make model-check

This is Tier 2: it is deliberately not part of `make check`, so a checkout
without a JDK or the jar still gets a green hermetic run.
MSG
  exit 2
fi

if ! command -v java >/dev/null 2>&1; then
  echo "model-check: java not found on PATH; TLC needs a JDK" >&2
  exit 2
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

status=0
found=0
for spec in "$spec_dir"/*.tla; do
  [ -f "$spec" ] || continue
  found=1
  name=$(basename "$spec" .tla)
  cfg="$spec_dir/$name.cfg"
  if [ ! -f "$cfg" ]; then
    echo "model-check: $name.tla has no matching $name.cfg" >&2
    status=1
    continue
  fi

  echo "== TLC: $name =="
  dot="$work/$name.dot"
  # -dump dot,actionlabels writes the whole reachable state graph with each
  # edge labelled by the action that produced it. That labelling is what makes
  # the export a TRANSITION table rather than just a set of reachable states.
  ( cd "$spec_dir" && java -XX:+UseParallelGC -cp "$jar" tlc2.TLC \
      -workers auto -cleanup -metadir "$work/$name-meta" \
      -dump dot,actionlabels "$dot" \
      -config "$name.cfg" "$name.tla" )

  table="$table_dir/$(printf '%s' "$name" | tr '[:upper:]' '[:lower:]')-transitions.json"
  generated="$work/$name.json"
  python3 "$repo_root/scripts/tla_transitions.py" \
    --dot "$dot" \
    --spec "spec/agentboard/$name.tla" \
    --config "spec/agentboard/$name.cfg" \
    --out "$generated"

  if [ "$write" -eq 1 ]; then
    cp "$generated" "$table"
    echo "model-check: wrote $table"
    continue
  fi

  if [ ! -f "$table" ]; then
    echo "model-check: $table does not exist; run 'make model-check-write'" >&2
    status=1
    continue
  fi
  if ! diff -u "$table" "$generated"; then
    cat >&2 <<MSG
model-check: $table has drifted from $name.tla.
Tier 1 replays the Go implementation against the COMMITTED table, so a spec
change that never reaches it weakens every hermetic run. Regenerate and commit:

  TLA_TOOLS_JAR=$jar make model-check-write
MSG
    status=1
  else
    echo "model-check: $table matches $name.tla"
  fi
done

if [ "$found" -eq 0 ]; then
  echo "model-check: no .tla specs found under spec/agentboard" >&2
  exit 1
fi

exit "$status"
