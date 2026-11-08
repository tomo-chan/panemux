---
type: Decision
title: "Manual installer diagnostics and non-release versions (2026-10-31, #435 review)"
description: "Rationale for Manual installer diagnostics and non-release versions."
---

# Manual installer diagnostics and non-release versions (2026-10-31, #435 review)

Target prerequisites and git acquisition use the shared input lane so failures retain output on
both local and SSH hosts. Git diagnostics name the source operation rather than task collection.
Only exact release/tag spellings are comparable: describe builds, dirty trees and prereleases
are unknown, warn, and are never installed over. Prerequisites remain inside B1's reserved update
window to preserve prompt overlap refusal and avoid racing another installer. Moving slow source
preparation out of that window would need separate preparation reservation semantics.

Current contracts: [agent board guide](../../agent-board.md).
