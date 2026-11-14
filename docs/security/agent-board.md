---
type: Security Requirement
title: "Security: Agent Board"
description: "Agent Board remote-write constraints and treatment of agent-reported values."
---

# Security: Agent Board

> Part of the [security design](../security.md). The general command-execution and `gosec` rules
> also apply.

### Agent board remote writes

Only `internal/agmsg`, the adapter of the board-owned message and participation ports, may invoke
operator-installed agmsg scripts through the shared host execution port
(local `sh -s` or the per-host SSH command connection). It
must never install panemux or panemux helper scripts on a remote host. The agmsg tool adapter may
install the pinned upstream source through the shared input port, with the trust described below.

Remote commands must satisfy all of these rules:

- pass every value as an argument and quote every argument at the single SSH command-construction
  boundary;
- keep `team` on `[A-Za-z0-9_.-]+`; validate `from` and `to` structurally as a name
  on that alphabet, optionally followed by `@local` or `@ssh:<key>` with the same
  key alphabet; reject qualified `_system`, extra separators and shell syntax;
- base64-encode arbitrary message bodies, validate the encoded alphabet, and decode through a fixed
  wrapper using positional parameters;
- never interpolate caller-controlled data into wrapper or probe script text;
- export the compile-time literal `AGMSG_SELF_NAME=off` before invoking agmsg scripts,
  including the pinned installer, on both local and SSH hosts; never derive this value
  from `os.Getenv`. This overrides inherited opt-ins and prevents agmsg from treating
  panemux's calling session as the message sender for pane naming or `/rename` input;
- unset `BASH_ENV` and `ENV` at that boundary before launching child shells, and remove
  the pinned built-in drivers' pane markers plus Codex's thread identity as listed in
  [the agmsg contract](../agent-board/agmsg-contract.md). An unresolved caller pane makes
  self-rename return before durable marks; `off` alone can record a skip mark and alter
  the agent's later naming behavior. Preserve HOME, PATH and storage settings, and never
  remove or rewrite agent-owned marks as cleanup;
- leave SQL escaping to agmsg and never construct SQL in panemux;
- expand `~/` in the fixed `~/.agents/skills/agmsg` location to an absolute local or remote-home path before shell quoting.

Plain shell quoting is not sufficient evidence for the repository's CodeQL command-injection bar.
The encoded-body path deliberately places an allowlist check before the exec sink, but no real
CodeQL scan has yet confirmed that the analyzer recognizes the taint break. Treat this as structural
mitigation until verified.

Fixed probes resolve the remote home, test the read script/version marker, discover alternate
`.agmsg` installations and check installation prerequisites. Variable values must be positional arguments, not embedded in probe text.

Agent Board never writes participation instructions or fallback input to terminal PTYs.

### Agent-reported values in the dashboard UI

Every status field is untrusted free text. Sender validation attributes an unqualified name to the host read through `api.sh`;
it does not authenticate a same-user process or validate the content. Status keys use
`name@BoardID` within the configured team, keeping equal names on different hosts separate.

The dashboard must render agent-reported values as React text children. It must not use
`dangerouslySetInnerHTML` or place those values into DOM attributes. In particular, self-reported
URLs must not become links without an explicit scheme allowlist: escaping text does not make an
`href` safe, and script execution in the dashboard origin would expose the board bearer token.

The current dashboard shows `state`, `summary`, and `last_tool` as text. It intentionally omits the
self-reported repository, branch, and PR URL. Any future attribute-valued rendering reopens this
security sink and requires dedicated validation.

### Global enablement and sender scope

`agent_board.enabled` is instance-wide consent to relay for the configured team on the
local host and all configured SSH keys, without requiring panes, detected tasks, or roster
registration. A source host's valid unqualified names are accepted; every qualified `from`
and every pane ID belonging to another host is rejected. `_system` requires the unchanged
own-send ledger. Qualified recipients require a configured BoardID, not a pane or roster entry.

Forwarded senders are qualified using the actual read host. The sole unqualified compatibility
case is a configured pane sender addressing another configured pane by its bare ID. A pane-free
sender addressing a bare pane ID is still qualified: otherwise the destination would accept the
copy as a new original. Pane detection and dashboard visibility grant no sending authority.
Same-host, same-user name spoofing remains possible; received messages are not authorization
for work or approval. See [relay rules](../agent-board/relay.md).

`agent_board.mode` is ignored. The host menu can run the pinned installer with explicit consent.

### Pinned installer trust

Only the agmsg adapter chooses the fixed upstream repository, version and full commit SHA.
The panemux host fetches and verifies the exact commit, revalidates cached commit identity and uses
`git archive` to produce the transferred tar. Source/cache ownership is trusted like other local
application state. No request chooses repository, ref, archive bytes, command or installer arguments.
See [the command boundary](agmsg-install.md).

Install / Update authorizes execution as the target user of the pinned upstream code verified by
contract tests and human release review. The installer writes outside `~/.agents/skills` (Claude
commands, other agents' skill directories, Codex sandbox settings and agent shims). Panemux does
not sandbox those effects, operate on teams/DBs directly, run recovery, install dependencies or
stop agent-owned scripts.

Automatic agmsg updates use the same fixed-SHA source and installer path as manual Update. The
instance-wide preference authorizes unattended execution on older installed copies at startup,
on opt-in and when a configured host is added; it never authorizes automatic Install or downgrade. Eligibility is rechecked inside
the update reservation. No new command execution sink is introduced.
