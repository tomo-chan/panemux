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
them. At most 16 pending or provisional episodes are retained; overflow fails without dropping any pending notice.
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
start is the ingress verifier's responsibility. The metadata scanner below supplies a pre-start veto;
it does not establish live ownership or all routing eligibility.

Resolving a subset preserves the existing notice ID for remaining pending episodes. Neither
resolution nor a claim changes a task's ordinary state, since task collection owns that state.

## Provisional ingress and metadata evidence

After verifying the process owner, a caller can reserve an inactive episode before reading slow
eligibility evidence. Reservation increases the record revision without changing an existing active
notice ID. It contains no verified anchor and never appears in the pending set. Finishing requires
the exact reservation owner/session/turn, a verified anchor, and an unchanged `closed_revision`.
Concurrent reservations can finish in either order and preserve both episodes. Input provided or
a verified terminal transition advances this closing fence and settles unfinished reservations;
a delayed scan cannot reactivate them after a restart. An old displayed revision conflicts when
ingress has reserved a concurrent request. A fresh claim of the current pending set also closes
the provisional ingress it raced with.

A positive pre-start veto closes the reservation and advances the same fence. Replay of a finished
or vetoed token fails. Unknown eligibility leaves an inactive slot; a crashed verifier can exhaust
the bounded slot limit, causing fallback rather than evicting known pending notices. This library
does not expire reservations or infer that a hook starting after a claim is the same Codex request:
the fixed payload has no request ID to prove that identity.

`ScanRollout` reads at most 4 MiB with lines below 64 KiB. It requires a complete snapshot, one
matching `session_meta` with main CLI source, and explicit `approvals_reviewer=user` on the exact
turn. It returns only file identity, complete-line end offset and a veto. An exact-turn terminal
anywhere in the snapshot, including before the start watermark, or a later turn context vetoes a
new notice. Missing/null reviewer, partial or corrupt lines, duplicate JSON keys and budget overflow
are unknown. Commands, conversation and paths are discarded. The caller must verify the held
regular no-follow file identity and owner before and after the scan. Exact-turn user metadata alone
does not prove the absence of dynamic settings, strict/Guardian routing or deciding related hooks;
live recorder integration remains absent.

## Persistence and failure

Mutations serialize under one host-private advisory lock, compare the latest record, and commit
through atomic replacement and directory sync before acknowledging success. Readers see a complete
record. Lock acquisition is bounded to 100 ms and honors cancellation; process exit releases it.
Failed or unknown commits require rereading and retrying the same claim, never clearing only local
UI state. Filesystem I/O itself may block; a command runner must enforce its own total deadline.

See [Private task notice records](../security/command-execution.md#private-task-notice-records)
for no-follow, ownership, permissions and size checks.
