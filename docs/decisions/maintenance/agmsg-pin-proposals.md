---
type: Decision
title: "agmsg pin bump proposals (2026-10-31, issue #437)"
description: "Use deduplicated GitHub App proposals for verified pins while withholding publishing secrets from upstream capture code."
---

# agmsg pin bump proposals (2026-10-31, issue #437)

Successful real-install verification proposes a release/SHA and captured fixtures using a GitHub
App: GITHUB_TOKEN-created PRs do not trigger normal PR CI. Resolve/peel a release tag once so
retagging cannot change reviewed source. The original ordering-regression fixture remains frozen.
Token minting runs separately after validating a data-only artifact and testing trusted code;
upstream code gets no App secrets. Only JSONL/provenance cross that boundary.
Release-specific branches deduplicate even closed PRs. New releases get new proposals instead of
force-updating human decisions; a branch left by failed PR creation can be reused.
See [maintenance](../../maintenance.md) for the current update and permission contract.
