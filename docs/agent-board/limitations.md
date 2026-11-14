---
type: Reference
title: "Agent Board: known limitations"
description: "Current Agent Board availability, recovery, and deployment limitations."
---

# Agent Board: known limitations

> Part of the current [Agent Board design](../agent-board.md).

## Known limitations

### Availability and recovery

- An unreachable host is retried on later polls through the shared connection pool: failed dials
  wait 60 seconds, a connection lost during use can reconnect immediately, and the task host menu
  reconnect discards the shared connection and cooldown. No restart or pane is needed.
- A confirmed missing agmsg installation is cached as absent until a managed installer attempt; bootstrap
  continues checking presence until it succeeds for that pane session. Successful manual Install clears absence and is observed on the next poll.
- Updates pause only panemux calls. Existing agent-owned scripts can see files being replaced;
  restart agents after Install / Update. Failed installers may require manual recovery.
- An SSH pane whose `connection` exists only in `~/.ssh/config` is outside Agent Board. Add a
  name-only entry to `ssh_connections` to include it; startup warns without rejecting the config.
- The status/history cache is memory-only. Restart recovery uses bounded backfill and can omit older
  rows outside that window.
- Broadcast delivery is immediate, but dashboard history is updated only when the relay polls the
  row back. The visible lag is at most one poll interval under normal operation.

Relay accepts host-local agmsg names without panes or roster lookups. Qualified recipients
select a configured BoardID, including hosts without panes. Unqualified names stay local except
for the legacy configured pane-ID route. All forwarded copies have qualified senders, including
pane senders; consumers must reply to the received qualified `from` instead of assuming a bare
pane reply address. Copy exclusion does not depend on the pane map staying unchanged.
The current UI still lists only
detected agents or reporting configured panes; pane-free identity presentation is separate.

### Delivery guarantees

- Cross-host forwarding is best-effort. A missing destination client or failed send is not retried:
  the source row remains visible in history and its cursor advances.
- A crash between a successful forward and cursor persistence can produce a duplicate.
- Delivery is not guaranteed complete. Because agmsg has no forward cursor, a burst larger than the
  poll window can drop its oldest overflow rows.
- There are no message claim or lease semantics.
- agmsg names are not cryptographically authenticated. Qualified senders and pane IDs belonging
  to another host are rejected; a same-user process can impersonate any name on its own host.
  Receiving a message is not authorization. Cross-host plugins must send/reply with `--force`
  to the received qualified sender; panemux provides no proxy registration or send wrapper.

### Bootstrap

- Detection and display do not register participation or configure delivery. PTY onboarding is
  disabled. Participation, identity selection, and reception follow agmsg's official workflow.
  Relay and status accept valid unqualified names read from the configured team on that host;
  no pane or session mapping is required.
- Detection alone provides no same-project identity isolation or proof of message reception.
- Process detection can reject an interactive command whose quoted prompt contains a headless-mode
  exclusion token as a separate whitespace token.
- Existing agmsg registrations and project-local hooks are left untouched. The retired
  `board-bootstrap-state.json` is not read, written, or deleted.

### Status and usage

- Status is cooperative. An agent that stops reporting remains stale; panemux has no independent
  Agent Board liveness signal and does not infer replacement state.
- Agent Board does not report account-wide or per-pane token usage. Account usage belongs to the
  agent provider; any future per-pane measurement should extend the existing pane-inspection
  surface rather than the agmsg status schema.

### External dependency

Pinned agmsg v1.7.1 (`f5898f49eb3d3c079c446dab4bca715b832da313`) normalizes trailing
body newlines. This was observed through its real `send.sh` / public `api.sh` path and is
reproduced by `TestAgmsgContract_TrailingNewlinesAreNormalized`; panemux's fixed shell
wrapper also uses command substitution, which removes trailing newlines. The qualified-ID
contract protects shell syntax, Unicode and embedded newlines as data, not byte-for-byte
preservation of trailing newlines. No panemux-specific body encoding is added to agmsg messages.

agmsg guarantees compatibility for its read API, not for every write script Agent
Board uses. The pinned version and two-tier compatibility contract detect drift but cannot prevent a
future operator-installed release from breaking integration. A detected version outside the covered
range warns and continues.

### Command center

- There is one command-center conversation per panemux instance, with no workspace-specific or
  concurrent sessions.
- Each query starts a new `claude -p` process, so latency includes process startup.

SSH connection establishment starts in the background: polling and bootstrap return a connecting state
without waiting for the dial. Later polls retry, so a pane-less unreachable host cannot consume a
relay pass's timeout before healthy hosts are polled. Repeated identical poll errors are logged only
when the failure state changes; a successful pass resets that suppression. All configured keys
remain available to board execution, including hosts without a board pane. Rows use source-host attribution independently of pane presence.

Operator sends wait for a missing SSH connection within the send context, including before the
first poll and after reconnect or transport loss. Polling and bootstrap continue to use the
non-waiting runner; all runners share the same connection pool.

### Detection cost

With Agent Board enabled, every eligible configured pane is checked every five seconds, including
detected agents and plain shells. Local panes each request a process listing; tmux panes
also query tmux, and SSH panes run a remote detection command. Process listings are currently not
shared across panes. This steady-state work grows with the number of panes; detection does not
stop after an initial detection because the API keeps the detected type current.

Availability probes wait for two consecutive detections and stop after confirmation for that pane
session. Failed checks retry on later polls; managed tool-update pauses are silent and retryable.
Session replacement, host changes, and pane removal invalidate confirmation.
