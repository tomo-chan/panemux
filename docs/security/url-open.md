# Security: opening URLs from a pane

> Part of the [security design](../security.md). That document carries the general rules, the `gosec` policy, and the map of these files.

## Opening URLs From a Pane

Two mechanisms sit behind [behavior.md](../behavior.md)'s "Opening URLs from a pane": loopback port
forwarding (`internal/portforward`, `LoopbackDialer` in `internal/session/loopback.go`) and the
browser-open shim (`internal/session/browseropen.go`). Read this section before changing either.

### Loopback port forwarding

A forward binds a real listening socket on the panemux host, so its scope is deliberately narrow:

- **Loopback only.** `internal/portforward`'s `loopbackBindHost` is the constant `127.0.0.1`, and
  nothing derives a bind address from a request. A wildcard bind would republish a remote service to
  the entire network, which is a different feature with a different threat model, not a convenience.
- **Unprivileged ports only.** Ports below 1024 are rejected (`minForwardablePort`), so no forward
  can ever need elevated privileges, and a callback URL with no explicit port — implying 80 or 443,
  which no OAuth loopback listener uses in practice — never triggers a bind.
- **Bounded.** At most 8 forwards per pane and 32 in total, each expiring after 30 minutes without
  traffic, and all closed when the pane is deleted or restarted (`Handler.closeSessionForwards`) or
  when the server shuts down (`Server.Shutdown`).
- **No command execution.** Forwarding runs entirely over the pane's existing SSH connection using
  `(*ssh.Client).DialContext`. No shell is involved, so none of this document's shell-argument rules
  apply — there is no command string for a port number to be interpolated into.

What a forward does grant, stated plainly: while it is open, every process on the panemux host can
reach that one port on the pane's host through `127.0.0.1:<port>`. That is the point of the feature
(the browser is one such process), but it is a real widening of what a local process can reach, which
is why the limits above are hard-coded rather than configurable.

`POST /api/sessions/{id}/open-url` is not behind `bearerAuthMiddleware`: it follows the same
unauthenticated posture as every other `/api/*` route (see "Auth token and transport encryption"
above). It is a stronger primitive than the routes around it — it opens listening sockets — so it is
worth restating what bounds it in that posture: it only ever binds loopback, only ports the URL
itself names, only for a pane that already exists, and only when that pane's shell runs on another
host. Gating it would mean gating `/api/*` as a whole, which remains the separate, larger change
already tracked in this document.

### Browser-open interception

**The shim is not an `exec.Command` sink and does not fall under this document's command-execution
rules.** `remoteBrowserShimSetup` builds a shell snippet from a fixed string literal:
`browserShimScript` contains no caller-supplied value, and it reaches the remote command string
through the same `shellQuotePath` quoting every other remote argument uses. The one path that varies
per host — where the shim is written — is `"$HOME/.cache/panemux/bin"`, resolved by the remote shell
itself rather than by panemux, so no panemux-side value is interpolated at all. Locally the shim is
written with `os.WriteFile` under `os.UserCacheDir()`; no shell parses it.

**panemux writes a shell script to a remote host here, which is new.** This is compatible with this
document's existing rule that the `panemux` binary is never installed on a remote host: that rule
exists because `panemux` is itself a server, and a stray copy could start its own HTTP/WS listener
and command center on an SSH-reached machine. The shim has no such capability — it is a few lines of
POSIX shell that write an escape sequence to the terminal it was invoked from, with no network
access, no persistence beyond the file itself, and no privileges beyond the pane user's own. It is
installed under the pane user's cache directory with mode 0700, every install step is best-effort
(a read-only home leaves the pane working without interception), and `url_open.browser_shim: false`
turns the whole mechanism off.

**Terminal output is untrusted, and the OSC path treats it that way.** The shim's OSC sequence is
just bytes on the pane's terminal stream: any process that can write to that terminal can emit it,
including `cat` of a file an attacker controls. Two guards bound what that can achieve, and both are
required — neither is redundant:

1. **Scheme allowlist.** `parseBrowserOpenOsc` accepts only `http`/`https` URLs, so a crafted
   sequence cannot drive the dashboard's browser to a `file:` or `javascript:` URL. The backend
   applies the same rule independently in `portforward.ValidateOpenURL`, since the OSC path is not
   the only caller.
2. **Explicit approval.** A URL that arrives this way is never opened automatically: the pane shows
   it and waits for the operator to press `Open`. This is the guard that matters, because a
   plausible-looking `https://` URL passes the first check by construction. It is also a functional
   requirement — browsers only open a tab from a user gesture — but it is documented here as a
   security property, because removing the approval step to "smooth out the flow" would hand any
   process that can write to a pane's terminal the ability to navigate the operator's browser.

The shim's fall-through path (`xdg-open report.pdf`, `open -a Safari …`) execs the real opener
resolved from the pane's original `PATH`, exported as `PANEMUX_SHIM_FALLBACK_PATH` before the shim
directory is prepended. The script refuses to exec a target that resolves back into its own
directory, so a `PATH` that still contains the shim directory cannot make it recurse into itself.
