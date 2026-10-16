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
site cannot ask for a file. It resolves only for `local` and `tmux` sessions, whose files are on this
host, and only HTML and Markdown names.

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
