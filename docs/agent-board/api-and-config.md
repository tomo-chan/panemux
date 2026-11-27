---
type: Specification
title: "Agent Board: API and configuration"
description: "Agent Board configuration and API ownership with links to exact payload contracts."
---

# Agent Board: API and configuration

> Part of the current [Agent Board design](../agent-board.md).

## API additions

All Agent Board REST routes, `/api/session-token` bootstrap, `/ws/board/command` WebSocket frames,
authentication boundaries, and `identities` payload contracts are authoritative in
[Agent Board API](../api/agent-board.md).

## Config additions

```yaml
server:
  host: "127.0.0.1"
  port: 8080
  auth_token: ""   # empty: generate and persist a private token

command_center:
  enabled: true     # default: false; independent of agmsg availability

agent_board:
  enabled: false # opt in for the whole instance
  # mode: monitor # deprecated: validated, warned about once per load, and ignored
  team: "panemux"
  agmsg_auto_update: false # update older installed copies on all hosts
```

Enablement and team are instance-wide. For one release, `agent_board.mode` accepts
`monitor`, `turn`, `both`, `off`, or an empty value, emits one warning per configuration load when
present, and has no effect on detection, registration, delivery, or relay behavior. Saving the config
omits the key, including an explicit legacy value; the in-memory API mode is unchanged by saving.
Loading a config without the key keeps the API default `monitor` without a deprecation warning. There are no pane or host overrides.
Configure these settings in YAML; Pane Settings does not expose them. Each host uses
`~/.agents/skills/agmsg` relative to its own home. Local discovery uses the injectable home-directory
seam. The removed pane `agent_board` block and `agent_board.agmsg_path` produce migration errors.

Use the task-board host menu for manual Install / Update of the pinned agmsg source.
`agmsg_auto_update` controls one instance-wide preference. The host-menu checkbox saves it
through the existing config persistence path. See [automatic updating](agmsg-integration.md#automatic-updating).
See [installer behavior](agmsg-integration.md#manual-install--update).

Both boards use the panemux host and `ssh_connections` keys. An SSH pane participates only when
its `connection` exactly matches a key; resolved hostname, user and port are not compared. A
`~/.ssh/config`-only pane is warned about at startup and excluded without a validation error.
Add a name-only `ssh_connections` entry to use its OpenSSH configuration for both boards.

`command_center` is a sibling of `agent_board` because it talks only to panemux's API and can run
without enabling Agent Board or local agmsg installation. The browser receives independent
`command_center_enabled` and `agent_board_enabled` flags.

`_system` is reserved. Configuration rejects it as a pane ID or team, but external processes can
still ask agmsg to use that name; relay sender validation must therefore rely on the own-send ledger,
not the identifier alone.
