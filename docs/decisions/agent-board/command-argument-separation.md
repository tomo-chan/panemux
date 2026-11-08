---
type: Decision
title: "Command-center subprocess arguments are structurally separated (2026-08-14, PR #170)"
description: "Rationale for Command-center subprocess arguments are structurally separated."
---

# Command-center subprocess arguments are structurally separated (2026-08-14, PR #170)

Adversarial testing against the real `claude` CLI found two independent argv defects: a
dash-prefixed prompt could be parsed as a flag, while ordinary prompts failed because of variadic
flag placement. The runner now constructs a fixed option segment, uses the CLI's option terminator,
and keeps the prompt in the operand position. Exact argv-shape tests are intentionally retained as
a security contract. See [security/command-center.md](../../security/command-center.md).
