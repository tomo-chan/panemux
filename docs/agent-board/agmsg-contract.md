# Agent Board: agmsg compatibility contract

> Part of the current [Agent Board design](../agent-board.md).

## agmsg compatibility contract

Agent Board depends on agmsg scripts whose write and onboarding behavior is not covered by agmsg's
read-API compatibility promise. The consumer contract must detect drift before users encounter
silent delivery failure.

The contract verifies observable behavior, not log wording or private storage:

- joining assigns the requested pane identity;
- sending with `--force` preserves sender, recipient, and arbitrary body text;
- reads return the required JSONL fields in source order with opaque IDs;
- reads do not mark messages as consumed;
- identity claims and pane-specific watchers isolate same-project Claude sessions;
- dead claim owners can be replaced;
- installed version normalization matches agmsg's supported provenance forms.

### Tier 1: hermetic

Every `make check` run verifies panemux's command construction and parses captured JSONL fixtures
from the tested agmsg release. Fixtures are regenerated from a real tagged installation, never
hand-authored. This tier catches regressions in panemux without requiring agmsg, a network, or
external state.

### Tier 2: real installation

`make test-agmsg-contract AGMSG_PATH=...` runs the same contract against an installed agmsg through
panemux's real local client.

CI runs it:

- on relevant pull requests against `board.TestedAgmsgVersion`;
- on a schedule against the latest agmsg release.

The scheduled canary is an early warning; it does not automatically move the tested-version pin.
The job remains outside `make check` because it installs and executes an external project. Local
runs skip when no explicit agmsg path is supplied.

Assertions must describe facts Agent Board depends on. Exact diagnostic sentences, timing based on
fixed sleeps, and incidental process ancestry are not contract behavior.
