---
type: Decision
title: "2026-10-11: One browser authority boundary for APIs and WebSockets (#360)"
description: "Guard APIs before routing and preflight so other loopback origins cannot trigger side effects with simple requests."
---

# 2026-10-11: One browser authority boundary for APIs and WebSockets (#360)

Isolated preview pages can use a different loopback port, so permitting every localhost port
through CORS would let such pages reach the terminal server. CORS also cannot prevent simple
requests from executing handlers. All API and WS paths therefore apply the shared browser
request guard before routing and preflight responses; API mounts enforce it independently
before handlers. Development uses Vite’s existing same-origin proxy rather than a permanent
5173 allowlist. Origin-free CLI requests remain supported, while browser evidence without
Origin requires a matching Referer and consistent Fetch Metadata. Missing evidence and direct
navigation to protected APIs fail closed. Authority comparison preserves the existing TLS
termination contract and its documented scheme limitation; authentication and broader DNS
rebinding protection remain separate work.

Current contracts: [runtime guide](../../behavior.md).
