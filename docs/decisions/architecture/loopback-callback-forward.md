---
type: Decision
title: "Loopback callbacks use a panemux-host forward (2026-08-23, PR #177)"
description: "Rationale for Loopback callbacks use a panemux-host forward."
---

# Loopback callbacks use a panemux-host forward (2026-08-23, PR #177)

CLI login flows in remote panes open `localhost` callback URLs on the SSH host, while the browser
runs on the panemux host. Panemux chose an on-demand local listener backed by SSH `direct-tcpip`
instead of exposing a general proxy or requiring users to preconfigure `ssh -L`. The scope remains
loopback-only and the listener exists only for the callback flow.

Current contracts: [architecture guide](../../architecture.md).
