---
type: Security Requirement
title: "Security: operator browser launch"
description: "Fixed platform browser programs and discrete URL arguments for --open."
---

# Security: operator browser launch

See the [shared rules and boundary map](command-execution.md).

`openChrome` in `main.go` opens panemux's own listen address for `--open`.
`browserOpenArgv(goos, url)` is the only source of its program and argv: it returns only the
compile-time literals `open`, `google-chrome` or `cmd`, or reports an unsupported OS and launches
nothing. The URL is `"http://" + srv.Addr()`, and every value remains a discrete argv element;
no shell parses it and the URL is never spliced into another argument.

The variable first argument at this exec sink is safe only because of that literal-only source.
Its inline `//nolint:gosec` reason must explain this provenance, which is not visible at the call
site. This is the justification required by the [general rules](../security.md#general-rules).
