# Behavior: the split preview

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## Split Preview

A workspace pane and the task dashboard's Type in pane popup can show an HTML or Markdown file beside
their terminal: the terminal on the left, the preview on the right (issue #385). It is how a mock or a
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
absolute path names its own file.

### What is shown

- **HTML** is shown as written, with its scripts running, and loads the stylesheets, scripts and
  images beside it by relative path.
- **Markdown** is rendered as GitHub-flavored Markdown (tables, task lists, strikethrough,
  autolinks). Raw HTML inside it is left out, and the page runs no scripts. Relative links and
  images resolve beside the file, so a link to another `.md` or `.html` opens it in the preview.

A file is served from its **root**: the innermost working directory that holds it, or, for a file
outside every working directory, its own directory. Files beside it are reachable only within that
root. A path, or a symbolic link, that leads outside the root is refused, and a symbolic link that
stays inside it works.

The HTML or Markdown file itself may be at most **10 MiB**; a larger one shows the reason instead.
The files it loads beside it are not bounded.

### The preview header

The header names the host (`local` for the panemux host) and the file's absolute path, and carries:

- **Reload**: loads the file again as it is now on disk.
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

### Lifetime

The preview inside a pane or the popup is hidden when its terminal is not connected — its WebSocket
drops, or the session ends — and stays hidden after a reconnect; clicking the path again reopens it.
A tab opened with **Open in new tab** is not affected by any of this.
