---
type: Security Requirement
title: "Agent Board: security model"
description: "Agent Board trust boundaries and security responsibilities across hosts and processes."
---

# Agent Board: security model

> Part of the current [Agent Board design](../agent-board.md). Implementation rules live in
> [Security design](../security.md).

## Security model

### Deployment boundary

- Panemux is designed for loopback or a trusted network and does not terminate TLS.
- Non-loopback deployment requires an authenticated, TLS-terminating boundary such as a reverse
  proxy, SSH tunnel, or trusted overlay network.
- Configuration must reject a non-loopback bind with no explicit auth token.
- The bearer token protects board APIs and the command center; it does not convert the existing
  terminal server into an Internet-facing multi-user service.

### Remote execution boundary

Both boards share the host execution port and per-host command connection. Each operation has its
own exec channel; cancelling one operation closes only that channel, and a healthy connection
remains available to the other board. Terminal pane connections are separate.

- No caller-controlled argument may reach a remote shell unescaped, including read arguments.
- `team` keeps the strict identifier allowlist; `from`/`to` additionally accept structurally
  checked `name@local` and `name@ssh:key` addresses. Names and keys use `[A-Za-z0-9_.-]+`;
  qualified `_system` and malformed addresses are rejected.
- Arbitrary message bodies are base64-encoded, checked against the encoded alphabet, passed as a
  positional argument, and decoded by a fixed remote wrapper into the public `send.sh --body -`
  standard-input interface. Option-like bodies remain data.
- agmsg's own validation occurs after the remote shell has parsed the command and cannot replace
  panemux's quoting and validation.
- `send.sh` owns SQL escaping; panemux must not build SQL or add a competing SQL-escape layer.
- All agmsg script calls, including Install / Update, export the fixed
  `AGMSG_SELF_NAME=off` on the execution host. agmsg's self-naming and `/rename`
  side effects must not name or type into panemux's calling session on behalf of `from`.
  The same wrapper removes child-shell startup hooks and the pinned drivers' caller-pane
  markers, so the opt-out cannot be reset by `BASH_ENV`/`ENV` or leave a durable rename
  skip mark that changes an agent's later actions. Existing agent marks are untouched.

The encoded-body construction mirrors this repository's accepted allowlist-before-exec shape, but
has not been verified by a real CodeQL run. Treat it as structural mitigation, not proof that the
taint path is recognized as closed.

Panemux never installs its binary or helper scripts on a remote host. Remote hosts contain only
agmsg and agent processes; the host menu can run the pinned agmsg installer.

### Message trust

SSH protects each transport hop, but panemux sees plaintext while relaying. The panemux process and
host are therefore trusted; Agent Board does not provide end-to-end encryption.

agmsg's `--force` permits arbitrary sender names. The relay accepts:

- any valid unqualified name read through public `api.sh` from its source host, without
  requiring a roster, pane or task mapping;
- `_system` only when the row matches a recent panemux-originated send in the own-send ledger.

Every qualified `from` is excluded from history, state and forwarding, as is an unqualified
configured pane ID belonging to another host. Forwarding qualifies the sender with the read
host's BoardID on every forwarded copy, including pane-to-pane traffic addressed by a bare
pane ID. Rejecting qualified senders prevents duplicate history, false source-host attribution
and re-forwarding even when panes are removed or moved before a delayed copy is read.
Recipients reply to the received qualified sender. `local` and `ssh:local` identify different hosts.

Ledger entries are recorded before sending, removed if sending fails, expire quickly and are
consumed once. The key uses the unqualified destination-visible recipient, with host/team/body
hash as before. No forwarding ledger is added.

This check does not cryptographically authenticate same-user processes. Any same-user process
with access to a host's agmsg installation can impersonate another name on that host. Status is
self-reported and host-scoped; a message is not permission to perform work or grant approval.

### Command center authority

The command center has the same bearer-token gate as the board API and can address every
configured pane. Its subprocess receives only the three board MCP tools, isolated settings, an
empty working directory, and sandboxing. It receives neither shell nor filesystem access.

A delivered message is not pre-authorized action. The receiving agent's normal approval and safety
rules continue to apply.

### Global participation consent

Top-level enablement authorizes detection, display, and relay polling. Board never writes to a PTY
or asks agents to join, claim an identity, or install project-local delivery hooks. Delivery mode is
ignored. Existing agmsg registrations and hook files remain untouched after disablement.
Sender validation accepts host-local unqualified names independently of pane detection or
roster membership. Consent covers the local host and all configured SSH keys in one team.
The same-host, same-user trust boundary is unchanged. Visibility in the dashboard
grants no authority.

Manual Install / Update requires trust in panemux's pinned agmsg code on the target host.
The installer can write outside the skill directory, including agent settings and commands;
the UI explains this consent. See [installer details](agmsg-integration.md#manual-install--update).
Opting into global automatic updating authorizes this same installer on eligible hosts at startup,
on opt-in and after host additions without further prompts. Panemux pauses its own calls but cannot stop agent-owned scripts;
restart agents after installation/update.
