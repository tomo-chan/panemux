# Agent Board: agmsg compatibility contract

> Part of the [Agent Board design](../agent-board.md). Read that document's status note first — it says which parts of this design are shipped.

## agmsg compatibility contract

agmsg's own compatibility promise only covers reading through `scripts/api.sh` (see [Version
pinning](agmsg-integration.md#integration-with-agmsg)); `send.sh`, `join.sh`, and everything else this design depends on
carry no such promise. Without a mechanical check, an agmsg upgrade that changes one of those
scripts' behavior would surface as a pane silently failing to communicate, discovered by a user
long after the fact rather than by CI at the moment it happened.

**What this borrows from [Pact](https://docs.pact.io/), and what it deliberately doesn't.** Pact's
core idea — the *consumer* writes down the exact interactions it depends on as a contract, and that
contract is mechanically verified against the real *provider* — is exactly the right shape here:
panemux is the consumer, agmsg is the provider, and [Integration with
agmsg](agmsg-integration.md#integration-with-agmsg)'s precise, source-verified prose about `api.sh`/`send.sh`/`join.sh`
*is* that contract in everything but file format. What doesn't transfer is Pact's actual
machinery: Pact is built for HTTP/message request-response between two services that both
participate in verification (typically via a shared Pact Broker, with the *provider's own CI*
replaying the consumer's recorded interactions against itself). agmsg is a CLI script tool whose
maintainers are not signed up for any such loop, and there is no request/response protocol here to
generate Pact's consumer-side mocks from in the first place — shoehorning the actual Pact
tooling/broker onto shell-exec fixtures would add a heavyweight dependency for no benefit over a
plain table-driven Go test. So: adopt the *idea*, skip the *tool*.

**Two test tiers, not one:**

- **Tier 1 — fast, hermetic, part of `make check` on every commit.** Implemented. A fake
  `AgmsgClient`/`BoardExecutor` asserts the exact command strings panemux builds for each operation,
  and `internal/board/agmsg_fixture_test.go` parses frozen JSONL in
  `internal/board/testdata/agmsg-v1.2.0/` the way real `api.sh` output is parsed. Those fixtures are
  **captured, not hand-written** — `testdata/agmsg-v1.2.0/capture.sh` regenerates them by installing
  agmsg at that tag into a throwaway `HOME` and recording what `api.sh` actually printed. This
  protects panemux's own code from regressing against its own documented assumptions; it cannot, by
  itself, detect that agmsg changed, since it never touches a real agmsg install at test time.
- **Tier 2 — a separate CI job that runs the same contract against a real agmsg install.**
  Implemented: `make test-agmsg-contract AGMSG_PATH=...` (`internal/board/agmsg_contract_test.go`),
  run in CI by `.github/workflows/agmsg-contract.yml`. The job installs agmsg through its own
  documented installer (`install.sh --cmd agmsg`, the same path an operator takes) into an ephemeral
  runner, then drives `join.sh`/`identities.sh`/`actas-claim.sh`/`watch.sh` directly and
  `send.sh`/`api.sh` **through panemux's own `LocalAgmsgClient`** — deliberately, since a test that
  rebuilt those invocations itself would keep passing after panemux started sending something
  different. It runs in both situations this contract calls for: **on a schedule** against agmsg's latest
  release tag, as an early warning before anyone here has chosen to bump the pin, and **on pull
  requests** against the pinned version, so a PR that bumps the pin cannot merge on a version whose
  real behavior differs. The canary polls daily rather than weekly because agmsg's median gap
  between releases is 2.9 days (22 releases, `v1.0.2`→`v1.2.2`), so a weekly poll would straddle
  several releases and leave a failure unattributable; it caches which tag it last verified, so the
  work it actually does happens once per agmsg release, and a failing release is retried every day
  until it is handled. The PR trigger deliberately carries no `paths:` filter — a
  path-filtered workflow reports no status at all on the PRs it skips, and a required check that
  never reports blocks every one of them — so a fast `scope` job decides instead whether installing
  agmsg is warranted, and the contract job always reports rather than skipping itself, which lets the
  check be marked required in branch protection (a repository setting; see
  [maintenance.md](../maintenance.md#the-agmsg-compatibility-contract-job)). It stays out of
  the main `make check` gate, and the test skips itself when `PANEMUX_AGMSG_PATH` is unset, so a
  contributor without agmsg installed still gets a green, hermetic local run.

This does not remove the underlying risk noted in [Known limitations](limitations.md#known-limitations) — agmsg
still makes no compatibility promise for the scripts panemux's write path depends on — but it turns
a silent, user-discovered failure into a specific, actionable CI signal naming exactly which
documented behavior changed.

**It has already paid for itself four times, which is worth recording as evidence rather than as a
claim.** Running the contract against a real install for the first time found (1) that two of its
own pre-existing assertions described behavior agmsg does not have — `watch.sh` prints nothing at
all naming the pairs it resolved when it skips none, so "which identities did this watcher
subscribe to" was being read off a log line that only exists in the *other* branch; the assertions
now observe message *delivery* instead, which is both what a user experiences and visible in either
branch — and (2) the numeric-cursor bug described under [Integration with
agmsg](agmsg-integration.md#integration-with-agmsg), which every hermetic test missed because the hand-written fixtures
carried integer ids that no real 1.2.0 install emits.

The third came from the CI job itself, on its first run, and was a flaw in the contract tests rather
than in panemux: they passed locally and failed on a runner. agmsg keys its actas exclusivity locks
on an instance id of `<session_id>.<agent pid>` and treats a lock as live only while that pid is,
resolving the pid by walking its own ancestors for an agent process. Run from inside a real Claude
Code session, the tests silently inherited that session's pid and every lock looked live; on a
runner, with no agent process anywhere in the tree, every lock read as stale and reclaimable. The
assertions were reading the harness's own environment rather than agmsg's behavior. They now declare
the owning process explicitly through agmsg's own `AGMSG_AGENT_PID` override, and cover the other
half of the same rule — a lock whose owning process has exited must be reclaimable, or a crashed
pane would block its own ID forever. The same run also exposed a fixed-sleep delivery probe timed at
the watcher's own 5-second poll interval; it now sends before the watcher starts and waits on the
delivery itself.

The fourth is the canary doing exactly the job it was built for, and it is worth reading as a
calibration of what these assertions should be made of. agmsg v1.3.1 rewrote one `watch.sh` line —
`skipping pairs held by other sessions` became `not serving these pairs (held by another session, or
unverified)`, because that report now folds in a pair it could not verify — and the scheduled run
went red the morning after the release while every behavioral assertion around it passed: the
claimed pane's messages were still withheld from the other session's watcher, and the dropped pair
was still named. So the contract had a *sentence* in it where it meant to have a *fact*. The
assertion now reads "the watcher names the pair it dropped" (`contract/pane-a`), which is what
panemux's bootstrap instruction depends on being told and which is identical in both releases; if a
release ever stops naming the pair, that is a real change and it still fails. Verified by installing
both `v1.2.0` and `v1.3.1` and running the contract against each. The pin
(`board.TestedAgmsgVersion`) deliberately stays at 1.2.0: a canary failure is an early warning, and
moving the pin is its own change, made against a full run on the new version.
