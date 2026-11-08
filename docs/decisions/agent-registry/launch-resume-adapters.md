---
type: Decision
title: "Launch and resume through per-agent adapters (2026-10-18, issue #374)"
description: "Rationale for Launch and resume through per-agent adapters."
---

# Launch and resume through per-agent adapters (2026-10-18, issue #374)

The launch script and `Service.Launch`/`Resume` branched on the claude and codex IDs for the
arguments, the session ID minted or not, the tmux session name and the codex `--no-daemon` check, and
`PendingLabels` was codex's alone. Agreed on #374:

- **Launch and resume are two adapters, not one.** An agent can launch without resuming. Each is
  listed by ID in `launchAdapters`/`resumeAdapters` and checked against the registry at startup, as
  collect adapters are.
- **One fixed script, with each adapter's fragment composed under `case $agent:$mode`.** Request
  values stay in the seven checked placeholders; a fragment is a Go constant, checked to set only its
  variable, stay in its case and refuse only with declared codes. A per-agent script was rejected: the
  tmux, working-directory and prompt-file handling is the same for every agent and is where the
  safety argument lives.
- **Whether labels wait is read from the launch, not the agent's name.** A launch that names no
  session ID holds them in `PendingLabels`, keyed by (host, agent, tmux session). Only exactly one
  session of that agent claims them. A task of another agent in the tmux session no longer keeps them
  waiting (codex's version counted any task there, so a claude started in it held them until
  restart), and two sessions of the agent drop them rather than guess.
- **`cli_missing` refuses before anything runs, and probes again.** The refusal reuses the script's
  `no-<agent>` code and `409`, so no API shape changes (the API exposure of availability is #376).
  Probing again at once was chosen over refusing for the rest of the 5-minute TTL, which would refuse
  a CLI installed in the meantime. `not_probed` and `probe_failed` refuse nothing: the script still
  looks for the CLI itself, and refusing on what is not known would be guessing.

Current contracts: [agent registry guide](../../agent-registry.md).
