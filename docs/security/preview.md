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
and links with a script scheme (`javascript:` and the like) are dropped. Do not enable goldmark's
`WithUnsafe`; the page's policy forbids scripts besides, but the renderer is the first guard, not the
policy.

### Paths and symbolic links

`POST /api/sessions/{id}/preview` is an `/api/*` route behind the request guard; a page on another
site cannot ask for a file. It resolves only for `local` and `tmux` sessions, whose files are on this
host, and only HTML and Markdown names.

Each opened file is registered with a **root** (see
[behavior](../behavior/preview.md#what-is-shown)):

- registration resolves both the root and the file through their symbolic links and refuses a file
  whose real path is outside the root's real path;
- every request below the token is opened through `os.Root` for that root, which refuses `..`
  components and symbolic links that leave the root at open time, so a link added after
  registration is refused too. The request path is also refused unless `fs.ValidPath` accepts it,
  and a backslash is refused outright;
- directories are never listed, and anything but a regular file answers `404`.

### Tokens

A token is 128 random bits from `crypto/rand`, issued only by the guarded API. The `/preview/` route
itself is outside `/api` and the request guard on purpose — the preview's own subresource requests
are cross-site by construction — so the token is what keeps another site from reading a file: it
cannot learn one. Tokens are kept for the server's lifetime, as a tab opened on a preview is meant
to outlive the pane it came from ([lifetime](../behavior/preview.md#lifetime)).

### Size

The previewed HTML or Markdown file is at most `preview.MaxDocumentBytes` (10 MiB), checked when it is
opened and again on every request, and Markdown is read through a reader bounded to that size, so a
file that grew does not turn rendering into an unbounded read. Other files are streamed with
`http.ServeContent` and are not bounded.
