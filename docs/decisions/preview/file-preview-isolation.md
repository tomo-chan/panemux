---
type: Decision
title: "2026-10-15: The split preview's isolation and bounds (#385)"
description: "Use opaque-origin isolation and root-based lookup instead of arbitrary absolute host-file previews."
---

# 2026-10-15: The split preview's isolation and bounds (#385)

Opaque-origin CSP on the existing server was chosen over another listener with port/lifetime
management. Frame sandbox repeats the boundary; token URLs let sandboxed resources load without
passing the API guard, and tabs outlive their source panes/sessions.
CodeQL findings led to rejecting arbitrary absolute-file previews. Lookup and serving both use
os.Root within session/worktree roots, preventing path/symlink escapes. String-only checks rejected
logical /tmp versus resolved /private/tmp forms and alternate case; directory identity recognizes
the same allowed root without expanding scope.
See [preview security](../../security/preview.md) for authoritative paths, limits and isolation.
