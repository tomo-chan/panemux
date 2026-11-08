---
type: Decision
title: "2026-10-17: Updating the codex permission hook by itself (#294, #402)"
description: "Offer per-host opt-in update-only checks instead of redundant binaries or automatic retries."
---

# 2026-10-17: Updating the codex permission hook by itself (#294, #402)

Stage 4 of #294 was to ship the hook as a binary for four targets and update it. Stage 3 had already
made it a POSIX `sh` script embedded in panemux, which reaches any host, so the binaries were dropped
and only the updating kept.

- **Opt-in per host, Update only.** `Update automatically` is off until a host's menu turns it on.
  It never installs on a host that has no hook, and never touches codex itself.
- **When the check runs.** Stage 3 checks only when a host's menu opens, which would have left an
  update waiting for someone to open it. A host with the setting on is therefore also checked once
  in the background after panemux starts, on its first successful collection, so restarting a new
  panemux updates every such host with nothing pressed.
- **Once per process.** Each host is updated by itself at most once per panemux process, success or
  failure; a failure is shown and not retried until a restart. No lock between panemux instances, no
  downgrade guard and no online lookup of a newer hook: each would answer a failure no one has met.
- **A file of its own.** The setting lives in `~/.config/panemux/codex-hook.json`, not in
  `config.yaml` (which has no entry for the panemux host) or the task record file (keyed by task).

Current contracts: [maintenance guide](../../maintenance.md).
