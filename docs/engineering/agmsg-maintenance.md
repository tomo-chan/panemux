---
type: Runbook
title: "agmsg compatibility maintenance"
description: "Running the agmsg compatibility workflow and reviewing automated pin bump proposals."
---

# agmsg compatibility maintenance

## The agmsg Compatibility Contract Job

`.github/workflows/agmsg-contract.yml` runs Tier 2 of the
[agmsg compatibility contract](../agent-board/agmsg-contract.md#agmsg-compatibility-contract):
`make test-agmsg-contract` against a real agmsg install, which hermetic `make check` cannot run.

- **Daily (06:00 UTC / 15:00 JST)**: Resolves the newest stable `vN.N.N` tag via
  `git ls-remote --tags` (excluding `app-v*` and prereleases, peeling annotated tags with `^{}`).
  All subsequent checkout, install, and capture use that full SHA; failure leaves the pin intact.
- **On every pull request**: `scope` selects changes to `internal/agmsg/`, `scripts/agmsg_contract*`,
  `Makefile`, or this workflow. Relevant PRs run against `TestedAgmsgCommit` from the PR checkout;
  others report success without installation.
- **Manual dispatch**: Accepts a stable tag or full SHA (defaulting to the pinned SHA), resolves
  tags once before installation, and never creates a PR.

Successful verification is cached by **tag and commit SHA** (moved tags recheck; failures are never
cached; cached successes still retry proposal creation after transient GitHub errors; proposal runs
only after contract success). The PR trigger intentionally has no `paths:` filter—`scope` and
`contract` always run and report real success/failure (never `skipped`) so `agmsg-contract /
contract` can be configured as a required branch-protection check.

### Bumping the pinned agmsg version

The pin is `agmsg.TestedAgmsgVersion` (`internal/agmsg/agmsg_version.go`) paired with
`agmsg.TestedAgmsgCommit` (`internal/agmsg/tool.go`). After scheduled verification passes,
`scripts/agmsg_contract.py` prepares a `fix: bump pinned agmsg to vN.N.N` PR against `main`:

1. Update both constants for a newer stable release only (never downgrade or follow upstream `main`).
2. Run `capture.sh` against the checked-out SHA in an isolated `HOME` (sanitizing project paths to
   `/tmp/sample-project`), recording raw JSONL, `VERSION`, and `COMMIT`.
3. Fixture tests derive paths and IDs from `TestedAgmsgVersion` and captured data; run full `agmsg`
   and `board` tests while preserving the original `v1.2.0` capture for the ID-order regression.
4. Push `automation/agmsg-vN.N.N` and open its PR with a GitHub App token so PR CI rechecks the
   recorded SHA independently of later tag movement.

One tag gets at most one proposal (open, closed, and merged PRs deduplicate; a previously pushed
branch whose PR creation failed is reused without force-pushing; deliberately closed proposals are
not reopened; moved tags reverify without rewriting existing proposals). Review upstream changes,
the version/SHA pair, captures, and CI before merging; a failed canary requires investigation, not
an unverified bump.

### GitHub App setup (repository owner)

Create a GitHub App with **Contents: read and write** and **Pull requests: read and write**
(Actions/Workflows write is not needed: proposals touch only `internal/agmsg/`), install it on the
repository, and set secrets `AGMSG_BUMP_APP_ID` and `AGMSG_BUMP_APP_PRIVATE_KEY`.

Upstream installation and capture run in a read-only job without App secrets. A separate
publication runner checks out trusted code, validates one bounded JSON artifact (names, regular
files, sizes, JSON shape, and provenance against the independently resolved tag/SHA—applying no
uploaded scripts or executable modes), writes constants/captures, runs full `agmsg` and `board`
tests, and only then mints the short-lived App token (`actions/create-github-app-token`, which
triggers normal PR CI unlike `GITHUB_TOKEN`). Missing secrets report no proposal without failing
contract verification.

### Running it locally

```sh
make test-agmsg-contract AGMSG_PATH=/tmp/agmsg-home/.agents/skills/agmsg
make test-agmsg-automation
```

For the first command, clone agmsg into a temporary directory, check out the target full SHA, and
install with a throwaway `HOME` and cleared XDG/agent overrides (never into your own skills or
settings; requires `sqlite3`). The second command runs offline Python 3 tests with fake process
boundaries and temporary git repositories.
