---
type: Decision
title: "Stage 2: starting and resuming claude tasks from the dashboard (2026-09-26, issue #257)"
description: "Launch via fixed stdin scripts and prompt files, and serialize resumes without destroying existing shell sessions."
---

# Stage 2: starting and resuming claude tasks from the dashboard (2026-09-26, issue #257)

Fixed stdin scripts and checked heredocs were chosen over interpolating values into remote
commands. Prompt files avoid tmux retaining prompts in server argv. UUIDs, -- and --resume=<id>
prevent names/prompts from becoming CLI options. The agent owns the tmux session lifetime.
Review found tmux -c expands directory format characters; an inner fixed shell changes cwd instead.
Resume adds a window to an existing task-named shell rather than destroying its work or stranding
open panes under a different session name. Per-host/session serialization prevents duplicate resumes
within one panemux process; separate instances are not coordinated.
The original Claude-only limitation was replaced by [Codex support](codex-session-observation.md)
and [adapters](../agent-registry/launch-resume-adapters.md).
See [launch security](../../security/command-execution.md) and [Task launch and resume](../../behavior/task-launch.md).
