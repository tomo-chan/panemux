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

- No caller-controlled argument may reach a remote shell unescaped, including read arguments.
- `team`, `from`, and `to` use a strict identifier allowlist.
- Arbitrary message bodies are base64-encoded, checked against the encoded alphabet, passed as a
  positional argument, and decoded by a fixed remote wrapper before `send.sh`.
- agmsg's own validation occurs after the remote shell has parsed the command and cannot replace
  panemux's quoting and validation.
- `send.sh` owns SQL escaping; panemux must not build SQL or add a competing SQL-escape layer.

The encoded-body construction mirrors this repository's accepted allowlist-before-exec shape, but
has not been verified by a real CodeQL run. Treat it as structural mitigation, not proof that the
taint path is recognized as closed.

Panemux never installs its binary or helper scripts on a remote host. Remote hosts contain only
operator-managed agmsg and agent processes.

### Message trust

SSH protects each transport hop, but panemux sees plaintext while relaying. The panemux process and
host are therefore trusted; Agent Board does not provide end-to-end encryption.

agmsg's `--force` permits arbitrary sender names. The relay accepts:

- a pane sender only on the host that owns that board-enabled pane;
- `_system` only when the row matches a recent panemux-originated send in the own-send ledger.

Ledger entries are recorded before sending to cover a fast poll, removed if the send fails, expire
quickly, and are consumed once. This prevents a failed send from leaving a temporary impersonation
credential and preserves identical repeated sends.

This check does not cryptographically authenticate local pane processes. Any same-user process with
access to a host's agmsg installation can still impersonate another valid pane on that host; this is
accepted under panemux's existing same-user trust model.

### Command center authority

The command center has the same bearer-token gate as the board API and can address every
board-enabled pane. Its subprocess receives only the three board MCP tools, isolated settings, an
empty working directory, and sandboxing. It receives neither shell nor filesystem access.

A delivered message is not pre-authorized action. The receiving agent's normal approval and safety
rules continue to apply.
