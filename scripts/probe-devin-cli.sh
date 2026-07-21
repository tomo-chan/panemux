#!/usr/bin/env bash
# scripts/probe-devin-cli.sh
#
# One-shot investigation script for adding Devin CLI (https://docs.devin.ai/cli)
# support to panemux's interactive-agent detection (see internal/session/local.go).
#
# Run this locally where Devin CLI is installed and authenticated. It only reads
# process lists, file listings, and JSON *key names* (never file contents, never
# your actual session/task text), so its output is safe to paste back into the
# panemux PR/issue discussion. It does not modify anything.
#
# Some checks require a Devin CLI session already running interactively in
# another terminal; this script prints exact instructions for those instead of
# trying to automate typing into Devin's TUI.

set -uo pipefail

section() { printf '\n=== %s ===\n' "$1"; }

section "1. Binary and version"
which devin || echo "devin not found on PATH"
devin --version 2>&1 || true

section "2. Running devin process(es)"
DEVIN_PIDS=$(pgrep -f '(^|/)devin( |$)' || true)
if [ -z "$DEVIN_PIDS" ]; then
  echo "No running devin process found."
  echo "Start an interactive session in another terminal first (just 'devin' in a scratch git repo), then re-run this script."
else
  # shellcheck disable=SC2046
  ps -o pid,ppid,command -p $(echo "$DEVIN_PIDS" | tr '\n' ',' | sed 's/,$//')
fi

section "3. Live cwd of running devin process(es) (macOS)"
for pid in $DEVIN_PIDS; do
  echo "-- pid $pid --"
  lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | tail -1 || echo "  (lsof failed or unsupported)"
done
echo
echo "MANUAL STEP: inside the running devin session, ask it to cd into a sibling"
echo "git worktree and list files there, then re-run just this section (2-3) to"
echo "see whether the devin PID's own cwd changed, or whether a *child* PID shows"
echo "the new cwd instead. Note whichever PID actually moved."

section "4. Approval prompt text (manual)"
echo "MANUAL STEP: inside the running devin session, ask it to do something that"
echo "needs write/exec approval under normal permission mode, e.g. in a scratch dir:"
echo '  "create a file named devin_probe_test.txt in the current directory"'
echo "Copy the exact prompt text it shows (all menu lines) and paste it back —"
echo "this is UI chrome text only, not sensitive."

section "5. Non-interactive (-p/--print) mode"
if command -v timeout >/dev/null 2>&1; then
  TIMEOUT_CMD="timeout 15"
elif command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_CMD="gtimeout 15"
else
  TIMEOUT_CMD=""
  echo "(no timeout/gtimeout found — this may hang waiting for approval input; Ctrl+C if so)"
fi
echo "Running: devin -p \"list the files in the current directory\""
$TIMEOUT_CMD devin -p "list the files in the current directory" 2>&1 | tail -20

section "6. Config file discovery (keys only, never values)"
for f in "$HOME/.config/devin/config.json" "./.devin/config.json" "./.devin/config.local.json"; do
  if [ -f "$f" ]; then
    echo "found: $f"
    if command -v jq >/dev/null 2>&1; then
      jq -r 'paths(scalars) as $p | ($p | join("."))' "$f" 2>/dev/null | sed 's/^/  key: /' || echo "  (jq parse failed)"
    else
      echo "  (install jq to list keys; deliberately not printing raw content)"
    fi
  else
    echo "not found: $f"
  fi
done

section "7. Session/transcript file discovery (paths + JSON key names only)"
CANDIDATE_DIRS=(
  "$HOME/.config/devin"
  "$HOME/.devin"
  "$HOME/Library/Application Support/devin"
  "$HOME/.local/share/devin"
)
for d in "${CANDIDATE_DIRS[@]}"; do
  if [ -d "$d" ]; then
    echo "-- $d --"
    find "$d" -type f 2>/dev/null
  fi
done
for pid in $DEVIN_PIDS; do
  echo "-- open files for pid $pid (paths only) --"
  lsof -p "$pid" 2>/dev/null | awk '{print $NF}' | grep -iE 'json|log|session|\.devin' | sort -u
done
echo
echo "If any .json/.jsonl files were found above, run ONE of these manually to see"
echo "its schema without printing content:"
echo '  jq -r "keys" <file>            # single JSON object'
echo '  head -1 <file> | jq -r "keys"  # JSONL'

section "8. Session list (structure only)"
devin list 2>&1 | head -5
echo "(showing at most 5 lines — not dumping full session titles)"
if command -v jq >/dev/null 2>&1; then
  devin list --json 2>&1 | jq -r '.[0] | keys' 2>/dev/null || echo "(no --json output or jq parse failed)"
fi

section "Done"
echo "Paste the full output above, plus the manual results from sections 3 and 4,"
echo "back into the panemux discussion."
