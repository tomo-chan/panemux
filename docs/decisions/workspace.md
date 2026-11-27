---
type: Decision
title: "Workspace and runtime decisions"
description: "Design rationale for browser authority boundaries, layout normalization, stream framing, remote shims, and split preview isolation."
---

# Workspace and runtime decisions

Current contracts live in [Behavior specification](../behavior.md) and
[Preview security](../security/preview-and-urls.md).

## One browser authority boundary for APIs and WebSockets (2026-10-11, issue #360)

Isolated preview pages can run on a different loopback port, so permitting every localhost port
through CORS would let them reach the terminal server, and CORS alone cannot stop simple requests
from executing handlers. All API and WebSocket routes therefore apply the shared browser request
guard before routing and preflight responses, and API mounts enforce it independently before
handlers. Development uses Vite's same-origin proxy rather than a permanent 5173 allowlist.
Origin-free CLI requests remain supported; browser evidence without `Origin` requires a matching
`Referer` and consistent Fetch Metadata, failing closed when evidence is missing.

## WebSocket route separation and Origin validation (2026-10-08, issues #297, #298)

The command stream moved from `/ws/board-command` to `/ws/board/command` so the valid
`board-command` pane keeps its single-segment terminal URL; fixed streams use two segments. All
WebSockets and guarded APIs share hostname/effective-port `Origin` validation and reject
same-site/cross-site Fetch Metadata (Fetch Metadata alone was rejected because Chrome WebSocket
upgrades can omit it). Scheme matching against backend TLS was rejected because deployments may
terminate TLS upstream; forwarded headers are not trusted and same explicit host/port across
schemes remains an accepted limitation.

## Layout responses are normalized before frontend parsing (2026-09-03, PR #198)

Valid hand-written config can omit `direction`/`children` or place a lone pane at the layout root,
while the browser contract requires a normalized recursive node. The API fills missing keys,
relocates a lone root pane into its implied child, and validates it like every other pane. The
normalized form is persisted only on the next explicit save.

## Command-center prompts use a panemux-owned history entry (2026-08-17)

The Claude stream emits `stream_event`, `system`, `assistant`, and `result` frames but does not
repeat the prompt that began each turn. Panemux writes the prompt first as `panemux_prompt` and
then appends the captured subprocess frames. A subprocess failure still records the prompt; a
request that never starts a subprocess does not create a turn.

## Command-center terminal frames carry non-fatal warnings (2026-09-11–2026-09-13, PRs #228, #234)

Representing a failed history write as `error` followed by `done` violated the one-terminal-frame
rule, and limiting warnings to successful turns hid them when a query also failed. `warnings` now
rides whichever single terminal frame ends the turn. Malformed stream output cancels the subprocess
before draining remaining output so draining cannot hold the busy flag until timeout.

## Browser interception in new remote tmux sessions (2026-10-09, issue #339)

Support is scoped to new `ssh_tmux` sessions created by panemux because existing shells cannot
inherit environment changes retroactively. Session-local `new-session -e` sets the browser and
opt-in marker without polluting the global tmux environment. Because tmux overwrites the initial
pane's `PATH` from the attaching client, a fixed bootstrap prepends the shim after that override
and then invokes the configured command or login shell. The shim enables pane-local passthrough
(requiring tmux 3.3+) and browser approval remains mandatory.

## Split preview file isolation and bounds (2026-10-15, issue #385)

Opaque-origin CSP on the existing server was chosen over running a second listener. Frame sandbox
repeats the boundary; token URLs let sandboxed resources load without passing the API guard, and
tabs outlive their source panes. Arbitrary absolute-file previews were rejected after CodeQL
findings: lookup and serving both use `os.Root` within session/worktree roots, using directory
identity rather than string comparison so logical `/tmp` and resolved `/private/tmp` match without
expanding scope.

## Localhost pages in the split preview and shared URL opener (2026-10-16, issue #386)

Direct framing preserves local development servers' absolute asset paths and hot reload while the
dashboard API guard denies cross-port calls. A `srcdoc` wrapper CSP constrains redirect
destinations; IPv6 loopback is rewritten to `localhost` because CSP cannot name `[::1]` (accepting
localhost cookie/storage sharing rather than dropping IPv6). Cross-origin failures are unobservable
so limits are documented rather than server-probed. The shared URL opener routes output-chosen OSC 8
links through operator approval.

## Previewing SSH host files and localhost URLs (2026-10-16, issue #387)

Remote file reads use SFTP rather than shell-executed `cat`, avoiding a shell-injection boundary;
component-wise symlink resolution compensates for non-recursive `REALPATH` (with the check/open race
explicitly accepted). Preview owns a per-host SSH connection using the pane's original dial config
so tabs outlive panes and config edits cannot redirect old panes to new hosts. Dedicated relay ports
avoid OAuth forwarding collisions, and transport traffic rather than a fixed deadline keeps busy
relays alive.

## Mermaid inside isolated Markdown previews (2026-11-01–2026-11-02, issues #471, #477)

Markdown keeps goldmark's GFM renderer and enables a separately bundled IIFE Mermaid script only
when an AST check finds a Mermaid block, authorized by a per-response nonce. Bundling as one classic
script avoids ESM CORS or `allow-same-origin` in opaque-origin previews and avoids shipping a
server-side browser/Node runtime. Diagram directives and frontmatter are refused, per-block errors
preserve source, and input limits (10 diagrams, 50,000 UTF-16 code units, 20,000 per diagram, 100
edges) bound work without claiming a preemptive CPU timeout.

Because Mermaid requires a live DOM to measure its SVG before sanitization, the Markdown image
policy applies during measurement and final SVG sanitization cannot retract already-issued image
requests; diagram rendering is therefore not claimed to be network-isolated.
