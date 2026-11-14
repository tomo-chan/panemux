---
type: Decision
title: "Command center pins its own execution context (2026-08-22, PR #171)"
description: "Isolate command-center sessions, settings, directories and permissions from the ambient interactive CLI."
---

# Command center pins its own execution context (2026-08-22, PR #171)

Live CLI verification showed that plain `claude -p` can report the ambient interactive session ID,
and inherited settings can broaden an `--allowedTools` policy. The command center now mints and
persists its own session ID, disables ambient setting sources, uses an empty per-query directory,
passes its instructions explicitly, restricts MCP configuration, and combines an allowlist with an
explicit acting-tool denylist. The current security contract and evidence are in
[security/command-center.md](../../security/command-center.md#session-settings-and-working-directory).
