# Behavior: opening URLs from a pane

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## Opening URLs from a Pane

CLI login flows (`claude` MCP OAuth, `gh auth login`, `wrangler login`, `gcloud auth login`) start a
listener on the loopback interface of the machine the CLI runs on, then send the browser to an
authorization URL whose `redirect_uri` points back at `http://localhost:<port>/…`. For `ssh` and
`ssh_tmux` panes that listener is on the remote host, so the callback would otherwise resolve to the
wrong machine and the CLI would wait forever.

### Loopback port forwarding

When a URL is opened from a pane, panemux resolves the loopback port it expects its callback on and
republishes that port at the identical port number on the panemux host, over the pane's existing SSH
connection. The port is taken from:

- the URL's own host, when it already points at loopback (`http://localhost:3000/…`), or
- a `redirect_uri` / `redirect_url` / `callback_uri` / `callback_url` query parameter (percent-encoded
  or plain, and matched regardless of case or `_`/`-` spelling) whose value points at loopback.

Only ports 1024–65535 are forwarded; a callback URL with no explicit port (implying 80 or 443) is
never an OAuth loopback listener in practice and would need a privileged bind. Forwards bind
`127.0.0.1` only, are shared per pane and port, expire after 30 minutes without traffic, and are
closed when the pane is deleted, restarted, or the server shuts down. A pane may hold at most 8
forwards, and the server at most 32.

`local` and `tmux` panes need no forward: their callback listener is already on the panemux host.

**This assumes the browser showing the dashboard runs on the panemux host** — the documented local
workflow. panemux can only bind ports on its own host, so when the dashboard is opened from a
different machine, forwarding does not make that machine's `localhost:<port>` resolve to the pane.
That deployment shape is out of scope.

### Browser-open interception

Some flows never print a usable URL: they invoke `$BROWSER`, `xdg-open`, or `open` directly on the
pane's host, which on a remote or headless machine opens nothing the operator can see. With
`url_open.browser_shim` enabled (the default), `local` and `ssh` panes get a small POSIX shell shim
installed under the pane host's user cache directory (`~/.cache/panemux/bin` remotely), exported as
`$BROWSER` and prepended to `PATH` as `xdg-open` and `open`. Given a single `http`/`https` argument
the shim writes a private OSC sequence (identifier `7373`) to the pane's terminal instead of opening
anything locally; panemux's frontend consumes that sequence, so it is never drawn. For every other
invocation — a file path, a non-http scheme, extra flags — the shim execs the real opener from the
pane's original `PATH`, so `xdg-open report.pdf` and `open -a Safari …` behave exactly as they would
without panemux. When the shim cannot be installed (a read-only home directory, for instance) the
pane still starts, just without interception.

A URL requested this way is never opened automatically. The pane shows a strip naming the URL with
`Open` and `Ignore`; only `Open` opens the tab and prepares the forward. The request is a live event:
a sequence that arrives in replayed scrollback is ignored, so reconnecting a pane never re-raises a
request that was already opened or dismissed. Terminal output is
untrusted — see [security.md](../security.md) — and the browser also requires a user gesture to open a
tab, so the approval step is both a safety and a functional requirement.

Two limitations are inherent to the mechanism:

- `tmux` and `ssh_tmux` panes are not covered. Their shell environment is inherited from a tmux
  server that was started independently of panemux, and tmux does not pass unknown OSC sequences
  through to the outer terminal by default.
- With the shim enabled, an `ssh` pane that would otherwise have used the SSH shell request runs a
  command instead, so it execs the login shell explicitly (`$SHELL -l` for `bash`, `zsh`, and
  `fish`; a plain `exec "$SHELL"` for anything else). Panes that set `cwd` or `shell` already ran a
  command and keep exactly the form they had before. Setting `url_open.browser_shim: false` restores
  the previous behavior everywhere.

### Clicked links

Links in the terminal are opened by the frontend in a new tab. A link that is itself a loopback URL
is opened into a blank tab that is navigated once the forward is ready, because the browser would
otherwise reach the port before it exists; every other URL opens immediately and the forward is
prepared alongside it, since its callback only fires after the operator has logged in. A forward
that fails is reported in the pane rather than silently swallowed.
