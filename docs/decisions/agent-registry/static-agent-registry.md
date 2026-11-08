---
type: Decision
title: "One registry declares agents; capabilities are ports in their consumers (2026-10-12, issue #372)"
description: "Use explicit static declarations and consumer-owned ports instead of configuration or implicit linking order."
---

# One registry declares agents; capabilities are ports in their consumers (2026-10-12, issue #372)

Agent aliases/process rules drifted across consumers. Static explicit registration centralizes
declarations without config-injected patterns/scripts; init registration was rejected because link
and filename order would decide matching precedence.
Capability ports belong to consumers in their own types, avoiding speculative neutral interfaces.
Comparable host/agent/session keys need no escaping and support multiple sessions per daemon.
Unknown external formats fail visibly instead of guessing. See [registry](../../agent-registry.md).
