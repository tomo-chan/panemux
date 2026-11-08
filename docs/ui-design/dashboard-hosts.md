---
type: Specification
title: "Dashboard Hosts"
description: "Host chips, connection menus, tool controls, unreadable state, and host editing."
---

# Dashboard Hosts

These controls belong to the [task dashboard](task-dashboard.md).
Runtime contracts are in [task behavior](../behavior/tasks.md).

- **Host connection menu.** The chip of a remote host whose collection succeeded is a button
  (`aria-haspopup="dialog"`, `aria-expanded`) with a small terminal icon; the panemux host's chip
  opens its tool items (Codex input notifications, plus agmsg when Agent Board is enabled),
  and a `connecting`, failed or not-yet-collected
  chip is plain text
  ([behavior](../behavior/tasks.md#opening-a-host)). Pressing it opens a small popover dialog anchored
  under the chip, titled after the host: a `Connection` group of `ssh` / `ssh_tmux` radios (`ssh`
  checked), the tmux session name the server generated in the monospace face once `ssh_tmux` is
  chosen (or "Could not name the tmux session: …" in red), and `Cancel`, `Open` and `Type in pane`
  in the interactive blue, labelled `Open: <host>` and `Type in pane: <host>` like a task's
  buttons. A press outside it closes it; focus returns to the chip. A host that stops being
  reachable takes its menu with it, and focus goes to its `Reconnect`.
- **Codex input notifications.** Below the menu's actions, separated by a rule, a row reads
  `Codex input notifications` with a small `Install` or `Update` button at its end, or `Installed`
  in the Working green, and a muted line under it (`Not installed`, `Not checked: …`); a
  `hooks.json` panemux will not rewrite reads `Cannot install: …` in red, with no button. After an
  install, a muted line says to trust the hook in codex's `/hooks`. A host without codex shows no row.
  Under the status, an `Update automatically` checkbox, off until turned on; a muted line says the
  hook was updated automatically and to trust it, and a red one why that failed, the setting could
  not be read (the checkbox is then disabled), or it was not saved. The checkbox and the
  `Install`/`Update` button are each disabled while the other's request runs.
  The panemux host's chip is a button too, with no terminal icon: its menu holds this row and
  `Close` alone ([behavior](../behavior/tasks.md#codex-permission-requests)).
- **Unreadable state.** The warning is amber, one for every host together, and never beside a host
  chip, where it would crowd them as hosts are added. It opens a modal anchored below the top bar at
  the right and kept within the screen's height, titled "Unreadable session state · <n> files on <m> hosts": a table with one row per
  file — host, file name, reason with what was found beneath it, and the process (`claude · pid
  <pid>` with where it runs, or `unknown` when the file name carries no pid) — and a footer naming
  the fields panemux reads, a link to the steps to fix it in `docs/behavior/tasks.md`, and *Copy
  details*, disabled where the clipboard is not available. The files are not cards on the board
  ([Unreadable state files](../behavior/tasks.md#unreadable-state-files)).
- **Hosts….** A modal over the dashboard titled "Dashboard hosts", with `ssh_connections in
  config.yaml` beside the title and a note that a host only in `~/.ssh/config` works in panes but is
  not collected from until its name is added here. A table lists each entry — name, where it
  connects (`user@host:port`, or "from ~/.ssh/config" for a name-only entry), and small tags for
  `~/.ssh/config`, `key file`, `password set` and `used by N panes` — with `Edit` and `Delete` per
  row, and `Add host` and `Close` below it. `Add host` and `Edit` open a form under the table, and
  while the form or a delete confirmation is open, `Add host` and `Close` are hidden so only that
  step's own buttons show; its `Cancel` returns to the list, and `Escape` or a click outside still
  closes the dialog. The form has Name (offering the `~/.ssh/config` host
  names; read-only when editing, since a name cannot change), Host (marked optional, with a note
  that empty fields use the block's values, when `~/.ssh/config` has a block of that name), User,
  Port, Key file, Known hosts file and Password. The password field is always empty; for an entry
  with one its placeholder says to leave it empty to keep it, and a `Remove saved password` checkbox
  disables it. What the form can tell without the server — a missing or malformed name, a name
  already listed, a port outside 1–65535, no host and no block to take it from — is refused under the
  form; the server's refusals appear there as "Could not save: …", keeping what was typed. `Delete`
  asks in an inline red confirmation; a host panes use by name with no `~/.ssh/config` block to fall
  back to is refused before asking, naming the panes, and a refusal only the server can tell — a
  pane or host whose `ProxyJump` goes through it — appears as "Could not delete …". A green notice says what was added, saved or deleted, and the board collects again.
