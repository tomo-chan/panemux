---
type: Decision
title: "Agent availability: a separate probe and an anti-corruption layer for formats (2026-10-16, issue #373)"
description: "Rationale for Agent availability: a separate probe and an anti-corruption layer for formats."
---

# Agent availability: a separate probe and an anti-corruption layer for formats (2026-10-16, issue #373)

CLI lookup uses slow/noisy login shells, so a separate background probe avoids blocking state
collection or corrupting its protocol. Host/connection caching and one run per host preserve prior
results while refreshing. It never runs a CLI for a version or drops a healthy connection on timeout.
Unknown statuses/events become unknown/degraded rather than falling back to stale events and
guessing live state. Bounded diagnostics log only on change. See [registry](../../agent-registry.md).
