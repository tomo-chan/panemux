---
type: Decision
title: "Layout responses are normalized before frontend parsing (2026-09-03, PR #198)"
description: "Normalize hand-written layouts at the API boundary before strict parsing without forcing an immediate configuration save."
---

# Layout responses are normalized before frontend parsing (2026-09-03, PR #198)

Valid hand-written config can omit `direction`/`children` or put a lone pane at the layout root,
while the browser contract requires a normalized recursive node. The API now fills missing keys,
relocates a lone root pane into its implied child, and validates that pane exactly like every other
pane. The normalized form is persisted only on the next explicit save.

Current contracts: [runtime guide](../../behavior.md).
