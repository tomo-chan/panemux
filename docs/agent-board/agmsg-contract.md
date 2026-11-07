# Agent Board: agmsg compatibility contract

> Part of the current [Agent Board design](../agent-board.md).

## agmsg compatibility contract

Agent Board reads through `api.sh`, sends through `send.sh`, and uses the pinned `install.sh` for
managed Install / Update. Sending and installation are not covered by agmsg's read-API compatibility
promise. The consumer contract must detect drift before users encounter silent delivery failure.
Compatibility tests also exercise agmsg's official participation workflow; panemux does not invoke
its join or delivery setup scripts.

Every panemux invocation of an agmsg script sets the fixed environment value
`AGMSG_SELF_NAME=off` on the execution host, for local and SSH execution alike.
This includes reads, sends, and managed Install / Update. Board sends act for the
message's `from`, not the calling session: agmsg must not assign that name to the
caller's pane or type `/rename` into it. The fixed value overrides inherited settings;
`--force`, sender/recipient arguments, and message bodies retain their transport contract.

The contract verifies observable behavior, not log wording or private storage:

- joining assigns the requested pane identity;
- sending with `--force` preserves sender, recipient, and arbitrary body text;
- script execution disables the self-naming family even when inherited settings enable it;
- reads return the required JSONL fields in source order with opaque IDs;
- reads do not mark messages as consumed;
- identity claims and pane-specific watchers isolate same-project Claude sessions;
- dead claim owners can be replaced;
- exact release/tag normalization is accepted; non-release provenance warns and is not ordered.

### Tier 1: hermetic

Every `make check` run verifies panemux's command construction and parses captured JSONL fixtures
from the tested agmsg release selected by `fixtureDir` in `internal/agmsg/agmsg_fixture_test.go`.
The original `internal/agmsg/testdata/agmsg-v1.2.0/` capture also protects the historical ID-order regression.
Parser, cursor, command-wrapper, availability, and real-install contract tests live in
`internal/agmsg`; the relay and own-send transition tests remain in `internal/board`. Fixtures are regenerated from a verified release commit SHA, never
hand-authored. This tier catches regressions in panemux without requiring agmsg, a network, or
external state.

### Tier 2: real installation

`make test-agmsg-contract AGMSG_PATH=...` runs the same contract against an installed agmsg through
panemux's real local client.

CI runs it:

- on relevant pull requests against the PR's `agmsg.TestedAgmsgCommit`;
- on a schedule against the latest agmsg release.

The scheduled canary proposes a version/SHA pin and fixture bump only after success.
The App-authenticated `fix:` PR must pass ordinary CI and be reviewed before merging; draft
release publication remains manual. See [maintenance](../maintenance.md#bumping-the-pinned-agmsg-version).
The job remains outside `make check` because it installs and executes an external project. Local
runs skip when no explicit agmsg path is supplied.

Assertions must describe facts Agent Board depends on. Exact diagnostic sentences, timing based on
fixed sleeps, and incidental process ancestry are not contract behavior.

The workflow reads the version from `internal/agmsg/agmsg_version.go` and commit from
`internal/agmsg/tool.go`, and selects pull requests touching `internal/agmsg/`,
`scripts/agmsg_contract*`, `Makefile`, or the workflow itself. The package is included in
the Go coverage gate and in the fixture-agent registry consistency check.
