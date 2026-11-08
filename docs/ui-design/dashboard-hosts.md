---
type: Specification
title: "Dashboard Hosts"
description: "Host chips, connection menus, tool controls, unreadable state, and host editing."
---

# Dashboard Hosts

These controls belong to the [task dashboard](task-dashboard.md).
Runtime contracts are in [task behavior](../behavior/tasks.md).

## Connection menu

[Opening a host](../behavior/task-navigation.md#opening-a-host) owns chip availability, session naming,
Open / Type in pane effects, dismissal and focus restoration, including a host losing connectivity.
The remote chip has a terminal icon and `aria-haspopup="dialog"` / `aria-expanded`; the local
chip has no terminal icon and opens tool items, including agmsg when Agent Board is enabled.
The popover sits under the chip with the host as title, `Connection` radios (`ssh`, `ssh_tmux`),
a monospace generated tmux name or red "Could not name the tmux session: …", and `Cancel`,
`Open`, `Type in pane`. The last two are interactive blue with accessible names `Open: <host>`
and `Type in pane: <host>`.

## Host tools and diagnostics

- **Codex input notifications.** Below the menu's actions, separated by a rule, a row reads
  `Codex input notifications` with a small `Install` or `Update` button at its end, or `Installed`
  in the Working green, and a muted line under it (`Not installed`, `Not checked: …`); a
  `hooks.json` panemux will not rewrite reads `Cannot install: …` in red, with no button. After an
  install, a muted line says to trust the hook in codex's `/hooks`. A host without codex shows no row.
  Under the status, an `Update automatically` checkbox, off until turned on; a muted line says the
  hook was updated automatically and to trust it, and a red one why that failed, the setting could
  not be read (the checkbox is then disabled), or it was not saved. The checkbox and the
  `Install`/`Update` button are each disabled while the other's request runs.
  The local menu has tool items and `Close`; [permission behavior](../behavior/task-codex-permissions.md#codex-permission-requests)
  owns hook installation and automatic-update contracts.
- **Unreadable state.** The warning is amber, one for every host together, and never beside a host
  chip, where it would crowd them as hosts are added. It opens a modal anchored below the top bar at
  the right and kept within the screen's height, titled "Unreadable session state · <n> files on <m> hosts": a table with one row per
  file — host, file name, reason with what was found beneath it, and the process (`claude · pid
  <pid>` with where it runs, or `unknown` when the file name carries no pid) — and a footer naming
  the fields panemux reads, a link to the steps to fix it in `docs/behavior/task-collection.md`, and *Copy
  details*, disabled where the clipboard is not available. The files are not cards on the board
  ([Unreadable state files](../behavior/task-collection.md#unreadable-state-files)).
## Editing hosts

The "Dashboard hosts" modal labels its source `ssh_connections in config.yaml` and explains that
hosts only in `~/.ssh/config` are usable in panes but not collected. Rows show name, destination
(`user@host:port` or "from ~/.ssh/config"), tags for SSH config/key/password/pane usage, Edit and
Delete. Below: Add host and Close. The inline add/edit form or delete confirmation hides those
buttons; Cancel returns to the list, Escape/outside click closes the dialog.

Fields: Name (SSH-config suggestions, read-only on edit), optional Host (empty fields use its SSH
block), User, Port, Key file, Known hosts file, Password. Password is empty; existing entries say
leave empty to keep, with Remove saved password disabling the field. Show client validation
beneath the form (missing/malformed/duplicate name, port outside 1–65535, or neither Host nor an
SSH block), and "Could not save: …" for server failures, preserving input.
Deletion asks inline in red; an in-use host without an SSH block is refused before asking, naming
panes; server-only dependency refusals say "Could not delete …". A green notice names successful
add/save/delete; the board collects again. [SSH behavior](../behavior/ssh.md) owns host configuration
and deletion constraints.
