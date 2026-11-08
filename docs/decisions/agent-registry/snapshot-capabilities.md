---
type: Decision
title: "Capabilities in the task snapshot (2026-10-23, issue #376)"
description: "Rationale for Capabilities in the task snapshot."
---

# Capabilities in the task snapshot (2026-10-23, issue #376)

Capabilities/availability/actions live in the existing snapshot. A new agents endpoint would
need another guard/schema and force the UI to reconcile two answers. The server derives eligibility
from adapters instead of duplicating agent-name/UUID checks in the browser.
Consumer-owned notes keep task-dependent facts out of registry declarations.
See [registry](../../agent-registry.md) for the authoritative API contract.
