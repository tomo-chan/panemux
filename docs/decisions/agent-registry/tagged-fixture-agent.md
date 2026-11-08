---
type: Decision
title: "A third agent behind a build tag (2026-10-24, issue #377)"
description: "Verify extensibility with a tagged third agent and isolated processes instead of production collection switches."
---

# A third agent behind a build tag (2026-10-24, issue #377)

A build-tagged third agent verifies extensibility without test seams or hidden registry flags
in shipped binaries. Selected tagged tests assert required contract names so renaming cannot silently
shrink coverage. Default tests keep their shipped-registry expectations.
Mutation measures the default build and reports ignored fixture-tag files; a second tagged mutation
run conflicts with tests pinning shipped agents. Lint separately covers tagged code.
Browser fixtures use real tagged collection with fake CLIs. Review found developer processes being
read; ps/lsof stand-ins now expose fixture descendants only. Production collection switches and
container-only isolation were rejected because local runs need protection too.
See [tagged build contract](../../quality-gateway.md#the-fixture-agents-tagged-build).
