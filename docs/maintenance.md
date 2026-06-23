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
