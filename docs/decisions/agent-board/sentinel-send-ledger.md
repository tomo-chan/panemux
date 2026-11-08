---
type: Decision
title: "The own-send ledger authenticates sentinel-attributed rows (2026-08, PR #162)"
description: "Rationale for The own-send ledger authenticates sentinel-attributed rows."
---

# The own-send ledger authenticates sentinel-attributed rows (2026-08, PR #162)

`send.sh --force` does not authenticate the free-text `from` value, so trusting every row from the
reserved `_system` identity allowed impersonation. Panemux now accepts such a row only when it
matches a recent send recorded by panemux itself. Model checking later formalized this multiset
behavior; see [agent-board/model-checking.md](../../agent-board/model-checking.md).
