---
type: Security Requirement
title: "Task dashboard security"
description: "Fixed task collection and probe scripts, launch/resume shell handover, Claude and Codex summary runners, and the Codex permission hook."
---

# Task dashboard security

> Part of [Security overview and invariants](overview.md).

## Task collection

The task dashboard ([behavior](../behavior/task-collection.md)) collects on the panemux host and
every `ssh_connections` host without letting request, config, or host text become command text:

- `collectScript` and `attentionScript` compose once at startup from Go constants and collect
  adapter fragments (`[a-z0-9-]` process hints only). Attention collection runs only with active
  subscribers, at most once concurrently and no faster than every 5 seconds per host.
- Execution runs `sh -s` with the script on stdin (local `HOME` from `internal/homedir` and
  process-group kill on context cancellation). Hosts are dial data via the guarded
  [SSH connection API](../behavior/rest-api.md#apiconfigssh-connections) (rejecting control
  characters, never returning passwords, no `ProxyCommand`/`ProxyJump` fields).
- Remote Git inspection validates host-reported CWD with `validRemotePath` + `shellQuotePath`;
  local Git uses `sanitizeGitExecDir` + `cmd.Dir`; `gh pr view` uses discrete argv and constant
  `prTaskFields`.
- Only the collecting user's processes are listed (`ps -U "$(id -u)"`). Codex open-file probes
  retain only `rollout-*.jsonl`, read with `head`/`tail`/`grep`/`awk` (never executed). The sole
  interpolated SQLite value in `sqlite3 -readonly ~/.codex/thread_history_1.sqlite` is the stripped
  session ID restricted to lowercase hex and `-` (`stdin` `/dev/null`, double-quoted query; Go
  additionally requires a UUID).
- All task routes and `/ws/tasks/events` apply the [shared browser request guard](auth.md#shared-browser-request-guard).
  Task records write `~/.config/panemux/tasks.json` via `fileops.AtomicWrite` and render labels as
  plain text; event frames omit conversations, prompts, command lines, and PIDs.
- Host output is untrusted: `parseCollectOutput` requires `^[a-zA-Z0-9_-]+$` for session IDs and
  rechecks `validTmuxSessionName` before offering attach. `PANEMUX_PANE_ID` is read from Linux
  `/proc/<pid>/environ` or macOS `ps -E` (stripping argv, refusing multiple ` PANEMUX_PANE_ID=`
  matches, printing only `A-Za-z0-9_.-`; a single space-prefixed embedded occurrence on macOS is an
  [accepted limitation](../decisions/tasks.md#opening-an-agent-outside-tmux-through-panemux_pane_id-2026-09-262026-09-27-issues-254-263);
  Go rechecks `^[A-Za-z0-9_.-]{1,128}$` and uses it only to focus an existing pane).
- Clickable links accept only `http`/`https` from `gh` (`closingIssueLinks`) and `https`
  `url_template`s with `<num>` after host (`validAutolinkURLTemplate`, rechecked by `HttpUrlSchema`;
  identifiers restricted to `isAutolinkIDByte`).
- Unreadable state file diagnostics pass `boundedText` (valid UTF-8, control/invisible chars →
  `U+FFFD`, capped at 128/120 chars, `%q`-quoted in logs, text-only in UI).

## Task agent probe

`probeScript` (`internal/tasks/availability.go`) composes once at startup from Go constants with
`[a-z0-9-]` registry IDs for agents declaring `launch` or `resume`, running via `sh -s` under a
dedicated 5-second context. Lookup checks `command -v "$1"` then `"${SHELL:-/bin/sh}" -lc "command
-v $1"` with `/dev/null` stdin, accepting only an absolute executable regular file and **never
executing the CLI**. `parseProbeOutput` skips greeting text before `::panemux-probe v1`, accepts
only `cli <id> found`/`missing` once per ID, and requires `::end`. Probe failures do not affect
collection or drop connections; host detail strings and unknown-format examples/versions are
sanitized via `boundedText` (120/32 chars, max 3 each) and rendered as text only.

## Task launch and resume

`POST /api/tasks` and `POST /api/tasks/resume` ([behavior](../behavior/task-launch.md)) compose
constant `launchScriptTemplate` + adapter fragments (validated to set only their variable, stay in
`case`, and use declared refusal codes) and execute via literal `sh -s`:

| Placeholder | Source and check (refuse rather than escape) |
|---|---|
| agent | Registered ID with requested launch/resume adapter (no quotes) |
| mode | Constant `new` or `resume` |
| session ID | `crypto/rand` UUID (new Claude), empty (new Codex), or request UUID (resume) |
| tmux name | `task-` + 8 ID/random hex chars; must pass `validTmuxSessionName` |
| heredoc tag | 32 `crypto/rand` hex chars (`^[0-9a-f]{32}$`) |
| working directory | Must pass `session.ValidateRemotePath` ([SSH path guard](process-and-ssh.md#remote-path-arguments-ssh-working-directory)) |
| first instruction | Nonempty, ≤32 KiB, no NUL, no line matching heredoc terminator |

CWD and instruction travel in quoted heredocs (`<<'PANEMUX_CWD_<tag>'`, `<<'PANEMUX_PROMPT_<tag>'`)
and double-quoted variables; session IDs and tmux names are single-quoted.

- **Shell handover**: Only literal `claude` and `codex` are resolved to absolute executable files
  (Codex's binary dir appended **last** to `PATH` after rc). `tmux new-session -d -s <name> --`
  launches a fixed `sh -c` starter that reads and deletes the `mktemp` (`umask 077`) prompt file,
  then either runs the agent directly or invokes an absolute executable `bash`/`zsh`/`sh` via `-lic`
  with one constant command expanding `PANEMUX_LAUNCH_*` variables (immune to rc `set --`/`shift`)
  and unsetting them before exec. `sh` is resolved before rc; CWD changes via `cd -- "$4"` / `cd --
  "$1"` (never `tmux -c`, which expands `#` formats). Panemux never passes auth keys in argv, files,
  or `tmux -e`.
- **Fixed agent commands**:
  - New Claude: `exec "$2" "--session-id=$3" -- "$1"`
  - New Codex: `PATH=$PATH:${2%/*}; export PATH; exec "$2" --no-daemon -c check_for_update_on_startup=false -- "$1"`
  - Resume Claude: `claude --resume=<id>` (UUID-only, `=` form prevents option injection)
  - Resume Codex: `codex --no-daemon -c check_for_update_on_startup=false resume -- <id>` (UUID-only)
- Codex verifies `--no-daemon` in `"$bin" --help` before launch (`codex-too-old`). Resumes recollect
  the host, allow only stopped tasks using their own log's guarded CWD, serialize per `(host,
  session)` (`Service.lockResume`), and use `tmux new-window -t "=<name>:" --` only when no running
  task exists there. Only `::panemux-launch` with 8 fixed refusal codes is read; `NormalizeLabels`
  runs before launch.

## Task summaries

Summary sinks run only when `task_dashboard.summary.enabled` is true
([behavior](../behavior/task-summaries.md#summaries)). Excerpts never cross agents; invalid IDs
fail with `ErrInvalidSummary` before execution; missing CLIs report fixed messages without paths.

### Log reading and Claude runner

`transcriptScriptTemplate` runs as `sh -s` with constant byte counts and `validSessionID`
(`^[a-zA-Z0-9_-]+$`) inside `"$HOME"/.claude/projects/*/"$sid.jsonl"`, reading framed bodies by
header byte length. `newClaudeSummarizer` executes literal `claude` (no shell, empty temp dir,
excerpt on stdin, compile-time instruction after `--`) with:

- `--session-id <minted UUID>`, `--no-session-persistence`, `--setting-sources ""`,
  `--strict-mcp-config`, `--disable-slash-commands`, `--tools ""`,
  `--disallowedTools=<commandcenter.DisallowedTools()>`, `--output-format=json`,
  `--json-schema <schema>`, `--system-prompt <literal>`, `--model=haiku` (non-Haiku model IDs from
  `modelUsage` are validated before logging/display; `--bare` is omitted to preserve signed-in auth).
- Only recognized user/assistant text is extracted (excluding tool calls/results, thinking, and
  attachments): 4 KiB first message, 24 KiB recent messages (≤2 KiB each). Secrets in text are
  unmasked (hence opt-in). Output is bounded to 1 KiB summary and 10×300B remaining-work items;
  failures use fixed messages; runners have a 2-minute process-group timeout and max concurrency 2.
- `SummaryStore` writes metadata + answer (never logs/excerpts) to
  `~/.config/panemux/task-summaries.json` mode `0600` via `fileops.AtomicWrite`, renaming
  unparseable or invalid files aside unread.

### Codex summary runner and rollout reader

`internal/tasks/codex_summary.go` runs literal `codex` (no shell, empty temp dir, excerpt + fixed
instruction on stdin) with fixed argv `exec --ephemeral --skip-git-repo-check --color never
--output-schema <private file> --output-last-message <private file> -`. It intentionally preserves
the operator's auth, global instructions, MCP, hooks, and managed policies (never approving or
relaxing permissions, without guaranteeing a tool-free runtime). Stdout/stderr are discarded; the
answer file is opened without following final symlinks and nonblocking (`O_NOFOLLOW`/regular file
check under deadline, 64 KiB cap) before applying the same 1 KiB / 10×300B limits.

`buildCodexTranscriptScript` reads `~/.codex/sessions/*/*/*/rollout-*-<UUID>.jsonl` (UUID-only),
pins the collected basename/version, verifies `session_meta.id`, and `buildCodexExcerpt` keeps only
message `input_text`/`output_text` (excluding calls, reasoning, attachments, `event_msg` mirrors,
and leading AGENTS/environment wrappers). Codex never falls back to Claude.

## Codex permission hook

Embedded `internal/codexhook/codex_permission_hook.sh` runs under Codex (never panemux), extracts
only hex-and-dash `session_id`/`turn_id`, writes atomically under `umask 077` in
`~/.local/state/panemux/codex-permission/`, cleans files >7 days old there, prints nothing, and
exits `0` (never allow/deny). Go requires UUIDs when reading.

`GET`/`POST /api/tasks/codex-hook` accept only local or `ssh_connections` keys (`checkHost`),
running literal `sh -s` with fixed `codexHookReadScript` / `codexHookWriteScript` (32-hex random
quoted heredoc tag, Go-merged `hooks.json` so no `jq` is required; refuses malformed `hooks.json`;
never alters Codex `/hooks` trust). `POST /api/tasks/codex-hook/auto-update` persists hosts in
`~/.config/panemux/codex-hook.json` via `fileops.AtomicWrite`, updating at most once per host per
process only when `outdated` (never `missing`).
