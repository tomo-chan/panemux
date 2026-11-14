---
type: Specification
title: "Codex permission notifications"
description: "Permission hook installation, automatic updates, records, and answered waits."
---

# Codex permission notifications

Part of the [task dashboard](tasks.md).

## Codex permission requests

A codex `PermissionRequest` command hook (`internal/codexhook/codex_permission_hook.sh`, a POSIX `sh`
script) records each approval request, so the session shows as `wait` and the
[input-wait notification](task-events.md) fires. It is installed on each host whose codex should be
watched from the host's menu ([Opening a host](task-navigation.md#opening-a-host)), and then trusted in codex's `/hooks`
review there.

- **`Codex input notifications` in the host menu.** The menu checks the host when it opens
  ([`GET /api/tasks/codex-hook`](task-api.md#get-and-post-apitaskscodex-hook-post-apitaskscodex-hookauto-update)) and shows one of:

  | Host | Shown |
  |---|---|
  | No `~/.codex` and no `codex` on the `PATH` of `sh -s` there | Nothing (the panemux host's menu says no Codex was found) |
  | Neither the hook file nor its `hooks.json` entry | `Not installed` and `Install` |
  | The hook file as panemux embeds it, executable, and its entry in `hooks.json` | `Installed` |
  | Anything else: an older or edited hook file, one not executable, or only one of the two | `Update` |
  | A `hooks.json` that is not JSON, not an object, or whose `hooks` or `hooks.PermissionRequest` is not an object or a list, or one larger than 1 MiB (which the check does not read) | `Cannot install:` and the reason; nothing is offered |
  | The check could not run (the connection failed, the output ended early) | `Not checked:` and the reason, never taken for a host without codex |

- **What `Install` and `Update` do.** They write the script panemux embeds to
  `~/.local/bin/panemux-codex-permission-hook.sh` (mode 755) and, unless `~/.codex/hooks.json`
  already has a `PermissionRequest` hook whose `command` is that file's absolute path, add one entry
  to it — creating the file when there is none:

  ```json
  { "hooks": [{ "type": "command", "command": "/remote/home/demo/.local/bin/panemux-codex-permission-hook.sh", "timeout": 1 }] }
  ```

  Other keys/hooks and literal `&`, `<`, `>` are preserved, with two-space indentation and sorted
  top-level keys. A temporary file in `~/.codex` is prepared before copying in place, preserving
  hooks.json symlinks and mode. Preparation failure leaves the original intact; a partial copy
  keeps the temporary recovery file and reports its path. A read-only file reports
  `~/.codex/hooks.json is not writable`, remains untouched and keeps no temporary file.
  Local/SSH failures expose the shell reason; directory-creation failures name `~/.local/bin` or
  `~/.codex`. A hook file over 1 MiB shows `Update` without being read; unreadable/invalid
  hooks.json prevents writing either file. The menu reads back status and asks the operator to
  trust `/hooks`; panemux neither trusts nor checks trust. Different hook content stays `Update`
  until updated manually or automatically. See [hook safety](../security/codex-hook.md).
- **`Update automatically`.** A checkbox under the status, offered on a host showing `Not
  installed`, `Update` or `Installed`, and off on every host until turned on there. The hosts it is
  on for are kept in `~/.config/panemux/codex-hook.json` (`{"version":1,"auto_update":["","gpu-box"]}`,
  mode 600, the panemux host as `""`), so the setting survives a restart; a file panemux cannot read
  is reported (`Could not read the setting:` and the reason under the status, with the checkbox
  disabled until it can be read; `Could not save:` when it fails on saving) and never written over.
  Changing the checkbox, and `Install` or `Update`, are each disabled while the other runs. Turning
  it on on a host showing `Update` checks the hook again at once, which updates it. On such
  a host, a check that finds `Update` runs `Update` by itself: the check when the host's menu opens,
  and one more made in the background after panemux starts, on the host's first successful
  collection, so replacing panemux and restarting it updates the hook with nothing pressed. Nothing
  is installed by itself on a host showing `Not installed`, and codex itself is never updated. Each
  host is updated by itself at most once per panemux process, whatever came of it: the menu then says
  `Updated automatically. Trust it in codex's /hooks before it runs.` (the new content may need
  trusting again), or `Could not update automatically:` and the reason, which is not tried again until
  panemux restarts. `Update` can still be pressed; once it succeeds, the message is cleared. Both
  outcomes are also written to the server log.
- **By hand.** Copy `internal/codexhook/codex_permission_hook.sh` to a path of the user's on the host,
  make it executable, add an entry like the one above with that path to the host's
  `~/.codex/hooks.json`, and trust it in `/hooks`. The menu knows only its own path: a hook
  installed at another path shows `Not installed`, and `Install` adds panemux's entry beside it, so
  remove the hand-made entry then.
- **On an `ssh_connections` host.** The same check and install run on the host over its connection.
  The records stay on that host; the task collection, which runs there over the host's connection,
  reads them as it does locally. The wait and its `Input given` are the host's task's: the wait
  signature includes the host, so marking it answered leaves the same session ID's request on
  another host waiting.
- **What the hook writes.** It reads the hook's JSON on stdin and keeps only `session_id`, `turn_id`
  (hex digits and dashes) and the time, as `<session ID> <turn ID> <unix seconds>` in
  `~/.local/state/panemux/codex-permission/<session ID>` — one file per session, replaced by the next
  request. Nothing of the command or the conversation is kept. It removes records older than 7 days as
  it writes. It prints nothing and exits 0, so codex decides as it would without it.
- **What the collection reads.** Codex's live section `codex/permission` reads every record with one
  `cat`, in the full and the event stream's collection alike. A session
  waits when its newest turn is in progress, the record is of that turn, and it is not marked
  answered. The record is of the turn when the rollout's `task_started` for the turn carries the
  record's turn ID; when that line is out of the rollout's final MiB (a turn that has written more),
  when the record was made at or after the turn's start. A turn that has ended, or any
  other turn, ignores the record: the wait clears by itself when the turn is over.
- **`Input given`.** The detail panel of such a task offers `Input given`, which
  [`POST /api/tasks/input-given`](task-api.md#post-apitasksinput-given) sends. panemux keeps the answered
  requests in memory only; the next request in the same turn waits again. The pane running the task
  and the Type in pane popup on it offer the same button, on a strip below their header
  ([Codex permission requests in a pane](task-events.md#codex-permission-requests-in-a-pane)).
- **Known limits.** Codex runs the hook before it approves a request on its own too, so a request
  that needed no person can show as a wait until its turn ends or `Input given` is pressed. Codex
  records nothing when the person answers in the pane, so the wait stays until one of those.
  Without the hook nothing changes.

Host tools share [update coordination](host-tools.md#update-coordination).

## Manually verifying a permission request

1. Install the hook on the target host and trust it in that host's Codex `/hooks` review.
2. Start Codex with `sandbox_mode = "workspace-write"` and `approval_policy = "on-request"`,
   without a profile setting `approval_policy = "never"`. That policy suppresses approval prompts;
   a permitted command also needs no request. Writes inside the checkout or `/tmp` do not verify
   the hook under these defaults.
3. Ask Codex to run `curl -sI https://example.com`: default workspace-write networking is disabled,
   so it needs approval. If networking is enabled, ask it to run `touch ~/panemux-approval-test`
   outside the writable roots instead, and remove that test file afterwards. Check the actual
   host's grants and managed policy; an already permitted operation does not establish a request.
4. Confirm the task becomes Waiting for input, then press `Input given`. The wait clears; another
   request in the same turn waits again, and ending the turn clears it without that button.

See [J51 and J53](../scenarios.md#j-the-task-dashboard) for the local and SSH manual walks.
