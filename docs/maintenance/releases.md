---
type: Runbook
title: "Release maintenance"
description: "Recovering release builds and maintaining release-please proposals."
---

# Release maintenance

## Release Workflow

### Recovering a failed release build

The `release-please` workflow also accepts a manual `release_tag`, for example
`gh workflow run release-please.yml --ref main -f release_tag=v0.27.0`. Recovery
has a main-ref condition to prevent accidental dispatch from another branch;
this is not a security boundary against writers who can edit a branch's workflow.
Use it after the workflow fix has merged to main.

Both push and recovery resolve the draft inside the GoReleaser job, using
`contents: write` because draft visibility requires push access. They require a
draft whose name and tag match the requested tag, whose asset list is empty, and
whose `target_commitish` is a full commit SHA. Both check out that recorded SHA,
even when release-please creates a draft for an earlier merge than the current
push, and verify HEAD against it before creating a local tag. Both refuse an
existing tag that points elsewhere; neither path moves tags. Metadata lookup,
validation and upload share a per-tag concurrency group, preventing simultaneous
uploads to that draft without blocking builds of other tags or release-please.
GitHub keeps one active and one pending job per group: a newer job can replace
an older pending recovery. Check a dispatch's outcome and dispatch again if it
was superseded; active uploads are not cancelled.

Recovery does not run release-please again or change the proposed next version.
The current workflow
provides runner setup, while the selected release commit provides the source,
runtime pins, GoReleaser configuration and full `make check` gate.

Manual recovery passes `--draft`: after GoReleaser succeeds, inspect the draft's
four platform archives and `checksums.txt`, then publish it. The push path retains
the default `release --clean` behavior, which publishes after a successful build;
the manual inspection/publication step applies to recovery only.
A recovery failure leaves the draft unpublished; inspect the failed check before
retrying. Leftover assets are rejected before checkout, dependency setup or tests.
If an upload partially succeeded,
remove every asset left on that draft (including `checksums.txt`) with
`gh release delete-asset <tag> <asset-name>` before retrying: the release
configuration does not replace existing assets, and mixing builds would leave
inconsistent checksums. Confirm it is still a draft before deleting any assets.
Do not move an already published tag.

### release-please handling

- Never manually close a release-please PR.
- Closing it leaves the `release-please--branches--main` tracking branch stale.
- On the next push to `main`, release-please may recreate a PR with incorrect release notes that include historical commits.
- The release PR bumps `.release-please-manifest.json` itself and, through `extra-files` in `release-please-config.json`, three version fields: `$.version` in `frontend/package.json`, and `$.version` and `$.packages[""].version` in `frontend/package-lock.json`. Add any other file that records the release version to `extra-files`.

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
