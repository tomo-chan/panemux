---
type: Security Requirement
title: "Security: agmsg acquisition and installation"
description: "Pinned upstream source verification and fixed-script agmsg installation."
---

# Security: agmsg acquisition and installation

[Common execution requirements](command-execution.md) apply at this boundary.

`internal/agmsg.Source` executes the literal `git` through the local input execution port with
separate argv: SHA-scoped cache path, fixed repository URL, fixed full commit and fixed git
subcommands. No browser input selects any operand. The local split-output runner returns only stdout for
SHA comparison and tar transfer; successful stderr warnings are ignored as data. On failure,
trimmed stderr and the exit error are retained in git operation diagnostics (init, fetch,
verification and archive). Target prerequisite
checks use the combined input lane so local and SSH failures preserve missing-tool names. It verifies `rev-parse <SHA>^{commit}` against
the complete pin before archiving. Git object hashes protect the fetched content; cache ownership
is trusted. A verified cache needs Git but no network. No checkout hooks run during acquisition.

`internal/agmsg.Tool` passes git archive's tar bytes as stdin to a fixed target script through
`host.InputExecutor`. The only operand is either empty or the literal `--update`; the script
allowlists both, checks alternate `.agmsg` markers again, creates a mktemp directory, extracts the
archive there, and runs the literal `bash ./install.sh` with stdin closed. The trap removes only
that invocation's temporary directory. No shell program is read from transferred stdin. The tar
and installer are trusted upstream code pinned in the application; this is not an untrusted
archive extractor. Output and exit/timeout errors are retained. Process-group cancellation and
SSH channel cancellation use the shared input execution lane. Agent-owned processes are outside its pause.
