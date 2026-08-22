# Maintenance Guide

This document captures repository maintenance rules that belong in durable project documentation rather than in the agent index.

## GitHub Actions Pinning

- Pin GitHub Actions to full commit SHAs, not floating tags such as `@v4` or `@v5`.
- When updating workflows, resolve the intended tag to its exact commit SHA first, then commit the SHA-pinned reference.
- Renovate manages scheduled GitHub Actions updates for this repository. Keep workflow `uses:` entries SHA-pinned so Renovate can advance them as explicit reviewable changes.

Why this matters:

- it makes workflow execution reproducible
- it narrows supply-chain drift
- it keeps workflow changes explicit in code review

## Dependency Updates

- Renovate is the repository's scheduled dependency update mechanism.
- Weekly update PRs run before 9am on Monday in the `Asia/Tokyo` timezone.
- Monthly lockfile maintenance runs before 9am on the first day of the month.
- Renovate covers Go modules, frontend npm dependencies, GitHub Actions, and the `GOLANGCI_LINT_VERSION` pin in `Makefile`.
- Review Renovate PRs like any other dependency bump and keep automerge disabled unless project policy changes.

## The agmsg Compatibility Contract Job

`.github/workflows/agmsg-contract.yml` runs Tier 2 of the [agmsg compatibility contract](agent-board.md#agmsg-compatibility-contract): `make test-agmsg-contract` against a real agmsg install, which the hermetic `make check` suite deliberately cannot do.

- **Weekly (Mondays, 06:00 UTC)** it installs agmsg's **latest release tag** and runs the contract. This run gates nothing — a failure is an early warning that an agmsg release changed behavior panemux depends on, arriving before anyone here has chosen to bump the pin.
- **On every pull request** a fast `scope` job checks whether the PR touches the pin, the contract test, its fixtures, or the client code the contract drives. If it does, the contract runs against the **pinned** version; if not, the job reports success without installing anything.

The PR trigger has no `paths:` filter on purpose. A path-filtered workflow reports no status on the PRs it skips, and a required check that never reports blocks every one of them — hence the always-running `scope` job. That is what makes this workflow safe to mark as a **required check** in branch protection, which is how the contract's "a bump of the pin cannot merge unverified" requirement is actually enforced.

### Bumping the pinned agmsg version

The pin is `board.TestedAgmsgVersion` in `internal/board/agmsg_version.go`, and the workflow reads it from that file rather than duplicating it. To bump it:

1. Update `TestedAgmsgVersion`.
2. Recapture the Tier 1 fixtures at the new version and rename their directory to match — see `internal/board/testdata/agmsg-v1.2.0/README.md` and its `capture.sh`.
3. Update `fixtureDir` (and the quoted fixture ids) in `internal/board/agmsg_fixture_test.go`.
4. Push. The contract job runs against the new pin automatically, because step 1 touches a scoped path.

A weekly canary failure is a signal to investigate agmsg's changelog, not to bump the pin reflexively: the pinned version is the one panemux's prose and fixtures were verified against, and it keeps working until it is deliberately moved.

### Running it locally

```sh
git clone https://github.com/fujibee/agmsg.git /tmp/agmsg-src
cd /tmp/agmsg-src && ./install.sh --cmd agmsg
make test-agmsg-contract AGMSG_PATH=~/.agents/skills/agmsg
```

`sqlite3` must be on `PATH` — agmsg's own scripts require it, and the test skips itself when it is missing, as it does when `AGMSG_PATH` is unset.

## Release Workflow

### release-please handling

- Never manually close a release-please PR.
- Closing it leaves the `release-please--branches--main` tracking branch stale.
- On the next push to `main`, release-please may recreate a PR with incorrect release notes that include historical commits.

### Overriding a proposed version

If release-please proposes the wrong version:

- add the `release-as: x.y.z` label to the existing release PR before merging, or
- include `Release-As: x.y.z` in the footer of a commit message on `main`

### Recovering from a stale internal branch

If the internal release-please branch becomes stale:

```sh
gh pr close <release-pr-number>
git push origin origin/main:refs/heads/release-please--branches--main --force
```

Close the incorrect PR first, then reset the tracking branch.

## Related Documents

- Developer workflow and PR rules: [../DEVELOPMENT.md](../DEVELOPMENT.md)
- Architecture and security design: [architecture.md](architecture.md)
- Runtime behavior and operational assumptions: [behavior.md](behavior.md)
