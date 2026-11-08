---
type: Decision
title: "Collection is composed from per-agent collect adapters (2026-10-16, issue #373)"
description: "Rationale for Collection is composed from per-agent collect adapters."
---

# Collection is composed from per-agent collect adapters (2026-10-16, issue #373)

Constant per-agent fragments replace monolithic collection without letting request/config/host
values become script source. Namespaced sections changed protocol grammar; adding adapter sections
alone does not change the common version. Missing declared sections fail instead of pretending
there are no sessions. Live sections precede ps to drop processes disappearing mid-observation.
Full and attention paths share live normalization. Ports stay in tasks to avoid import cycles,
and startup checks adapters against declarations. See [registry](../../agent-registry.md).
