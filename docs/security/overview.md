---
type: Security Requirement
title: "Security overview and invariants"
description: "Implementation security invariants, development trust boundaries, gosec policy, and routes to per-sink requirements."
---

# Security overview and invariants

Required reading before changing command execution, shell argument handling, SSH path handling,
host key handling, authentication, preview isolation, or `gosec`-sensitive code. The General Rules,
`gosec` policy, and standing exception below apply to every change.

## Boundary map

| Boundary | Document |
|---|---|
| Bearer auth, loopback bootstrap, MCP token file, shared browser request guard | [auth.md](auth.md) |
| Shell paths, tmux targets/attaches, SSH paths/keys/`ProxyCommand`, host execution, `--open` | [process-and-ssh.md](process-and-ssh.md) |
| Task collection, availability probe, launch/resume, Claude/Codex summaries, Codex permission hook | [tasks.md](tasks.md) |
| Agent Board trust/relay/remote writes, dashboard values, pinned agmsg install, command center subprocess | [agent-board.md](agent-board.md) |
| Split preview (local/SSH, localhost, Mermaid, relays), pane URL-open, loopback forwarding, browser shim | [preview-and-urls.md](preview-and-urls.md) |

## General rules

- When adding session types or `exec.Command` calls, the executable passed as the first argument
  must come from a hardcoded literal or a trusted system source (such as `/etc/shells` or an
  internal registry) with no data-flow path to user input.
- Arguments after the executable may carry user-supplied values only when the target binary cannot
  reinterpret them as commands or options. User-configurable values (including shell paths, tmux
  names, and remote paths) must be validated before any session `exec.Command` or remote shell sink.
- Do not pass `os.Getenv` values into flows that reach `exec.Command` unless the security model for
  that path is explicitly designed and documented.

## Development sandbox permissions

Codex verification uses fixed targets and checkout-local expendable caches/logs; it does not
install permission rules, alter managed policy, or authorize external operations
([Codex verification](../engineering/codex-verification.md)).

Shared `.claude/settings.json` grants macOS sandboxed commands local port binding and access to all
localhost services, including services outside the sandbox. Treat unauthenticated local services as
a potential route outside the sandbox and non-loopback listeners as externally reachable. This is a
development-tool permission; it does not authenticate panemux routes or change runtime trust
boundaries ([Claude Code sandbox](../engineering/runtime-and-sandbox.md#claude-code-sandbox)).

Do not add `sandbox.allowPty`, `sandbox.network.allowUnixSockets`,
`sandbox.network.allowAllUnixSockets`, or `sandbox.filesystem.allowGitConfig` to shared settings:

- `allowGitConfig` lets a sandboxed command rewrite `.git/config`, whose `core.hooksPath` decides
  what runs outside the sandbox on the next `git push`.
- `allowAllUnixSockets` exposes unrelated local sockets (Docker, `ssh-agent`) to sandboxed commands.
- `allowUnixSockets` requires per-user absolute paths (`/tmp/claude-<uid>`) unsuitable for shared config.

Tests needing denied capabilities probe via `internal/testcap`, skip locally, and fail in CI
([D13](../decisions/quality-gateway.md#d13-capability-probes-skip-locally-and-fail-in-ci)). Tests
must never write to developer config or cache directories; substitute `internal/homedir` and
`internal/cachedir` ([testability seams](../engineering/testing.md#injectable-filesystem-seams)).

## Development runtime bootstrap

The runtime selector reads exact pins from `go.mod` and `.node-version`. Node bootstrap downloads
only an allowlisted platform/architecture archive from the official HTTPS release origin, verifies
its official SHA256 checksum before extraction, checks startup version and bundled npm, and
publishes atomically under an exclusive lock. Cached and preinstalled SDKs must still start with
the exact version; a bad cache fails closed rather than silently falling back. Exclusive locking
requires a conforming `mkdir` (Ubuntu 26.04 uutils 0.10.0 is unsuitable for concurrent acquisition;
see [Runtime toolchain contract](../engineering/runtime-and-sandbox.md#runtime-toolchain-contract)).

## agmsg release proposal CI

Upstream installers and agmsg scripts run in jobs without GitHub App secrets. Publication uses a
fresh runner and trusted checkout, accepting only a bounded data-only JSON artifact (fixed capture
filenames, JSON objects, and version/SHA provenance validated against independently resolved scope
outputs; rejecting code, paths, symlinks, extra files, and oversized content). Trusted code applies
constants and captures, runs full package tests, and only then mints the repository-scoped App
token ([agmsg maintenance](../engineering/agmsg-maintenance.md#github-app-setup-repository-owner)).

## `gosec` policy

- Fix `gosec` findings structurally in shipped code rather than adding suppressions.
- Test-only code may use narrow `//nolint:gosec` annotations when a fixture intentionally violates a
  production hardening rule.
- Any rare production suppression must explain the safety provenance inline at the sink.

## Exception: OpenSSH hashed `known_hosts` compatibility

`internal/session/ssh.go` keeps a narrow `//nolint:gosec` on `crypto/sha1` (`G505`) solely to
recognize OpenSSH hashed `known_hosts` patterns (`|1|<salt>|<hash>`), whose format requires
HMAC-SHA1 (`ssh-keygen -H`), so `HostKeyAlgorithms` can be narrowed to the host's recorded key
types. This is file-format compatibility, not application hashing or credential storage. Host-key
verification still runs through `knownhosts.New(...)`.
