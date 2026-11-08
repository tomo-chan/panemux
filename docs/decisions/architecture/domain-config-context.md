---
type: Decision
title: "Domain config and file context use separate types (2026-09-20, issue #66, PR #240)"
description: "Rationale for Domain config and file context use separate types."
---

# Domain config and file context use separate types (2026-09-20, issue #66, PR #240)

`config.Data` is the serializable domain model and preserves user-facing YAML section order.
`config.Config` embeds it and adds load/save context such as file and SSH-config paths. The earlier
single type mixed persisted data with runtime context; a separate serialization struct then had to
repeat every config section and silently dropped a section when that copy was not updated.

Current contracts: [architecture guide](../../architecture.md).
