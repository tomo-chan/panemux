# Behavior: host-local task notice records

The `internal/taskhook` library describes input-needed notices. It does not decide approvals,
send terminal input, establish that an answer was accepted, register Codex hooks, verify live
processes, or connect records to the task dashboard. Normal task collection retains its existing
behavior, including reporting Codex command approvals as busy.

## Record identity and limits

A private root holds protocol-version-1 records keyed by owner generation and session UUID.
The owner carries UID, PID and opaque high-precision process birth identity. A trusted caller must
verify the owner/session/turn before supplying a binding; a generation inherited in the environment
or an ID in hook JSON is not sufficient evidence. Different generations and sessions occupy
separate records. The library never repairs, migrates or overwrites a corrupt or unknown record.

Each start adds a random `ep1-` episode and changes the stable `h1-` notice ID. These are panemux
notification identities, not Codex request or tool-use IDs. Repeated reads and restarts preserve
them. At most 16 pending episodes are retained; overflow fails without dropping any pending notice.
Settled episodes are compacted when another start is added. Revisions increase on changes and
remain within the exact integer range used by JSON/browser consumers.

Records contain session/turn, owner, notification IDs, dispositions and an optional log anchor.
They contain no command, arguments, conversation, directory or transcript path. An anchor is an
opaque file identity and the end offset of its last complete line at notice start. PermissionRequest
input parsing only extracts main-task Bash session/turn metadata; routing eligibility remains the
trusted caller's responsibility.

## Compare and update

An input-provided claim must carry the same owner, session, expected revision and the entire set
of displayed pending episode IDs. Order is immaterial. Subsets, duplicates, another owner/birth,
or a stale revision fail without changing any episode. A matching claim durably changes the set
to `provided`. Repeating that same claim against the unchanged resulting revision succeeds without
another write. A later start invalidates the retry receipt and prevents an old claim from clearing
new pending episodes. This is a user declaration, independent of Done, labels and approval outcome.

Verified same-turn terminal evidence changes only that turn's pending episodes to `resolved`.
The owner, session and expected revision must match. For this transition, evidence must name the
same file identity and a complete terminal line ending strictly after the start anchor. A missing
anchor, another turn or rotation supplies no resolution evidence. Partial scans and elapsed time
must not be represented as terminal evidence. Detecting an already-ended turn before creating a
start is the ingress verifier's responsibility; this library has no log scanner.

Resolving a subset preserves the existing notice ID for remaining pending episodes. Neither
resolution nor a claim changes a task's ordinary state, since task collection owns that state.

## Persistence and failure

Mutations serialize under one host-private advisory lock, compare the latest record, and commit
through atomic replacement and directory sync before acknowledging success. Readers see a complete
record. Lock acquisition is bounded to 100 ms and honors cancellation; process exit releases it.
Failed or unknown commits require rereading and retrying the same claim, never clearing only local
UI state. Filesystem I/O itself may block; a command runner must enforce its own total deadline.

See [Private task notice records](../security/command-execution.md#private-task-notice-records)
for no-follow, ownership, permissions and size checks.
