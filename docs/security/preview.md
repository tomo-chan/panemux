# Security: the split preview

> Part of the [security design](../security.md). That document carries the general rules, the `gosec` policy, and the map of these files.

## Previewing Local Files

The split preview ([behavior](../behavior/preview.md)) shows HTML and Markdown from the panemux
host's filesystem in the dashboard. An HTML mock is untrusted content: an agent wrote it, and its
scripts run. Read this section before changing `internal/preview`, `PostSessionPreview` /
`ServePreview` in `internal/api/preview.go`, `PreviewSplit`, or `usePreview`.

### Isolation from the dashboard's origin

Previews are served by the dashboard's own server under `/preview/<token>/…`, and every preview
response carries a `Content-Security-Policy` `sandbox` directive **without `allow-same-origin`**:

| Response | Policy |
|---|---|
| HTML file | `sandbox allow-scripts; frame-ancestors 'self'` |
| Markdown without Mermaid, rendered | `sandbox; default-src 'none'; img-src * data:; style-src 'unsafe-inline'; frame-ancestors 'self'` |
| Anything else beside it | `sandbox; frame-ancestors 'self'` |

A sandboxed document without `allow-same-origin` runs in an opaque origin, whether it is framed by the
dashboard or opened in a tab of its own. That is the isolation:

- it cannot read the dashboard's DOM, storage or cookies, nor the dashboard read it;
- every request it makes to `/api/*` or `/ws/*` arrives as cross-site — `Origin: null` or
  `Sec-Fetch-Site: cross-site` — and the [shared browser request guard](auth.md#shared-browser-request-guard)
  refuses it before any handler runs. `TestServer_PreviewIsIsolatedFromTheDashboardOrigin` pins this
  through the shipped router, and the WebSocket and session-token routes with it;
- no `Access-Control-Allow-Origin` is sent, so a script in it cannot read even a refused answer;
- without `allow-top-navigation` or `allow-popups` it cannot navigate the dashboard or open windows.

The frame in `PreviewSplit` repeats `sandbox="allow-scripts"`, so the page stays out of the
dashboard's origin even if the header were lost on the way. A Markdown page without Mermaid runs
no script at all, by its policy. A page with Mermaid permits only its nonce-bearing product script, as described below.

`X-Frame-Options: DENY`, which every other response carries, is removed from preview responses so the
dashboard can frame them; `frame-ancestors 'self'` still refuses every other site. Responses are
`Cache-Control: no-store`, so Reload always shows the file as it is.

**Do not add `allow-same-origin` to either sandbox.** With it, the page's scripts would run in the
dashboard's origin and could call every unauthenticated route, including those that start processes.

### Localhost pages

A localhost URL on the panemux host ([behavior](../behavior/preview.md#opening-a-localhost-url)) is
not served by panemux: `PreviewSplit` frames the URL itself, and panemux makes no request to it.
The page is the operator's own development server, the same page a tab would show, so it keeps its
server's origin — its module scripts, hot reload socket and storage depend on it.

The page is not framed directly. `PreviewSplit` frames a document of panemux's own
(`localhostFrameDocument` in `utils/preview.ts`, given as `srcdoc`) that holds one frame of the page,
and that document's `Content-Security-Policy` is `default-src 'none'; style-src 'unsafe-inline';
frame-src` followed by `http`/`https` on any port for `localhost`, `127.0.0.1` and the IPv4 loopback
address the URL names. A browser enforces `frame-src` on every navigation of that frame, a redirect
or one the page starts itself, so a localhost page that leads to an external site — a `302`, an
open redirect, a login bounced to an external identity provider, `location.href` — is refused in
the frame rather than shown under the localhost URL; #359 opens external sites in a tab. Navigation
between loopback addresses stays allowed. The document runs no script of its own (`default-src
'none'`), and the URL reaches it only as an escaped attribute value.

A policy cannot name an IPv6 address, so an `[::1]` URL is framed as `localhost` on the same port,
path, query and fragment, which the browser resolves to `::1` as well. That page's origin is then
`http://localhost:<port>`, not `http://[::1]:<port>`: its cookies and storage are localhost's, not
those a tab on the `[::1]` URL would use. Open in new tab opens the `[::1]` URL itself.

Both frames' `sandbox` is `allow-scripts allow-same-origin allow-forms allow-modals allow-popups`
(the inner frame's flags can only narrow the outer's):

- `allow-same-origin` here leaves the page in **its own** origin (`http://localhost:<port>`), never
  the dashboard's. It is not the `allow-same-origin` forbidden above, which would apply to pages the
  dashboard's origin serves. The `srcdoc` document is in the dashboard's origin, which is why it
  holds no script and why its policy forbids any; the page inside it is cross-origin to it and can
  neither read it nor, being sandboxed, navigate it. A URL naming the dashboard's own origin cannot be used to get there:
  every dashboard response carries `X-Frame-Options: DENY`, and `/preview/` responses carry their own
  `sandbox` policy, which the browser applies together with the frame's.
- Another loopback port is another origin, and the
  [shared browser request guard](auth.md#shared-browser-request-guard) refuses its requests to
  `/api/*` and `/ws/*`, as it would from that page in a tab.
- Without `allow-top-navigation` or `allow-popups-to-escape-sandbox` it cannot navigate the dashboard,
  and a window it opens stays sandboxed.
- Only loopback `http`/`https` URLs, and only in a terminal on the panemux host, are opened in the
  frame (`usePreview.openUrl` checks again), and the policy above keeps the frame on loopback after
  that; a request from terminal output is framed only after the operator presses Preview
  ([url-open.md](url-open.md#browser-open-interception)).

### Markdown rendering

Markdown is rendered on the server by goldmark with its defaults left in place: raw HTML is omitted
and links with a script scheme (`javascript:` and the like) are dropped. goldmark checks a
destination before decoding its character references, so `jav&#x61;script:` would pass that check
and be written out as `javascript:`; an AST transformer (`dangerousLinks`) therefore checks every
link and image destination again with goldmark's own `IsDangerousURL`, as the browser will read it —
references decoded, tabs and newlines removed, leading spaces and control characters trimmed — and
empties the ones it refuses. It refuses nothing goldmark's check would let through as written. Do
not enable goldmark's
`WithUnsafe`; the renderer is the first guard, supplemented by the policy, including the restricted
product script exception for Mermaid.

### Mermaid rendering

Markdown containing a Mermaid fenced block permits the product renderer with the following HTTP
policy; the nonce is generated afresh for every response using `crypto/rand`:

```text
sandbox allow-scripts; default-src 'none'; script-src 'nonce-<random>';
script-src-attr 'none'; connect-src 'none'; object-src 'none'; frame-src 'none';
worker-src 'none'; base-uri 'none'; form-action 'none'; img-src * data:;
style-src 'unsafe-inline'; frame-ancestors 'self'
```

The renderer is a separate, bundled classic script, served by content hash under
`/preview-assets/`. Only the exact bundled asset exists there; unknown names return 404, with
no filesystem lookup or SPA fallback. It contains public product code and needs no preview
token. The nonce authorizes that script element, not arbitrary same-host scripts. Neither
`unsafe-eval`, script `unsafe-inline`, `allow-same-origin`, nor CORS permission for `Origin: null`
is added. The iframe keeps its existing `sandbox="allow-scripts"` and the API/WS request guard
continues to refuse the preview's opaque origin. Markdown without Mermaid keeps its old policy.

Goldmark emits escaped code; the renderer reads `textContent`, never evaluates Markdown, and
uses Mermaid's strict security level with HTML labels disabled. All diagram frontmatter and
legacy directives are rejected before Mermaid preprocessing can apply their configuration.
Each diagram reinitializes product settings and never calls returned `bindFunctions` handlers.
Mermaid first inserts unsanitized SVG and generated styles into the live preview DOM to measure
layout. That intermediate DOM is governed by the same CSP as the document: image shapes such
as `A@{ img: "https://example.invalid/image.png" }` can fetch their image under `img-src * data:`.
A failed image decode fails that diagram. Sanitizing the final result cannot undo those requests.

The returned SVG then passes through DOMPurify with executable, HTML, image and link elements
forbidden; nonlocal references and unsafe CSS are removed before the final SVG is inserted.
Both stages stay in the preview's opaque origin, never the dashboard DOM. This is not a
network-isolated viewer: Markdown images and images requested during Mermaid measurement can
load externally. Bundling the runtime avoids a CDN dependency; it does not prevent requests
explicitly named by the document.

The document's 10 MiB limit remains. At most 10 diagrams are attempted, with 20,000 UTF-16
code units per diagram, 50,000 combined code units per document and Mermaid's 100-edge limit
(where the diagram type supports it). Rejected source also consumes the document text budget.
These bounds do not interrupt synchronous layout or guarantee a CPU deadline.

### Paths and symbolic links

`POST /api/sessions/{id}/preview` is an `/api/*` route behind the request guard; a page on another
site cannot ask for a file. This section covers `local` and `tmux` sessions, whose files are on this
host; `ssh` and `ssh_tmux` sessions are in [Previewing Files of an SSH Host](#previewing-files-of-an-ssh-host).
Only HTML and Markdown names resolve.

Only files inside the session's working directory and its agent's worktrees can be previewed. That
is the boundary #359's "path and symlink escape" asks for: the request names a path, and the server
decides which directories it may reach — never the path itself.

Each opened file is registered with a **root**, the innermost of those directories holding it (see
[behavior](../behavior/preview.md#what-is-shown)):

- the clicked path is made absolute against each directory and must lie inside one of them; an
  absolute path elsewhere or a `../` that leaves them is refused (`403`). The working directories
  are reported resolved while a shell prints the path it was given, so a path that is not inside one
  as written is decided by identity: the deepest directory above the file that exists is resolved
  through its symbolic links, and that resolved path — which holds no link left to follow — is
  walked up, each directory looked up through an `os.Root` for `/` by its path relative to `/` and
  compared with `os.SameFile`. A path whose prefix leads into a working directory through a
  symbolic link — to the working directory, to one of its parents or to a directory below it — or
  in another letter case on a case-insensitive filesystem, is therefore inside it. Only directories
  are decided this way, never the file: the rest of the clicked path below that directory is
  reached through the root's `os.Root`;
- the file is then looked up, and at registration checked again, only through `os.Root` for its
  root, by the path relative to that root (`filepath.Rel`). No request-derived path is ever passed
  to `os.Stat` or `os.Open`; `os.Root` refuses a `..` or a symbolic link that leaves the root, and
  any symbolic link with an absolute target, so a link out of the working directory is refused
  (`403`) and a link with a relative target that stays inside works;
- every request below the token is opened through `os.Root` for that root, which refuses `..`
  components and symbolic links that leave the root at open time, so a link added after
  registration is refused too. The request path is also refused unless `fs.ValidPath` accepts it,
  and a backslash is refused outright;
- directories are never listed, and anything but a regular file answers `404`.
- a file that cannot be read for lack of permission is reported as not found, not as outside the
  working directories: only a path or symbolic link that leaves them is an escape.

### Tokens

A token is 128 random bits from `crypto/rand`, issued only by the guarded API. The `/preview/` route
itself is outside `/api` and the request guard on purpose — the preview's own subresource requests
are cross-site by construction — so the token is what keeps another site from reading a file: it
cannot learn one. Tokens are kept for the server's lifetime, as a tab opened on a preview is meant
to outlive the pane it came from ([lifetime](../behavior/preview.md#lifetime)).

### Size

The previewed HTML or Markdown file is at most `preview.MaxDocumentBytes` (10 MiB), checked when it is
opened and again on every request, and Markdown is read through a reader bounded to that size, so a
file that grew does not turn rendering into an unbounded read. HTML is served through an
`io.SectionReader` of the size that request checked, since `http.ServeContent` seeks to the end of
what it is given to learn the length: a file that grows past the limit while it is being served is
cut at the checked size. Other files are streamed with
`http.ServeContent` and are not bounded.

## Previewing Files of an SSH Host

For an `ssh` or `ssh_tmux` session (a pane, or a task's attach) the preview reads the file from the
session's host and can relay a localhost URL of that host (issue #387,
[behavior](../behavior/preview.md#panes-on-an-ssh-host)). Read this section before changing
`internal/preview`'s `remote.go` or `relay.go`, `session.PreviewConn`, or `previewHosts` in
`internal/api/preview.go`.

### The connection

- `session.DialPreviewConn` dials with the pane dialer (`dialSSHClient`): the same authentication,
  ProxyJump, ProxyCommand and `knownhosts` verification as a pane, with the `SSHConfig` the session
  itself was dialed with (`session.PreviewTarget`), never its `ssh_connections` entry as it reads
  now. An edited entry restarts no pane, and re-resolving it would read a host the pane is not on,
  under the pane's working directory there. It is never the pane's own connection, so closing the
  pane ends neither a tab opened on a file nor a relay.
- **No command runs over it.** Files are read over the SFTP subsystem and URLs reached over
  direct-tcpip channels. No path, port or host name a preview names is ever part of a remote shell
  command, so none of [command-execution.md](command-execution.md)'s shell-argument rules apply. The
  **SFTP subsystem is required**: a host without one cannot show files, and no other reading method
  (`scp`, `cat` over exec) is used in its place.
- Sessions dialed with the same `SSHConfig` share one `preview.Remote`, which is made again, to that
  same configuration, when its connection ends, receives nothing for 15 s while a keepalive
  (`keepalive@openssh.com`, sent after 15 s with nothing received) or a request's lookups wait on it,
  or fails with anything but an SFTP status about a file (its SFTP subsystem is gone): what was
  opened on a host stays on it. Liveness is anything read on the transport
  (`session.PreviewConn.LastRead`, recorded by a wrapper around the transport to the host), as
  OpenSSH's `ServerAliveInterval` counts it: a reply queued behind relay data on a slow link is late,
  and a fixed bound on it alone would close a connection that is busy, not dead. The
  SSH connection is closed before its SFTP client, whose `Close` would otherwise wait behind a
  request stalled on the host. All are closed when the server shuts down, and `Close` never waits
  for a connection being made, which is closed once it is.

### Remote paths and symbolic links

The boundary is the one above: only files inside the session's working directories on its host. The
host's filesystem cannot be opened through an `os.Root`, so the guard is a resolution done by panemux
itself:

- `realPath` resolves a path one component at a time with SFTP `LSTAT`, replacing each symbolic link
  by its `READLINK` target (absolute, or relative to the directory holding the link) and taking `..`
  against the already resolved, link-free prefix. SFTP's own `REALPATH` is **not** used: servers
  differ in whether it follows links (pkg/sftp's server cleans the path lexically), and a check built
  on it would let a link out of the working directory through on such a server. At most 40 links are
  followed, so a loop ends.
- Each working directory is resolved the same way, and a file is found, registered and served only
  when the path it resolves to lies inside the path a working directory resolves to. Unlike a local
  file, a link with an absolute target that stays inside works: the decision is made on the resolved
  path, not by an `os.Root`. A path in another letter case on a case-insensitive host is not matched.
- Every request below a token resolves again before it opens, so a link added afterwards is refused
  too. What remains is the interval between that resolution and the `OPEN` of the resolved path: a
  process on the host that can write inside the working directory could swap a component for a link
  in between. That process can already write the files the preview shows; the check exists for
  paths and links a file names, not to contain the host's own users.
- The request path is checked with `fs.ValidPath` and backslashes refused, as for a local file; the
  10 MiB limit, the policies and the token are the same.

### Relays

A relay binds a real listening socket on the panemux host, so its scope is deliberately narrow:

- **Loopback only.** `relayBindHost` is the constant `127.0.0.1`, and the port is `0`, chosen by the
  system; nothing derives either from a request.
- **Loopback targets only.** `ParseLoopbackURL` accepts only `http`/`https` URLs of `localhost`,
  `127.0.0.0/8` or `::1`; the relay dials that address and port from the host, over the preview's
  connection. It is never a route to another machine.
- **TLS is passed through.** The relay copies bytes and terminates nothing, so an `https` relay of a
  `localhost` URL is named `https://localhost:<relay port>`: the browser verifies the host's own
  certificate for the name it was issued for. panemux never sees or holds the page's TLS keys.
- **One relay per host and address.** A relay is keyed by the `preview.Remote`, the address and the
  port, and never handed to another: a tab keeps the target it was opened on (#359).
- **Kept for the server's lifetime.** #359 rules out tying a tab to the pane, a count limit or an idle
  expiry, so relays have none; they close when the server shuts down. This differs on purpose from
  [loopback port forwarding](url-open.md#loopback-port-forwarding), which republishes an OAuth
  callback at its own port number and is bounded.

What a relay grants, stated plainly: while panemux runs, every process on the panemux host can reach
that one loopback address of the SSH host through `127.0.0.1:<relay port>`, as with a forward.

### The relayed page's origin

A relayed page is not served by panemux and carries no panemux policy. It runs in the origin of its
relay, `http://127.0.0.1:<relay port>`, which is never the dashboard's: the relay's port is another
loopback port, and the [shared browser request guard](auth.md#shared-browser-request-guard) refuses
another loopback port and every `same-site` request before any handler runs, so the page cannot call
`/api/*` or `/ws/*`.

`PreviewSplit` frames it exactly as a [localhost page](#localhost-pages): inside panemux's own
`srcdoc` document whose `frame-src` keeps the frame on loopback, with the localhost page's sandbox.
That keeps the page in its own origin rather than an opaque one, because a development server's
module scripts, storage and hot-reload WebSocket need it; it is safe only because that origin is the
relay's. **Never give a frame of a `/preview/` file `allow-same-origin`** — that origin is the
dashboard's ([Isolation](#isolation-from-the-dashboards-origin)). Cookies are kept by host name, so
relayed pages share `127.0.0.1`'s cookies with each other; panemux itself keeps none.
