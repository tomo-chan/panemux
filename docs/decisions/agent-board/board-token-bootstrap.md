---
type: Decision
title: "Browser clients obtain the board token from a dedicated bootstrap endpoint (2026-08-14, PR #170)"
description: "Bootstrap browser tokens through a dedicated local endpoint outside the authenticated board subtree."
---

# Browser clients obtain the board token from a dedicated bootstrap endpoint (2026-08-14, PR #170)

The original command-center design covered how its subprocess received a bearer token but not how
the bundled frontend learned a randomly generated token. `GET /api/session-token` was added as the
bootstrap contract. It is deliberately outside the authenticated `/api/board/*` subtree and is
limited to the local/trusted deployment model documented in [security/auth.md](../../security/auth.md).
