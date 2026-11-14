---
type: Runbook
title: "agmsg compatibility maintenance"
description: "Running the agmsg compatibility workflow and reviewing automated pin proposals."
---

# agmsg compatibility maintenance

## The agmsg Compatibility Contract Job

`.github/workflows/agmsg-contract.yml` runs Tier 2 of the [agmsg compatibility contract](../agent-board/agmsg-contract.md#agmsg-compatibility-contract): `make test-agmsg-contract` against a real agmsg install, which the hermetic `make check` suite deliberately cannot do.

- **Daily (06:00 UTC / 15:00 JST)** scope resolves the newest stable `vN.N.N` tag with
  `git ls-remote --tags`, excluding `app-v*` and prereleases. Annotated tags use the peeled
  commit (`^{}`). All later checkout, installation and capture use that full SHA. Failure
  leaves the current pin intact and produces no proposal.
- **On every pull request** scope selects changes to `internal/agmsg/`,
  `scripts/agmsg_contract*`, `Makefile`, or this workflow. Relevant PRs run against
  `TestedAgmsgCommit` from the PR checkout; others report success without installation.
- **Manual dispatch** accepts a stable tag or full SHA, defaults to the pinned SHA, and never
  creates a PR. Tags are resolved once before installation.

The daily poll suits agmsg's release cadence (the measured median gap across 22 releases in
2026-06 through 2026-08 was 2.9 days). Successful verification is cached by **tag and commit
SHA**, so a moved tag is rechecked. Failed verification is never cached. A cached success still
retries proposal creation after missing credentials or transient GitHub failures. The proposal
job depends on the contract job succeeding and cannot run after contract failure.

The PR trigger has no `paths:` filter on purpose. A path-filtered workflow reports no status on the PRs it skips, and a required check that never reports blocks every one of them — hence the always-running `scope` job. The `contract` job likewise always runs and gates its expensive steps individually rather than skipping itself, so it always reports a real success or failure and never a `skipped` conclusion whose scoring branch protection would have to be trusted to get right.

That is what makes **`agmsg-contract / contract`** safe to mark as a required check in branch protection — which is how the contract's "a bump of the pin cannot merge unverified" requirement is actually enforced. Marking it required is a repository setting, not something this workflow can do for itself; until someone sets it, the job still runs and still reports, it just does not block.

### Bumping the pinned agmsg version

The pin is the pair `agmsg.TestedAgmsgVersion` (`internal/agmsg/agmsg_version.go`) and
`agmsg.TestedAgmsgCommit` (`internal/agmsg/tool.go`). After scheduled verification succeeds,
`scripts/agmsg_contract.py` prepares a `fix: bump pinned agmsg to vN.N.N` PR against main:

1. Update both constants to the verified version and SHA, only for a newer release.
2. Run `capture.sh` against the already checked-out SHA in an isolated HOME. This works on CI
   with sqlite3 and Python 3; no manual capture is needed. The new versioned directory contains
   raw JSONL, VERSION and COMMIT provenance. Project paths are sanitized to `/tmp/sample-project`.
3. Fixture tests derive their directory from TestedAgmsgVersion and expected IDs/timestamps from
   the captured data, so a bump changes no test code. Run the full agmsg and board packages.
   Preserve the original v1.2.0 capture for the ID-order regression, because fresh captures need
   not reproduce out-of-order UUIDs within one millisecond.
4. Create branch `automation/agmsg-vN.N.N` and its PR using a GitHub App token. PR CI rechecks
   the recorded SHA, independent of later tag movement.

One tag has at most one proposal: open, closed and merged PRs on that branch suppress duplicates.
A push that succeeded before PR creation failed is reused without force-pushing after its pin is
checked. Preflight skips existing proposals before capture and resumes a pushed branch without
rerunning upstream code. New tags receive
separate PRs; older proposals stay open and unchanged for human comparison and closure. Deliberately
closed proposals are not reopened. Scheduled runs are serialized by workflow concurrency. Moved
tags are reverified but do not rewrite existing proposals; investigate upstream retagging manually.
The automation never downgrades the pin or follows upstream main.

Review upstream changes, the version/SHA pair, captures and CI, then merge the proposal. Its `fix:`
title makes release-please propose a patch; publishing the draft remains manual. A failed canary
calls for investigation, not an unverified bump.

### GitHub App setup (repository owner)

Create a GitHub App with repository permissions **Contents: read and write** and
**Pull requests: read and write** (Metadata read is implicit), and install it on this repository.
Actions/Workflows write is unnecessary: proposals change only `internal/agmsg/`. Register the App
ID as repository Actions secret `AGMSG_BUMP_APP_ID` and its generated private key as
`AGMSG_BUMP_APP_PRIVATE_KEY`. App creation, installation and secret registration are manual.

`actions/create-github-app-token` mints a short-lived token scoped to this repository with those
two write permissions. Unlike GITHUB_TOKEN, its events trigger ordinary PR CI. Upstream installation
and capture run in a job with read-only repository permission and no App secrets. A separate fresh
publication runner checks out trusted code and accepts only one bounded JSON artifact containing
the three JSONL captures, VERSION and COMMIT. It checks names, regular-file status, sizes, JSON
shape and provenance against the independently resolved tag/SHA; no uploaded source, scripts,
paths, hooks or executable modes are applied. Trusted code writes the constants and data, then
tests the full agmsg and board packages before minting the token. Scope needs contents/PR read;
other jobs need only contents read until the App token. If either secret is absent, the workflow
reports no proposal and succeeds after contract verification.
The automation changes no App settings or secrets.

### Running it locally

```sh
make test-agmsg-contract AGMSG_PATH=/tmp/agmsg-home/.agents/skills/agmsg
make test-agmsg-automation
```

For the first command, clone agmsg into a temporary directory, check out the desired full SHA,
and install with a throwaway HOME and cleared XDG/agent configuration overrides. Never install
into the developer's own skills/settings. sqlite3 and an explicit installation are required;
otherwise the contract skips. The second command uses Python 3 and offline fake process boundaries
and temporary git repositories.
