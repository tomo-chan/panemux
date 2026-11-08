---
type: Decision
title: "2026-10-16: Installing the codex permission hook from the host menu (#294, #396)"
description: "Rationale for 2026-10-16: Installing the codex permission hook from the host menu."
---

# 2026-10-16: Installing the codex permission hook from the host menu (#294, #396)

Stage 3 of #294 replaces copying the hook and editing `~/.codex/hooks.json` by hand with an
`Install` / `Update` item in each host's menu. The panemux host's chip, which opened nothing, opens a
menu with that item alone.

- **Content decides, not versions.** A hook that differs from the one this panemux embeds shows
  `Update`; there is no version number, no downgrade guard, no lock between two panemux instances and
  no crash journal. Each would answer a failure no one has met yet.
- **Read in Go, written by a fixed script.** `hooks.json` is merged in Go, from the file the check
  read, and written back through a quoted heredoc, because a host may have no `jq` and the shell
  cannot merge JSON. A file that is not the shape codex reads is reported and left alone rather than
  repaired.
- **Trust stays with the person.** panemux says to trust the hook in codex's `/hooks` and neither
  writes nor checks codex's trust.
- **No codex is not the same as not checked.** A host that could not be checked says so; hiding the
  item there would read as a host without codex.

Current contracts: [maintenance guide](../../maintenance.md).
