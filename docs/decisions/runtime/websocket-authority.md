---
type: Decision
title: "WebSocket route separation and browser authority guard (#297, #298)"
description: "Separate fixed streams from pane IDs and validate browser authority without trusting forwarded headers."
---

# WebSocket route separation and browser authority guard (#297, #298)

The command stream moved from `/ws/board-command` to `/ws/board/command` so the valid
`board-command` pane can keep its terminal URL. A legacy alias would preserve the collision,
so command clients must update and old tabs must reload. Fixed streams use two segments,
while terminal IDs occupy one.

All WebSockets and already-guarded task/SSH APIs now share hostname/effective-port Origin
validation and reject same-site/cross-site Fetch Metadata. The former arbitrary-loopback-port
exception exposed terminal input to other local web pages. Fetch Metadata alone was rejected
because Chrome WS upgrades were observed without it. Both Vite proxies preserve Host and Origin;
HTTP string proxy shorthand had changed Host and would have broken strict validation.

Scheme matching against backend TLS was rejected because existing deployments terminate TLS
upstream. Host without a port uses Origin's scheme default (http 80, https 443); same explicit
host/port across schemes remains an accepted limitation. Forwarded headers are not trusted,
no public-Origin setting was added, and CLI Origin-free access remains supported. Protecting
all remaining side-effect APIs and DNS rebinding are separate work.

Current contracts: [runtime guide](../../behavior.md).
