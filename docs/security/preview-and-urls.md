---
type: Security Requirement
title: "Split preview and URL-open security"
description: "Local and SSH split preview isolation, Markdown/Mermaid rendering, loopback relays, OAuth callback forwarding, and browser-open interception."
---

# Split preview and URL-open security

> Part of [Security overview and invariants](overview.md).

## Previewing local files

The [split preview](../backend/workspace/preview-and-urls.md#split-preview) renders local HTML and Markdown as untrusted content
(including agent-written scripts).

### Isolation from the dashboard's origin

Every `/preview/<token>/…` response omits `X-Frame-Options: DENY` (replacing it with
`frame-ancestors 'self'`), sets `Cache-Control: no-store`, and carries a CSP `sandbox` **without
`allow-same-origin`**:

| Response | Policy |
|---|---|
| HTML file | `sandbox allow-scripts; frame-ancestors 'self'` |
| Markdown without Mermaid | `sandbox; default-src 'none'; img-src * data:; style-src 'unsafe-inline'; frame-ancestors 'self'` |
| Other subresource beside it | `sandbox; frame-ancestors 'self'` |

Running in an opaque origin prevents DOM/storage/cookie access, forces requests to `/api/*` and
`/ws/*` to arrive cross-site (`Origin: null` / `Sec-Fetch-Site: cross-site`) where the
[shared browser request guard](auth.md#shared-browser-request-guard) refuses them
(`TestServer_PreviewIsIsolatedFromTheDashboardOrigin`), sends no CORS header, and prevents top
navigation or popups. `PreviewSplit` repeats `sandbox="allow-scripts"` on the iframe. **Never add
`allow-same-origin` to `/preview/` responses or their iframe.**

### Localhost pages

Localhost URLs on the panemux host are framed directly (never fetched by panemux) inside a
script-free `srcdoc` wrapper (`localhostFrameDocument`) whose CSP is
`default-src 'none'; style-src 'unsafe-inline'; frame-src ...` restricting `frame-src` to
`http`/`https` on `localhost`, `127.0.0.1`, and the URL's IPv4 loopback address (blocking `302` or
script redirects to external sites). Because the outer frame carries `allow-same-origin`, the
`srcdoc` wrapper itself runs in the **dashboard's own origin**: the URL enters only as an escaped
HTML attribute value (`escapeAttribute`), and `default-src 'none'` forbids any script in the wrapper
document. Because CSP cannot name `[::1]`, IPv6 loopback URLs are framed as `localhost` (sharing
`localhost` cookies/storage; Open in new tab uses `[::1]`).

Both frames use `sandbox="allow-scripts allow-same-origin allow-forms allow-modals allow-popups"`
(never `allow-top-navigation` or `allow-popups-to-escape-sandbox`, so the framed page cannot
navigate the dashboard and windows it opens stay sandboxed), keeping the dev server in **its own**
loopback origin (never the dashboard's, which denies framing via `X-Frame-Options: DENY` and refuses
cross-port `/api/*` and `/ws/*` via the request guard). Only loopback `http`/`https` URLs on the
panemux host are framed, after operator approval for terminal-emitted requests.

### Markdown and Mermaid rendering

- **Markdown**: Rendered server-side by goldmark defaults (raw HTML omitted; never enable
  `WithUnsafe`). An AST transformer (`dangerousLinks`) rechecks every link/image destination after
  decoding character references and stripping control/whitespace characters with `IsDangerousURL`.
- **Mermaid**: Markdown containing a Mermaid block uses a per-response `crypto/rand` nonce CSP:
  `sandbox allow-scripts; default-src 'none'; script-src 'nonce-<random>'; script-src-attr 'none';
  connect-src 'none'; object-src 'none'; frame-src 'none'; worker-src 'none'; base-uri 'none';
  form-action 'none'; img-src * data:; style-src 'unsafe-inline'; frame-ancestors 'self'`.
  The bundled classic IIFE script is served by content hash under `/preview-assets/` (404 for
  unknown names, no filesystem lookup or SPA fallback, no `unsafe-eval`/`allow-same-origin`).
  Goldmark emits escaped code; the renderer reads `textContent`, rejects all diagram frontmatter
  and directives, uses strict security with HTML labels off, never invokes `bindFunctions`, and
  sanitizes final SVG with DOMPurify (forbidding executable/HTML/image/link elements, nonlocal
  `href` references, and unsafe CSS). **Network caveat**: Mermaid measures unsanitized SVG in the
  preview DOM before final sanitization, so image shapes can issue external fetches under
  `img-src * data:` that final sanitization cannot retract; preview is not a network-isolated
  viewer. Limits: 10 MiB document, 10 diagrams, 20,000 UTF-16 code units/diagram, 50,000/document,
  100 edges (rejected source also consumes the document text budget; limits are not a preemptive CPU
  deadline).

### Paths, symbolic links, tokens, and size

- Only HTML/Markdown files inside the server-determined session working directory and agent
  worktrees resolve (`403` for escapes; unreadable files report not found). Deepest existing parent
  directories are matched by `os.SameFile` through an `/` `os.Root` (supporting symlinked roots
  and case-insensitive filesystems without expanding scope), and all file opens go strictly through
  the root's `os.Root` via `filepath.Rel` (rejecting `..`, escaping or absolute symlinks, invalid
  `fs.ValidPath`, backslashes, and non-regular files; directories are never listed).
- Tokens are 128-bit `crypto/rand` values issued only by guarded `POST /api/sessions/{id}/preview`
  and kept for server lifetime (`/preview/` itself is intentionally unguarded for opaque-origin
  subresources).
- HTML and Markdown are capped at `preview.MaxDocumentBytes` (10 MiB; HTML served via
  `io.SectionReader` to prevent concurrent growth leaks).

## Previewing files of an SSH host

- **Connection**: `session.DialPreviewConn` dials a dedicated per-`SSHConfig` connection using the
  pane's original `session.PreviewTarget` (never re-reading edited `ssh_connections`, never sharing
  the pane's session). **No shell command runs over it**—files use SFTP only (no `scp`/`cat`
  fallback) and relays use `direct-tcpip`. Keepalive (`keepalive@openssh.com`) uses transport
  liveness (`LastRead`, 15 s) so busy relays are not killed; SSH closes before SFTP on shutdown.
- **Remote paths and symlinks**: `realPath` resolves component-by-component with SFTP `LSTAT`/
  `READLINK` (never SFTP `REALPATH`, which servers may clean lexically; max 40 links). Every
  request re-resolves against resolved working directories, checks `fs.ValidPath` and backslashes,
  and applies the same 10 MiB limit and policies (an accepted check-to-`OPEN` race exists against a
  host user who can already write inside the working directory).
- **Relays and origin**: Loopback relays bind `127.0.0.1:0` (never request-derived), accept only
  `http`/`https` loopback targets (`ParseLoopbackURL`), pass TLS through untouched (`https://localhost:<port>`),
  are keyed per `(Remote, host, port)`, and live until server shutdown. Any local process on the
  panemux host can reach the relayed port. Relayed pages run in `http://127.0.0.1:<relay port>`,
  framed in the same `srcdoc` localhost wrapper; the request guard blocks their cross-port calls to
  `/api/*` and `/ws/*`. Because browsers scope cookies by host name, relayed pages share
  `127.0.0.1`'s cookies with each other; panemux itself sets none.

## Opening URLs from a pane

### Loopback port forwarding

- **Loopback & unprivileged ports only**: Binds constant `127.0.0.1` and rejects ports `< 1024`
  (`minForwardablePort`; implicit 80/443 never bind).
- **Bounded**: Max 8 forwards per pane, 32 total, 30-minute idle TTL, closed on pane
  delete/restart, host terminal close/replace/expiry, or server shutdown (rechecked after `Ensure`
  against concurrent removal).
- **No command execution**: Uses `(*ssh.Client).DialContext` directly with no shell. Any local
  process on the panemux host can reach the forwarded port while open. `POST /api/sessions/{id}/open-url`
  follows the core unauthenticated route posture ([Authentication](auth.md#route-ownership-and-bearer-authentication)).

### Browser-open interception

The shim (`internal/session/browseropen.go`) is a fixed string literal (`browserShimScript`,
best-effort, disabled by `url_open.browser_shim: false`). Locally it is written with `os.WriteFile`
under `os.UserCacheDir()` (via `internal/cachedir`, mode `0700`; no shell parses it). Remotely it is
written via `shellQuotePath` into `"$HOME/.cache/panemux/bin"` (mode `0700`), resolved by the remote
shell itself so no panemux-side path value is interpolated. It only emits an OSC escape
sequence to its invoking terminal—compatible with `internal/agmsg`'s remote-write rule—and falls
through via `PANEMUX_SHIM_FALLBACK_PATH` while refusing to exec targets inside its own directory.

Because terminal output is untrusted, two mandatory guards apply to OSC and OSC 8 URLs:

1. **Scheme allowlist**: `http`/`https` only (`parseBrowserOpenOsc` and `portforward.ValidateOpenURL`).
2. **Explicit approval**: Never auto-open; require an operator click on `Open` (or `Preview` for
   localhost). Each approval consumes the pending request once, and replayed scrollback never
   triggers strips.

### New `ssh_tmux` sessions

On tmux 3.3+, `new-session -e` passes shim environment operands only to new sessions (`-A` on
existing sessions ignores them), wrapped in a one-line `/bin/sh` script. A fixed initial-pane
bootstrap sets `PATH` after tmux's client override and reads the default command literally via
`display-message` into `$SHELL -c` (no eval or format expansion). Only opted-in panes set
pane-scoped `allow-passthrough` (`set-option -p -t "$TMUX_PANE"`); note this enables **all**
passthrough sequences from that pane, so scheme allowlist, approval, and replay suppression remain
the boundary. Local PTYs and ordinary SSH panes clear inherited opt-in markers.
