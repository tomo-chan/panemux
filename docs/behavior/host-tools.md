---
type: Specification
title: "Host tool management"
description: "Host tool update coordination and manual and automatic agmsg management."
---

# Host tool management

Part of the [task dashboard](tasks.md).

## Update coordination

Tool management uses the shared host execution port. Successful task collections notify it;
[Codex hook routes](task-api.md#get-and-post-apitaskscodex-hook-post-apitaskscodex-hookauto-update)
remain the host menu contract.

Tool updates announce a pause for that tool on that host before waiting for active management
checks to finish. New consumer calls and overlapping updates return an updating error immediately;
status checks report `unknown` with that reason instead of invoking the tool. Other tools and
hosts remain usable. Once drained, management runs any adapter-defined prerequisites
and installs only if they all pass. A failed prerequisite reports its name and reason and asks
the operator to set it up; management never installs dependencies. Success, failure and cancellation
all lift the pause. Codex hook's manual and automatic paths use the same coordination.

## Manual agmsg host tool

With `agent_board.enabled`, `GET /api/hosts/agmsg?host=<ssh_connections-key>` returns
`{status, version?, error?}`. Empty host selects the panemux host. Status is missing, installed,
outdated, newer, invalid or unknown. `POST /api/hosts/agmsg` accepts only `{host}` and runs the
pinned installer after prerequisites and draining existing board calls. Unknown hosts and disabled
board return 404; writes that fail or overlap return 502 with their reason; checks during a pause
return 200 / unknown. Requests share the browser authority guard. No request selects source or args.
See [agmsg installation](../agent-board/agmsg-integration.md#manual-install--update).

The [agmsg installation contract](../agent-board/agmsg-integration.md#manual-install--update)
owns pinned source acquisition, stdout/stderr separation, archive transfer and installer diagnostics.

## Global agmsg automatic updates

`POST /api/hosts/agmsg/auto-update` takes only `{ "enabled": true }` (no host) and returns
`{ "auto_update": true }`. It is available only with Agent Board enabled (otherwise 404),
rejects unknown fields or a missing/non-boolean enabled value (400), and reports config-save
failures as 500 without changing the in-memory preference. It uses the existing YAML save path.
GET/POST agmsg tool responses also contain `auto_update`, plus `auto_updated` or `auto_error`
when this process has a result. Success belongs to the current host run, including its queued
confirmation; a later opt-in run or a newly added host identity does not inherit earlier success.
The checkbox controls every host. Startup, off-to-on opt-in and persisted host additions trigger
checks under the [automatic update policy](../agent-board/agmsg-integration.md#automatic-updating):
update older installed releases only, retain failures for manual retry, never auto-install,
downgrade, recover or periodically retry. That contract owns coalescing and per-host result lifetime.
