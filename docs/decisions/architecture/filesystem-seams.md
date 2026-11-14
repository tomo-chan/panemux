---
type: Decision
title: "Filesystem globals use named seams (2026-09-10 through 2026-09-13, PRs #224, #231, #234)"
description: "Provide named home, cache and persistent-write seams instead of unrelated process-global test overrides."
---

# Filesystem globals use named seams (2026-09-10 through 2026-09-13, PRs #224, #231, #234)

Home-directory, cache-directory, and persistent-write behavior had been overridden independently by
callers or manipulated through process-wide environment variables in tests. The repository adopted
`internal/homedir`, `internal/cachedir`, and `internal/fileops` as one named seam per global
capability. The separation is deliberate: home and cache lookup have different platform semantics,
and atomic file replacement has different safety properties from either lookup.

These seams make failure paths injectable and give lint a single allowed location for direct OS
lookups. They are not safe for parallel substitution; tests that override them remain non-parallel.

Current contracts: [architecture guide](../../architecture.md).
