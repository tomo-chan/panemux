# Behavior: host-local task notice records

The `internal/taskhook` library describes input-needed notices. It does not decide approvals,
send terminal input, establish that an answer was accepted, install Codex hooks,
or connect records to the task dashboard. A standalone helper and native verifier are available,
but normal launch/resume does not yet register owners or attest routing scope. Task collection retains its existing
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
the fixed payload has no request ID to prove that identity. Failed verification attempts cancel
only their own inactive slot when the deadline permits. `RecoverProvisional` explicitly discards
the entire current provisional set at an exact owner/session/revision match, frees its capacity,
and invalidates old verifier tokens while preserving every active episode and notice ID. No
elapsed-time expiry resolves active notices. If a crash exhausts inactive capacity, reads expose
the provisional set and writes return the fixed limit error until explicit recovery succeeds.

`ScanRollout` reads at most 4 MiB with lines below 64 KiB. It requires a complete snapshot, one
matching `session_meta` with main CLI source, and explicit `approvals_reviewer=user` on the exact
turn. It returns only file identity, complete-line end offset and a veto. An exact-turn terminal
anywhere in the snapshot, including before the start watermark, or a later turn context vetoes a
new notice. Missing/null reviewer, partial or corrupt lines, duplicate JSON keys and budget overflow
are unknown. Commands, conversation and paths are discarded. The caller must verify the held
regular no-follow file identity and owner before and after the scan. Exact-turn user metadata alone
does not prove the absence of dynamic settings, strict/Guardian routing or deciding related hooks;
automatic routing-scope verification remains absent.

## Owner registry and native verification

An existing private root also holds bounded `g1-<generation>.owner.json` launch registrations.
These are separate from notification records: trusted host metadata includes a canonical sessions
root, native executable file identity, launch UID/PID/birth and externally verified routing scope.
Each generation is immutable; identical retries succeed, while another birth, root, executable,
scope or corrupt file cannot replace it. Registration never creates or repairs a root.

The verifier follows at most 32 same-UID ancestors from the helper to the registered launch
PID and exact birth. It verifies that a native ancestor maps the registered executable identity
and holds one regular no-follow file identity matching the explicit hook session. Other sessions
may remain open; duplicate descriptors of the same identity collapse. Multiple matching identities
or native ancestors are unknown. Owner and held session are checked again immediately before
publishing the reserved episode. A failed proof cannot create a pending notice.

macOS uses kernel birth microseconds and bounded `lsof`; Linux uses boot UUID plus kernel start
ticks and bounded `/proc` descriptors. Rollout paths stay within the trusted root, opened through
no-follow directory descriptors with UID and nonshared-write checks. Files must be regular,
owned by the effective user, unshared for writes and have one link. Snapshot size and modification
time changes fail closed. Payload `cwd` and `transcript_path` never select these paths.

Native macOS arm64 has been exercised in an isolated fixed-0.160.0 TUI; Linux is implemented and
cross-compiled but has not been exercised against a native TUI here. Node wrappers whose native
executable identity is not known to registration, shared daemon paths, arbitrary CLI starts,
SSH and unknown routing scope retain ordinary task fallback; this does not change existing task,
session or pane identity. Exact kernel ownership is distinct from true UI selection: the helper
does not relabel an old A hook as the collector's currently selected B.

## Standalone helper commands

Build `cmd/panemux-task-hook` with the repository Go runtime. It imports neither the main server
nor frontend assets. Its fixed command form is `panemux-task-hook <mode> --root <private-root>`:

- `record-permission` consumes bounded stock hook JSON, uses the inherited generation only as
  a registry lookup candidate, then verifies ancestry, file and exact-turn metadata before publish.
  All failures return exit 0 with empty stdout/stderr and no approval decision.
- `register-launch` consumes `sessions_root`, `executable_path` and `scope` as trusted managed
  control input. It derives the actual parent UID/PID/birth from the kernel, generates a random
  generation, registers it durably and returns only that generation. Use a normal child invocation
  whose stdout goes to a private file before the parent execs Codex; command substitution registers
  its short-lived child shell instead of the shell that will exec.
- `inspect` accepts an owner/session lookup and emits only the bounded record.
- `mark-provided` and `recover-provisional` accept owner/session/revision and the complete relevant
  episode-ID set. `resolve-turn` accepts trusted same-turn/file/offset terminal evidence and the
  expected revision. These are control-plane operations, not hook decisions or terminal input.
- `selfcheck` reports helper/protocol versions and whether the process-identity probe is available.
  It does not certify routing, installation, Codex hook trust or an active recorder.

The process entry point has a 500 ms deadline including blocked stdin and filesystem I/O. It
exits at the deadline even if an in-flight write has an unknown durability result; subsequent
reads and explicit recovery use the durable state. Other modes return exit 1 and a fixed diagnostic
on failure, without reflecting input, command text, private paths or decoder errors.

`scope` is a trusted launcher's declaration for fixed 0.160.0/no-daemon/main-task/Bash/user/no-strict/
passive-related-hook operation. Unknown `NoStrict` or `PassiveHooks` evidence must remain false.
The helper does not inspect all config/rules/plugin sources or prove dynamic routing. Normal
managed launch integration and its scope-evidence source remain unconnected; the isolated native
fixture used an explicit declaration of its own settings. Registering `unknown` scope is allowed
but cannot publish a notice. Existing user settings and hook trust are never rewritten.

## Persistence and failure

Mutations serialize under one host-private advisory lock, compare the latest record, and commit
through atomic replacement and directory sync before acknowledging success. Readers see a complete
record. Lock acquisition is bounded to 100 ms and honors cancellation; process exit releases it.
Failed or unknown commits require rereading and retrying the same claim, never clearing only local
UI state. Filesystem I/O itself may block; a command runner must enforce its own total deadline.

See [Private task notice records](../security/command-execution.md#private-task-notice-records)
for no-follow, ownership, permissions and size checks.
