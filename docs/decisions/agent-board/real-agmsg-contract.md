---
type: Decision
title: "Compatibility is checked against a real agmsg release (2026-08-23, PR #176)"
description: "Check captured CLI assumptions against a real pinned installation and daily latest-release canary."
---

# Compatibility is checked against a real agmsg release (2026-08-23, PR #176)

Fixture tests could not detect false assumptions about `watch.sh` output or the shape of event-log
message IDs. A second contract tier now installs the pinned agmsg version in CI and runs daily
against the latest release. Its first live run found that treating IDs as numbers prevented the
relay cursor from advancing; the current cursor contract treats IDs as opaque ordered values from
the returned stream.

The design borrows Pact's consumer-owned contract principle, but not Pact tooling: agmsg is a CLI
dependency without an HTTP/message provider verification loop or broker. Direct Go tests against
the installed scripts verify the actual boundary with less machinery.

Current contracts: [agent board guide](../../agent-board.md).
