---
type: Security Requirement
title: "Security: shell paths"
description: "Trusted shell selection and validation before local process execution."
---

# Security: shell paths

See the [General Rules](../security.md#general-rules) and [boundary map](command-execution.md).

`validateShell` in `internal/session/local.go` applies three layers:

1. **Absolute-path check**: reject relative paths outright.
2. **Regex character allowlist**: `^(/[a-zA-Z0-9._\\-/]+)$` rejects shell metacharacters such as spaces, semicolons, and quotes.
3. **`/etc/shells` allowlist**: iterate the system shell registry and return the key from the trusted map (`s`), not the caller-supplied value.

Return the trusted registry key, not a regex submatch derived from the caller. CodeQL's
`go/command-injection` analysis follows that caller-derived value through sanitization, while the
registry key originates in file I/O and equality comparison does not propagate taint. Environment
variables are taint sources too: the default must remain the literal `"/bin/sh"`, never
`os.Getenv("SHELL")`.
