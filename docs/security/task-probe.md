---
type: Security Requirement
title: "Security: task agent probe"
description: "Fixed CLI availability probes and bounded host diagnostics."
---

# Security: task agent probe

[Common execution requirements](command-execution.md) apply at this boundary.

The agent probe ([behavior](../behavior/task-collection.md#agent-availability)) runs one more fixed script per
host, `probeScript` in `internal/tasks/availability.go`, apart from the collection script. It is
composed once at startup from Go constants alone: a fixed `probe_cli` function and one
`probe_cli <id>` line for each registry ID that declares `launch` or `resume`. Composition accepts an
ID only as a `[a-z0-9-]` word (the registry's own rule is narrower still), so the ID can neither end
the word nor add a command. Nothing from a request, the config or a host reaches the script, and it
runs exactly as the collection does: locally `exec.CommandContext(ctx, "sh", "-s")` with the script
on stdin and `HOME` from `internal/homedir`, remotely the literal `sh -s` over the host's collection
connection with the script on stdin, under a 5-second context of its own that the service's close
ends and the request's does not.

The lookup is the one [Task launch and resume](task-launch.md) makes, with the same
safety: `command -v "$1"`, and when that finds nothing `"${SHELL:-/bin/sh}" -lc "command -v $1"`
with `/dev/null` on stdin. `$1` is the fixed ID above, and `$SHELL` is the host user's own login
shell run with a fixed argument; it chooses where the CLI is looked up, not what is run. What
either prints is believed only when it is an absolute path to an executable regular file, and the
probe never runs that file: it prints only `cli <id> found` or `cli <id> missing`. The parser
(`parseProbeOutput`) skips text before its `::panemux-probe v1` header (a login shell's greeting),
accepts only those two answers for an ID it asked about, once each, and fails the probe on
anything else, on a missing answer and on output without `::end`. A probe that fails changes
nothing about the host's collection: it runs beside it, never dials, keepalives or drops the
connection, and is not waited for. Its error reaches only the availability detail — which
`GET /api/tasks` carries, to be shown as text only, never as markup or a link — and the server
log (when it changes), the detail made valid UTF-8 with control characters replaced and bounded
(`boundedText`).

What the collection reads in a format this build does not know is reported the same way: the
examples and versions in an availability detail are host text, each bounded to 120 and 32
characters by `boundedText`, at most 3 of each, and never reach a command.
