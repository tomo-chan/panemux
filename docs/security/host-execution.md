---
type: Security Requirement
title: "Security: host execution"
description: "Trusted scripts, separate stdin and output capture contracts for host operations."
---

# Security: host execution

See the [shared rules and boundary map](command-execution.md).

`host.InputExecutor`, implemented by `hostexec.Pool.RunInput`, accepts trusted application script
text, positional operands and an arbitrary input stream separately. It is an internal port, with
no HTTP route supplying script text. Locally the executable is the literal `sh`, with `-c`, the
trusted script and a fixed `$0` in argv. Remotely the login shell parses only a fixed one-line
bootstrap and single-quoted octal operands: no raw newline, quote or `!` reaches that parser.
This avoids csh/tcsh history expansion and fish's different quote/backslash rules. POSIX `sh`
decodes the script and operands using its builtin `printf`, preserving empty operands and trailing
newlines, then executes the trusted script with the decoded operands as positional arguments.
Variable values must never be interpolated into script text. The input stream is data, not a shell program: the shell reads
its commands from `-c`, leaving stdin available for an archive consumer such as `tar`.

Local execution uses `homedir` for HOME and the existing process-group cancellation and wait
limit. SSH execution uses the shared connection, cancellation and liveness policy. Output from
this lane includes stderr as well as stdout and is preserved on nonzero exits; the wrapped exit
error or timeout is preserved too, including bytes captured before an SSH cancellation.
The remote wrapper's `exec 2>&1` is trusted fixed text.

`hostexec.RunLocalSplit` uses the same local setup, HOME seam, cancellation and wait limit but
separates stdout/stderr for binary or structured consumers: success warnings must not become data.
`RunInput` keeps combined output on local and SSH hosts. HTTP input selects neither script nor capture mode.

This transport does not fetch, verify, extract or run an installer itself. Adapters own those
policies, fixed scripts and cleanup. Tool management drains admitted calls before running optional
adapter prerequisites and installation, rejects new calls while paused and resumes after errors
or cancellation. A consumer must hold its admission lease for its entire operation; an updating
snapshot alone cannot prevent a race. Agent-owned scripts are outside this coordination.
