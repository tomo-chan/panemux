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

- Local `tmux` panes and already-existing remote tmux sessions are not configured for interception.
  Reattaching does not change a running shell or Claude process's environment. URLs printed into
  these panes remain clickable and can still prepare an SSH forward.
- With the shim enabled, an `ssh` pane that would otherwise have used the SSH shell request runs a
  command instead, so it execs the login shell explicitly (`$SHELL -l` for `bash`, `zsh`, and
  `fish`; a plain `exec "$SHELL"` for anything else). Panes that set `cwd` or `shell` keep their
  `cd` and `exec`, after the shim's setup. A pane whose ID is exported as `PANEMUX_PANE_ID`
  ([task dashboard](tasks.md#the-pane-of-an-agent-outside-tmux)) runs such a command whether or not
  the shim is enabled; `url_open.browser_shim: false` only drops the shim from it. A pane with
  neither keeps the SSH shell request, or its plain `cd` / `exec` command.

A command with any setup is POSIX shell handed whole to `/bin/sh` as one line
(`exec /bin/sh -c '…'`), because sshd runs it with the user's login shell, which need not be POSIX.
It therefore behaves the same under `fish` and `tcsh` as under `bash`: the shim is installed, the
variables are exported, and the login shell starts.

### New remote tmux sessions

With the shim enabled, an `ssh_tmux` pane that **creates a new remote tmux session** installs the
same shim and supplies `BROWSER` and a tmux opt-in marker using `tmux new-session -e`.
A fixed bootstrap in the initial pane prepends the shim to that pane's effective `PATH`, saves the
original path for non-URL fallback, then invokes the configured `default-command` or login shell.
tmux normally overrides the initial pane's `PATH` with its client's path, so setting `PATH` with
`-e` alone is insufficient. The server's global environment, existing sessions and configured
`default-shell`/`default-command` options are preserved. Later windows inherit the session's
`BROWSER` and marker; the initial pane's PATH bootstrap does not run in later windows.
The task dashboard's attach to a session created elsewhere does not add this setup.

This requires tmux **3.3 or newer**. Older or unrecognized versions, disabled interception and
failed shim installation keep ordinary tmux attach/create behavior without interception.

When a program in an opted-in session requests a URL, the shim enables `allow-passthrough` on
**that tmux pane only** and wraps OSC 7373 in tmux's DCS passthrough sequence. The frontend receives
the same live event as an `ssh` pane and shows `Open` / `Ignore`; neither a tab nor a forward is
created before approval. No server-wide or window-wide tmux option is changed. Nested tmux sessions
are not covered. Shell startup files that replace `BROWSER` or remove the shim from `PATH` can
disable interception, as they can in an ordinary `ssh` pane.

Fresh local panes and ordinary SSH shim setup clear an inherited tmux opt-in marker so a panemux
process launched inside tmux cannot wrap those panes' notifications for an unrelated parent pane.

### Clicked links

Links in the terminal are opened by the frontend in a new tab. A link that is itself a loopback URL
is opened into a blank tab that is navigated once the forward is ready, because the browser would
otherwise reach the port before it exists; every other URL opens immediately and the forward is
prepared alongside it, since its callback only fires after the operator has logged in. A forward
that fails is reported in the pane rather than silently swallowed.
