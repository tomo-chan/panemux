---
type: Decision
title: "The screenshots' fixed paths are links into the run's own root (2026-10-10, issue #328)"
description: "Keep screenshot markers under the run root and use fixed symlinks that tolerate temporary-file cleanup safely."
---

# The screenshots' fixed paths are links into the run's own root (2026-10-10, issue #328)

`/tmp/sample-project` was a real directory the run claimed through a marker file inside it. On
2026-10-08 a run stopped because the directory was still there with its directories but none of
its files, marker included; `/tmp/panemux-screenshots-agmsg` was found the same way. macOS's
`com.apple.tmp_cleaner` (`/usr/libexec/tmp_cleaner`, daily at 00:00) deletes files under `/tmp`
whose access, modification and change times are all more than three days old, and empty
directories more than three days old; a directory a file was just deleted from is newly modified,
so the directories outlast their files. The marker is never read after it is written, so it went
while a file read since stayed. As long as the marker lived at the fixed path, losing it alone
stopped every later run until the developer moved the directory away. The fixed paths became symbolic links into
`$TMPDIR/panemux-screenshots`, so the marker that decides what may be emptied sits only at the
run's root. A directory holding nothing but empty directories is removed as left over, with
`rmdir`, rather than refused: nothing a developer keeps can be lost that way. The root itself,
under the developer's own `$TMPDIR`, is cleaned the same way by `com.apple.bsd.dirhelper`, so it is
also claimed without the marker when every entry in it is a name the run creates there. A fresh
`mktemp` root per run, which would need no claiming at all, was not chosen: its longer name puts
the tmux socket past macOS's 104-byte limit under the default `$TMPDIR`, and the capture's teardown
would have to find a root that changes every run. Recognising a link
the run made by its target's name, `<...>/panemux-screenshots/<the link's own name>`, was chosen
over recording its target in a file, which could be lost the same way; replacing a link never
touches what it points to.

Current contracts: [development guide](../../index.md).
