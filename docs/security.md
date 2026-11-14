---
type: Security Requirement
title: "Security Design"
description: "Implementation security invariants, development trust boundaries, and routes to sink requirements."
---

# Security Design

This document defines the implementation-time security requirements for panemux. Treat it as required reading before changing command execution, shell argument handling, SSH path handling, host key handling, or `gosec`-sensitive code.

The General Rules, the `gosec` policy and the one standing exception below apply to every change.

## Document map

The per-sink requirements live in [`docs/security/`](security/), grouped by area. Each is required
reading before touching the code it covers.

| Sections | Document |
|---|---|
| Command execution sinks | [command-execution.md](security/command-execution.md) |
| Shell paths | [shell-path.md](security/shell-path.md) |
| Tmux sessions | [tmux.md](security/tmux.md) |
| SSH execution | [ssh-execution.md](security/ssh-execution.md) |
| Operator browser launch | [browser-open.md](security/browser-open.md) |
| Host execution | [host-execution.md](security/host-execution.md) |
| Agmsg acquisition and installation | [agmsg-install.md](security/agmsg-install.md) |
| Task collection | [task-collection.md](security/task-collection.md) |
| Codex permission hook | [codex-hook.md](security/codex-hook.md) |
| Task agent probe | [task-probe.md](security/task-probe.md) |
| Task launch and resume | [task-launch.md](security/task-launch.md) |
| Task summaries | [task-summaries.md](security/task-summaries.md) |
| Agent board remote writes; Agent-reported values in the dashboard UI | [agent-board.md](security/agent-board.md) |
| Command center subprocess execution | [command-center.md](security/command-center.md) |
| Auth token and transport encryption | [auth.md](security/auth.md) |
| Opening URLs From a Pane; Loopback port forwarding; Browser-open interception | [url-open.md](security/url-open.md) |
| Previewing Local Files; isolation, localhost pages, paths and symbolic links, tokens, size; Previewing Files of an SSH Host; the connection, remote paths and symbolic links, relays, the relayed page's origin | [preview.md](security/preview.md) |

## Session Command Execution

User-configurable values, including shell paths and tmux session names, must be sanitized before any session's local `exec.Command` sink.
Follow the [execution-boundary map](security/command-execution.md), [Agent Board](security/agent-board.md), [command center](security/command-center.md) and [URL-opening](security/url-open.md) requirements for their respective paths.

## General Rules

- When adding new session types or new `exec.Command` calls, the command value passed as the first argument must come from a hardcoded literal or from a trusted system source such as a file or registry with no data-flow path to user input.
- Arguments after the command may be user-supplied only when the target binary cannot reinterpret them as commands.
- Do not use `os.Getenv` values in flows that reach `exec.Command` unless the security model for that path is explicitly reworked and documented.

## Development sandbox permissions

Codex verification uses fixed targets and checkout-local expendable caches/logs;
it does not install permission rules, alter managed policy, or authorize external
operations. User approval of trusted verification code retains the scope and
limitations in [Codex verification](development/codex-verification.md).

The shared Claude Code settings grant macOS sandboxed commands local port binding and access to
all localhost services, including services outside the sandbox. Treat unauthenticated local
services as a potential route to actions outside the sandbox, and non-loopback listeners as
reachable from other machines. This is a development-tool permission; it does not authenticate
panemux routes or change their runtime trust boundaries. Platform differences and managed-policy
limits are documented in [Claude Code sandbox](development/claude-sandbox.md).

That is the only permission they grant. Do not add `sandbox.allowPty`,
`sandbox.network.allowUnixSockets`, `sandbox.network.allowAllUnixSockets` or
`sandbox.filesystem.allowGitConfig` to the shared settings to make a test pass:

- `allowGitConfig` lets a sandboxed command rewrite `.git/config`, and `core.hooksPath` there decides
  what runs, outside the sandbox, on the next `git push`.
- `allowAllUnixSockets` opens every local socket — docker's, ssh-agent's — to every sandboxed command.
- `allowUnixSockets` takes absolute paths, and the directory tmux would need is per user
  (`/tmp/claude-<uid>`), so a shared file cannot name it.

Tests that need these capabilities skip inside the sandbox and are verified in CI instead
(decision D13 in [Decision rationale](decisions/quality-gateway/capability-probes.md)).

Tests must not write to the developer's own configuration or cache directories. The sandbox makes a
violation fail loudly, but the rule holds outside it too: substitute `internal/cachedir` and
`internal/homedir` as [DEVELOPMENT.md's testability rule](../DEVELOPMENT.md#test-granularity) says.

## Development runtime bootstrap

The development runtime selector reads exact versions from tracked repository pins. Node
bootstrap downloads only an allowlisted platform/architecture archive from the official HTTPS
release origin, verifies its official SHA256 checksum before extraction, checks its startup
version and bundled npm, and publishes the SDK atomically under an install lock. Cached and
preinstalled local SDKs are trusted developer tools; they must still start with the exact version
and provide npm/npx. A bad published cache is an error, not a reason to silently use another
runtime. This is development tooling, with no product/user-input route into its commands.
Exclusive locking assumes a conforming `mkdir`; affected uutils 0.10.0 is unsuitable
for concurrent acquisition. The platform limitation and follow-up are in the runtime contract.
See [the runtime contract](development/runtime.md).

## agmsg release proposal CI

Upstream installers and agmsg scripts run in jobs without App secrets. Publication uses a fresh
runner and a trusted checkout, never that job's workspace, processes or hooks. Its only upstream
input is a bounded data-only JSON artifact: fixed capture filenames, JSON objects and version/SHA
provenance validated against independently resolved scope outputs. Reject code, paths, symlinks,
extra files and oversized content. Trusted code applies constants and fixture data, then runs
the package tests before minting the repository-scoped App token. See
[maintenance](maintenance/agmsg-contract.md#github-app-setup-repository-owner) for permissions and setup.

## `gosec` Policy

- Fix `gosec` findings structurally in the implementation rather than suppressing them in shipped code.
- Test-only code may use narrow `//nolint:gosec` annotations when the fixture behavior intentionally violates a production hardening rule.
- Production paths should avoid suppressions and make the safety argument explicit in code structure instead.

## Exception: OpenSSH Hashed `known_hosts` Compatibility

`internal/session/ssh.go` keeps a narrow `//nolint:gosec` on `crypto/sha1` (`G505`) solely for OpenSSH hashed `known_hosts` patterns (`|1|<salt>|<hash>`), whose format requires HMAC-SHA1. This is protocol/file-format compatibility, not application-defined hashing or credential storage.

To narrow `HostKeyAlgorithms` to the host's recorded key types, the scan must recognize both plaintext and hashed entries, including those produced by `ssh-keygen -H`. A different hash would break matching and SSH connections.

Host-key verification still uses `knownhosts.New(...)`; this exception permits only recognition of existing hashed host patterns.

## Related Documents

- Implementation structure: [architecture.md](architecture.md)
- Agent Board design: [agent-board.md](agent-board.md)
- Runtime behavior and SSH configuration rules: [behavior.md](behavior.md)
- Developer workflow rules: [../DEVELOPMENT.md](../DEVELOPMENT.md)
