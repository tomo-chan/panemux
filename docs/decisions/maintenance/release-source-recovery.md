---
type: Decision
title: "Release recovery uses the original source with GNU coreutils (2026-11-02, PR #479)"
description: "Rationale for Release recovery uses the original source with GNU coreutils."
---

# Release recovery uses the original source with GNU coreutils (2026-11-02, PR #479)

A failed draft must be rebuilt from its recorded source SHA rather than silently taking later
product changes. Ubuntu 26.04's uutils mkdir accepted EEXIST without -p, allowing two installers
to claim the same lock; installing zsh did not fix it. Repeated verification of the unchanged
release source justified Ubuntu 24.04's GNU coreutils instead of ignoring cleanup errors,
retrying until green, or overlaying new scripts on old source.
The [upstream correction](https://github.com/uutils/coreutils/commit/bf423f388d543b9180db6b7283d82b301f138913)
explains the defect. Draft metadata needs publisher write permission; per-tag serialization and
rejecting existing assets protect recovery uploads. A main-ref condition is not a boundary against
workflow writers. See [release maintenance](../../maintenance.md#recovering-a-failed-release-build)
for the authoritative procedure. The tool-lock limitation remains [#480](https://github.com/tomo-chan/panemux/issues/480).
