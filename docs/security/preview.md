# Security: the split preview

> Part of the [security design](../security.md). That document carries the general rules, the `gosec` policy, and the map of these files.

## Previewing Local Files

The split preview ([behavior](../behavior/preview.md)) shows HTML and Markdown from the panemux
host's filesystem in the dashboard. An HTML mock is untrusted content: an agent wrote it, and its
scripts run. Read this section before changing `internal/preview`, `PostSessionPreview` /
`ServePreview` in `internal/api/preview.go`, or `PreviewSplit`.

### Isolation from the dashboard's origin

Previews are served by the dashboard's own server under `/preview/<token>/…`, and every preview
response carries a `Content-Security-Policy` `sandbox` directive **without `allow-same-origin`**:

| Response | Policy |
|---|---|
| HTML file | `sandbox allow-scripts; frame-ancestors 'self'` |
| Markdown, rendered | `sandbox; default-src 'none'; img-src * data:; style-src 'unsafe-inline'; frame-ancestors 'self'` |
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
dashboard's origin even if the header were lost on the way. A Markdown page runs no script at all,
by its policy.

`X-Frame-Options: DENY`, which every other response carries, is removed from preview responses so the
dashboard can frame them; `frame-ancestors 'self'` still refuses every other site. Responses are
`Cache-Control: no-store`, so Reload always shows the file as it is.

**Do not add `allow-same-origin` to either sandbox.** With it, the page's scripts would run in the
dashboard's origin and could call every unauthenticated route, including those that start processes.

### Markdown rendering

Markdown is rendered on the server by goldmark with its defaults left in place: raw HTML is omitted
and links with a script scheme (`javascript:` and the like) are dropped. goldmark checks a
destination before decoding its character references, so `jav&#x61;script:` would pass that check
and be written out as `javascript:`; an AST transformer (`dangerousLinks`) therefore checks every
link and image destination again with goldmark's own `IsDangerousURL`, as the browser will read it —
references decoded, tabs and newlines removed, leading spaces and control characters trimmed — and
empties the ones it refuses. It refuses nothing goldmark's check would let through as written. Do
not enable goldmark's
`WithUnsafe`; the page's policy forbids scripts besides, but the renderer is the first guard, not the
policy.

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
  ProxyJump, ProxyCommand and `knownhosts` verification as a pane, for the session's
  `ssh_connections` entry as it resolves when the preview is opened. It is never the pane's own
  connection, so closing the pane ends neither a tab opened on a file nor a relay.
- **No command runs over it.** Files are read over the SFTP subsystem and URLs reached over
  direct-tcpip channels. No path, port or host name a preview names is ever part of a remote shell
  command, so none of [command-execution.md](command-execution.md)'s shell-argument rules apply; a
  host without an SFTP subsystem cannot show files.
- A host's connection is made again when it ends, to the same resolved entry: what was opened on a
  host stays on it. An edited entry gets a new `preview.Remote`; the old one is kept for the previews
  and relays already opened on it. All are closed when the server shuts down.

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

`PreviewSplit` frames it with `sandbox="allow-scripts allow-same-origin allow-forms"`. That keeps the
page in its own origin rather than an opaque one, because a development server's module scripts,
storage and hot-reload WebSocket need it; it is safe only because that origin is the relay's. **Never
give a frame of a `/preview/` file `allow-same-origin`** — that origin is the dashboard's
([Isolation](#isolation-from-the-dashboards-origin)). Without `allow-top-navigation` or
`allow-popups`, the framed page still cannot navigate the dashboard or open windows. Cookies are kept
by host name, so relayed pages share `127.0.0.1`'s cookies with each other; panemux itself keeps
none.
