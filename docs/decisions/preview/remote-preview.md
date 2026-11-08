---
type: Decision
title: "2026-10-16: Previewing an SSH host's files and localhost URLs (#387)"
description: "Rationale for 2026-10-16: Previewing an SSH host's files and localhost URLs."
---

# 2026-10-16: Previewing an SSH host's files and localhost URLs (#387)

SFTP avoids shell-interpreted file paths; cat/exec fallback would require narrower names and another
injection boundary. Component-wise symlink resolution is needed because REALPATH is not reliably
recursive; the check/open race remains explicitly accepted.
Preview owns a connection using the pane's original dial config, so tabs outlive panes and config
edits cannot redirect old panes to new hosts. Dial outside locks to preserve shutdown. Transport
traffic, not a fixed reply deadline, keeps slow busy relays alive; failed connections can recover.
Distinct chosen relay ports avoid OAuth forwarding collisions/expiry. TLS retains localhost for
certificate identity; separate-origin relay pages keep module support.
See [preview security](../../security/preview.md) for authoritative limits and trust boundaries.
