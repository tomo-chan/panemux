---
type: Security Requirement
title: "Security: Codex permission hook"
description: "Data-only permission records and guarded fixed-script hook installation."
---

# Security: Codex permission hook

See the [shared rules and boundary map](command-execution.md).

The embedded constant `internal/codexhook/codex_permission_hook.sh` runs under Codex, never
panemux ([behavior](../behavior/task-codex-permissions.md#codex-permission-requests)). From Codex
stdin it takes only hex-and-dash `session_id`/`turn_id`, which cannot escape
`~/.local/state/panemux/codex-permission/` or carry the requested command. It writes via temporary
file and rename under umask 077, deletes only that directory's files older than 7 days
(`find "$dir" -type f -mtime +7 -exec rm -f {} +`), prints nothing and exits 0, never allow/deny.
The constant `codex/permission` collect fragment only cats records; Go requires both IDs as UUIDs.

`GET` and `POST /api/tasks/codex-hook` accept only the panemux host or an `ssh_connections` key
(`hosttools.Service.checkHost`), never a browser-supplied path, command or content. Through the
adapter and shared host execution port, both use literal `sh -s` with fixed scripts on stdin,
locally and remotely:

- `codexHookReadScript` checks `~/.codex` and `codex` on PATH, then prints HOME, the hook and
  `~/.codex/hooks.json` with byte counts.
- `codexHookWriteScript` fills only a 32-hex crypto/rand heredoc tag, fixed `yes`/`no`, the embedded
  hook and new `hooks.json`. Quoted heredoc terminators prevent expansion. Go's two-space JSON
  encoding (`registerCodexHook`) leaves each line indented or containing a bracket or quoted string,
  never a terminator. Already-registered hooks never send the host's own JSON back. Paths are
  constants under HOME; the host's own HOME enters the command as a JSON string in its own file,
  run by Codex as that user.
- If panemux cannot read `hooks.json` as Codex does, it writes neither file. It never changes
  Codex trust; the person reviews the hook in `/hooks`.
- `POST /api/tasks/codex-hook/auto-update` stores only the host in
  `~/.config/panemux/codex-hook.json` through `fileops.AtomicWrite`. Automatic checks/installs use
  the same scripts, at most once per host per process, only for a listed host found `outdated`,
  never one found `missing`.
