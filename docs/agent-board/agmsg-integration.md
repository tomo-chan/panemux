# Agent Board: integration with agmsg

> Part of the current [Agent Board design](../agent-board.md).

## Integration with agmsg

Panemux uses agmsg's public scripts and never reads its database or team files. The required
contract is:

| Operation | Script contract |
|---|---|
| list teams | `api.sh get teams` |
| list members | `api.sh get teams <team> members` |
| read messages | `api.sh get teams <team> messages [--agent <name>] [--limit N] [--before-id <id>]` |
| send | `send.sh <team> <from> <to> <body> --force` |
| join | `join.sh <team> <agent-id> <agent-type> <project-path> --force` |

`api.sh` returns JSONL oldest-first and does not mark messages as read. Message IDs are opaque
strings: they are matched for equality only, never parsed, sorted, or compared across hosts.

### Incremental reads

agmsg has backward pagination (`--before-id`) but no forward `after`/`since` operation. Each relay
poll therefore reads the newest bounded window and selects rows after the previous cursor in the
order returned by agmsg.

If the cursor is absent from that window, the whole window is treated as new instead of assuming an
order that agmsg does not guarantee. This can replay rows. If more than the configured limit arrives
between polls, older overflow rows can instead be lost. Both are accepted limits of the external
contract; cross-host send failures have the separate best-effort semantics in
[Relay](relay.md#startup-and-recovery).

### Board writes

Every board write uses `send.sh --force`. The reserved `_system` identity and panes on other hosts
are not necessarily members of the local agmsg team, so normal roster validation cannot support
status reports or cross-host delivery. Agents may still use ordinary roster-checked agmsg commands
for conversations unrelated to Agent Board.

Because `--force` also makes `from` forgeable, the relay must validate senders. A `_system` row is
trusted only when it matches panemux's own-send ledger; the string alone grants no authority.

Panes join directly through `join.sh` with the pane ID as `agent_id`. Direct script calls avoid
agmsg's agent-specific `/agmsg` versus `$agmsg` command prefixes and keep pane addressing stable
across Agent Board.

### Discovery and paths

Panemux detects agmsg by checking `<agmsg_path>/scripts/api.sh`. It must not use or invoke the
`agmsg` npm bootstrap command, and it never installs or updates agmsg. A missing installation
disables board integration for that host without affecting terminal service.

`agent_board.agmsg_path` defaults to `~/.agents/skills/agmsg` and may be overridden. Panemux expands
a leading `~/` before remote shell quoting:

- locally, against the local user's home directory;
- remotely, against the remote user's home determined over the existing SSH connection.

Remote board commands run in a non-login, usually non-interactive shell. agmsg's dependencies
(`bash`, `node`, and `sqlite3`) must therefore be available on that shell's `PATH`; availability in
an interactive profile is insufficient.

### Version coverage

`board.TestedAgmsgVersion` is the version whose script behavior this repository verifies. Startup
normalizes agmsg's common `VERSION` forms and warns when an installed release is outside the
covered range. A warning never blocks startup.

Coverage means the same major/minor line at or above the tested patch. Older patches, other
major/minor lines, prereleases, and unparseable versions warn. A missing `VERSION` file is unknown
and does not produce a false mismatch claim. This policy reduces warning noise; it is not a semver
guarantee from agmsg. The [compatibility contract](agmsg-contract.md#agmsg-compatibility-contract)
must verify real releases.

## Two panes in one project directory

Pane identity is always the pane ID, never the project path. Two same-type agents in one directory
would otherwise be discovered by agmsg as the same subscription set and could receive each other's
messages.

For `claude-code`, bootstrap follows agmsg's act-as protocol:

1. join with the pane ID;
2. claim that identity with the current Claude session ID;
3. when monitor delivery is active, run a watcher restricted to that pane ID.

If another live session holds the identity, onboarding stops and reports the conflict. The claim is
still made for `turn` and `off`, but those modes do not start a watcher.

This protection is currently verified only for `claude-code`; other agent types in the same project
directory can still cross-receive. Claims are made only during bootstrap, so restarting an agent
inside the same unchanged pane does not automatically claim again. These bounds are documented in
[Known limitations](limitations.md#known-limitations).
