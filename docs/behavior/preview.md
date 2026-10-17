# Behavior: the split preview

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## Split Preview

A workspace pane and the task dashboard's Type in pane popup can show an HTML or Markdown file, or a
localhost page, beside their terminal: the terminal on the left, the preview on the right (issues
#385 and #386). It is how a mock or a
design note an agent wrote is checked while the agent is still being instructed in the same terminal.
The isolation that keeps the previewed page away from panemux's own API is in
[security/preview.md](../security/preview.md).

![A local pane split in two: its terminal on the left and the preview of the HTML mock whose path it printed on the right, under a header naming the host and the path, with Reload, Open in new tab and Close.](../images/preview.png)

### Opening a file

An HTML or Markdown path printed in the terminal is a link: `.html`, `.htm`, `.md` and `.markdown`,
in any letter case. Absolute paths (`/workspace/user/project/index.html`), dot-relative paths
(`./mockups/index.html`, `../notes.md`) and bare relative paths (`docs/design.md`) are recognized; a
trailing `:12` or sentence punctuation is not part of the path, and a path inside a URL belongs to the
URL. Paths are links only in a terminal on the panemux host — `local` and `tmux` panes, and the Type
in pane popup of a task on the panemux host. `ssh` and `ssh_tmux` panes and remote tasks show none.

Clicking one opens the split. A relative path is resolved on the server
([`POST /api/sessions/{id}/preview`](rest-api.md#post-apisessionsidpreview)) against:

- the shell's current working directory, then
- each worktree the pane's coding agent is working in (the same candidates the pane header's Git
  information uses).

When exactly one of them holds the file, it opens. When several do — the same relative path in two
worktrees — the preview lists the files found and opens the one chosen. When none does, or the path
cannot be previewed, the preview shows the reason instead, naming the directories looked in. An
absolute path names its own file, which must lie inside one of them.

Only files inside those directories — the shell's working directory and the agent's worktrees — can
be previewed (#359's "path and symlink escape"). An absolute path elsewhere, a `../` that leaves
them, and a symbolic link that leads out of them do not open; the preview shows that the file is
outside them and names the directories allowed. A `../` that stays inside one of them, such as from
a subdirectory of a worktree up to its root, opens. A path is inside a directory however it is
spelled: through a symbolic link in its prefix (macOS's `/tmp` for the `/private/tmp` a working
directory is reported as, or a link to a directory inside it) or, on a case-insensitive filesystem, in another letter case. When the
working directory and the worktrees are all unknown, no path opens.

### Opening a localhost URL

A `localhost`, `127.x.x.x` or `[::1]` `http`/`https` URL in a terminal on the panemux host — a `local`
or `tmux` pane, or the Type in pane popup of a task on the panemux host — opens in the same split
(issue #386), typically a development server the agent started. It reaches the preview three ways,
all handled by the one URL opener the pane already uses
([opening URLs from a pane](url-open.md)):

- **A printed link**, clicked: it opens in the preview at once. Its text is the URL, so the operator
  sees what they click.
- **The browser shim**: a program in the pane opening a browser (`vite --open`, `xdg-open`) shows the
  approval strip with **Preview**, **Open** and **Ignore**. Preview shows the page beside the
  terminal; Open opens it in a tab exactly as before, including the OAuth callback forward;
  Ignore drops it.
- **An OSC 8 hyperlink**, clicked: its target is chosen by the program's output and need not be its
  text, so it shows the same strip, saying **This link goes to** and naming the target.

Each request opens at most once. The strip's buttons act on the request they were shown for and
then remove it, so a second press, or Preview followed by Open, opens nothing more; clicking the same
URL as a link while its request is showing removes the request. A request is a live event: one that
arrives in replayed scrollback after a reconnect is not raised again.

Any other URL — an external site, or a localhost URL in an `ssh` or `ssh_tmux` pane or a remote
task, whose `localhost` is another host — opens in a tab as before, with no Preview. While the
terminal is not connected (its WebSocket dropped, or its session ended) the preview cannot open, so
Preview is not offered either: the strip keeps Open and Ignore, and a clicked localhost link opens in
a tab.

The strip is drawn over the terminal side of the split, so while a preview is open its header —
the URL it shows, Reload, Open in new tab and Close — stays in view. In the Type in pane popup a
request belongs to the attach that raised it: Retry or Reconnect replaces the terminal, and a request
still waiting is dropped with it.

The page is framed as its server serves it, in the server's own origin, with its scripts running,
so a development server's modules, hot reload and storage work as they do in a tab. Under the
header the preview says what a frame cannot always show: a page that refuses to be framed
(`X-Frame-Options` or `frame-ancestors`) stays blank, and hot reload, sign-in or storage can behave
differently than in a tab of its own, for which **Open in new tab** is the way to see it as a normal
page. Reload loads the URL again, and the header names the host (`local`) and the URL.

The preview stays on this host's loopback: a page that redirects, or sends itself, to another site
— an external login, an open redirect — shows the browser's refusal in the frame instead of that
site, and the header keeps naming the localhost URL. External sites open in a tab. Moving between
`localhost` and `127.0.0.1` addresses is allowed.

An `[::1]` URL is shown as `localhost` with the same port, path, query and fragment, since the frame
cannot be limited to an IPv6 address; the browser reaches the same server through `localhost`. The
header names the `localhost` URL shown, and **Open in new tab** opens the `[::1]` URL. The page's
origin is then `localhost`'s, so its cookies and storage in the preview are not those of a tab on
the `[::1]` URL.

### What is shown

- **HTML** is shown as written, with its scripts running, and loads the stylesheets, scripts and
  images beside it by relative path.
- **Markdown** is rendered as GitHub-flavored Markdown (tables, task lists, strikethrough,
  autolinks). Raw HTML inside it is left out, and the page runs no scripts. Relative links and
  images resolve beside the file, so a link to another `.md` or `.html` opens it in the preview.

A file is served from its **root**: the innermost working directory that holds it. Files beside it
are reachable only within that root. A path, or a symbolic link, that leads outside the root is
refused. A symbolic link with a relative target that stays inside the root works; one with an
absolute target is refused even when it points inside, as is a link from one working directory into
another (the file is then found, if at all, under the directory it really lives in).

The HTML or Markdown file itself may be at most **10 MiB**; a larger one shows the reason instead.
The files it loads beside it are not bounded.

### The preview header

The header names the host (`local` for the panemux host) and the file's absolute path, or the URL
of a localhost page, and carries:

- **Reload**: loads the file again as it is now on disk, or the localhost URL again.
- **Open in new tab**: opens the same page in a browser tab. A tab opened this way is the browser's
  own: closing the preview, the popup, the pane or the terminal session does not close it, and it
  keeps working for as long as panemux runs. It is how a page is seen larger; the preview has no
  full-screen mode of its own.
- **Close**: closes the preview only. The terminal takes the whole body again.

While a file is resolving, Reload and Open in new tab are disabled.

### The divider

The split starts at 50:50. The divider between the terminal and the preview:

- moves by dragging with a mouse, a pen or touch;
- moves by 5 points with the Left and Right arrow keys when it has focus;
- returns to 50:50 on a double-click;
- keeps the terminal's share between 20% and 80%, and neither side narrower than 160 pixels.

In a pane too narrow for both sides at 20% or 80%, the divider's range narrows to the shares that
leave both sides 160 pixels (in a 450-pixel split, 36% to 63%), and the split stays at 50:50 where
even that is impossible. The divider stops at the end of that range rather than moving past it
unseen: the share it shows is the one it announces to assistive technology, the one the arrow keys
step from, and the one kept once it is moved. A kept ratio outside a narrow pane's range shows at
the range's end until the divider moves, and in full once the pane is wide enough again.

The ratio is kept per pane (workspace panes) and per task (the Type in pane popup) in the browser's
`localStorage`, so the next preview of that pane or task opens at it. A browser that refuses storage
opens at 50:50.

### The terminal stays as it is

The terminal is the same terminal with or without a preview: prompt input, IME, selection and copy,
and scrollback work as before, and nothing typed is lost when the preview opens, closes or is
resized. Narrowing it resizes the PTY as resizing a pane does. Loading, a failed load and the
preview's own content never take the terminal's keys; the preview runs in a frame of its own.

In the Type in pane popup, the preview sits beside the terminal's area, not inside it: keys typed in
the terminal are the terminal's, and Tab cycles through the preview's controls within the popup as
through the rest of the popup.

Once the previewed page itself has focus — it was clicked, to scroll it or to type in one of its
fields — the keys typed are the page's. The page runs sandboxed in another origin, so they never
reach the dashboard, and panemux's own shortcuts do nothing until focus is back: in the popup,
Ctrl/Cmd+Shift+Esc and Ctrl/Cmd+Shift+Enter. Meanwhile the preview shows **Keys go to the page.
Click the terminal to use panemux's shortcuts.** under its header; clicking the terminal, or
anything else outside the page, returns focus and removes the note. Reloading the page also returns
focus. This holds in a workspace pane as in the popup.

### Lifetime

The preview inside a pane or the popup is hidden when its terminal is not connected — its WebSocket
drops, or the session ends — and stays hidden after a reconnect; clicking the path again reopens it.
A tab opened with **Open in new tab** is not affected by any of this. A localhost page follows
the same rule as a file.
