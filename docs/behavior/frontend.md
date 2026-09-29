# Behavior: frontend runtime behavior

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## Frontend Runtime Behavior

### Initial page load

```text
Browser loads SPA
  -> GET /api/layout
  -> GET /api/display
  -> validate JSON with Zod
  -> render recursive split tree
  -> each TerminalPane opens /ws/{sessionID}
```

### Terminal I/O round trip

```text
User types in xterm.js
  -> browser sends binary WebSocket frame
  -> session.Write(...)
  -> shell/SSH/tmux produces output
  -> session.Read(...)
  -> backend sends binary WebSocket frame
  -> xterm.js writes bytes to the terminal
```

During replayed reconnect output, xterm stdin is suppressed until the replay end marker is fully
applied, so terminal query responses embedded in old output are not regenerated as fresh shell
input.

When an SSH-backed pane loses its transport unexpectedly, panemux classifies that session as
`disconnected` instead of `exited`. The frontend performs one automatic recovery attempt by
recreating the session and reconnecting the pane. Deliberate remote shell termination such as
`exit` remains `exited` and shows the restart action instead of auto-reconnecting.

The same one-shot automatic recovery also triggers if the pane's WebSocket connection itself
repeatedly fails to (re)establish and exhausts its own reconnect attempt budget, even if the
backend never reported a `disconnected` status frame (e.g. during a prolonged network outage that
prevents the WebSocket handshake from completing at all). Either way, if the automatic recovery
attempt itself fails, the pane shows the manual "Reconnect Session" action instead of the
"reconnecting..." indicator forever.

### Selection and copy behavior

- Terminal text can be selected with the mouse using xterm.js standard selection behavior.
- If text is selected, `Cmd+C` or `Ctrl+C` copies the current selection instead of sending terminal input.
- If no text is selected, `Cmd+C` or `Ctrl+C` is left to normal terminal behavior, so shell interrupts still work.
- This interaction is currently validated in Chrome.

### Terminal link detection

- `http://` and `https://` URLs printed into a pane are auto-detected and become clickable through panemux's own xterm.js link provider (`createUrlLinkProvider` in `frontend/src/utils/terminalLinks.ts`).
- It replaced `@xterm/addon-web-links`, which joined rows only on the terminal's own `isWrapped` flag and accepted a cut-off fragment such as `https://exam` as a whole URL (issue [#175](https://github.com/tomo-chan/panemux/issues/175)). Neither is extensible from outside the addon.
- The default URL pattern only excludes ASCII punctuation, so panemux supplies its own pattern (`TERMINAL_URL_REGEX` in `frontend/src/hooks/useTerminal.ts`) that also excludes CJK and fullwidth punctuation. Trailing `。`, `、`, `・`, `…` and enclosing `（）`, `「」`, `【】`, `“”` are not part of the detected link.
- Non-ASCII *letters* are never excluded, so raw IRIs such as `https://ja.wikipedia.org/wiki/日本語` stay linkable in full. Fullwidth digits and fullwidth letters stay linkable for the same reason, as do the letters and numerals interleaved into the CJK symbols block itself (`々`, `〆`, `〇` and the ideographic numerals), so `https://ja.wikipedia.org/wiki/日々` is linked whole.
- Known limitation: kana or kanji that directly follows a URL with no delimiter (for example `https://example.com/docsを参照`) is still absorbed into the link. That case cannot be distinguished from a legitimate kana IRI path by pattern matching alone. Separate the URL with whitespace or punctuation, or emit an OSC 8 hyperlink — xterm.js resolves those itself, independently of this pattern, and its built-in handler asks for confirmation before navigating.
- A URL that does not fit the pane is linked whole, across every row it occupies, whether the terminal wrapped it or the program printed its own newline at the pane edge. The second case is the common one in a bordered TUI, in formatted CLI output, and wherever tmux redraws by line — none of which set `isWrapped`.
- Two rows are read as one line when the terminal wrapped them, or when the upper row runs all the way to the pane edge and the lower row starts with something other than a blank. A row whose last column holds the trailing half of a wide character counts as reaching the edge, since the character itself occupies it. That heuristic can join two unrelated rows that happen to meet both conditions; the alternative is a fragment that stays clickable, and a URL cut inside its hostname opens a host the operator never saw.
- When adjacent rows share Unicode box-drawing characters at the same left and right cell columns, those decorations and one optional inner padding cell per side are removed before applying the same continuation rule. The left and right decorations may differ from each other, and inner padding is not required. The characters and cell columns must match between rows; ASCII `|` is never treated as a frame. This keeps ordinary piped output out of the heuristic while allowing URLs wrapped inside TUI frames such as `│ https://… │` to remain one link.
- A match that runs to the pane edge of the last row with nothing continuing it is not offered as a link at all: whether the URL ended there or the rest is off screen is unknowable, and half a URL is worse than none.
- Known limitation: a URL broken well short of the pane edge — a program that prints `https://exam\r\nple.com/path` on an 80-column pane — is still linked as the fragment. A line that stops 60 columns early is indistinguishable from a line that simply ended there, so neither joining nor suppressing is safe.
- `#<number>` references are linked separately to the pane's GitHub pull request (see [Pane Git and PR metadata](#pane-git-and-pr-metadata)). The URL provider is registered first, so a `#123` inside a URL stays part of the URL — which requires both providers to report ranges in the same coordinate system (1-based on both axes, and mapped through cells rather than string indices), since xterm resolves the overlap by comparing the ranges themselves.

### Resize and layout updates

- `ResizeObserver` triggers terminal fit logic when pane size changes.
- The browser sends a `resize` control message with current cols/rows.
- Dragging split dividers updates layout percentages in memory.
- Divider resize remains available during normal terminal use.
- Layout persistence for resize is debounced by 500 ms before `PUT /api/workspaces/{active}/layout`.

### Layout editing and pane movement

There is no separate edit mode. Layout editing is part of the normal interface.

- Pane split, close, maximize, settings, new terminal creation, workspace add/rename/delete, and pane move are all available from the standard UI.
- Terminal keyboard input remains active during normal use because drag initiation is restricted to the pane header handle.
- Layout and workspace mutations are persisted immediately to the active workspace config.
- The workspace bar is always visible, even when only one workspace exists, so workspace actions remain discoverable.

Drag-and-drop pane movement works like this:

1. User presses or drags the `⠿` handle in the pane header.
2. The source pane enters a drag state: it fades, scales down slightly, and the cursor switches to `grabbing`.
3. Workspace edges, pane targets, and divider targets become active drop targets.
4. If the pointer is over another pane, the nearest pane edge is resolved from pointer position and the corresponding half-pane preview is shown.
5. Releasing on a workspace edge moves the pane there and creates a new outer split.
6. Releasing on a pane edge inserts the dragged pane beside that target pane.
7. Releasing on a divider inserts the dragged pane relative to the adjacent subtree boundary.
8. `dragSourcePaneId` is cleared and the updated layout is persisted.

Pane movement is a re-layout operation, not a session recreation:

- moving a pane does not create a new backend session
- moving a pane does not restart the session
- only the pane's position in the layout tree changes

When a pane moves to a different parent node, the component may be remounted by React. xterm.js terminal instances survive remounting via a module-level `TerminalEntry` map keyed by session ID; the existing canvas is reattached to the new container with `appendChild` rather than `replaceChildren`, preserving React-managed sibling nodes such as overlays and restart controls.

### Split and close semantics

- Splitting a pane creates a new local pane, creates a backend session through `POST /api/sessions`, then rewrites the layout tree so the original and new panes each receive `50%` under a new split node.
- The original pane keeps its current visible terminal contents when split; it must not go blank or reset to a fresh prompt while the new sibling pane is created.
- Closing a pane calls `DELETE /api/sessions/{id}`, removes the pane from the tree, collapses parents with a single child, and renormalizes sizes to total `100`.
- Moving a pane calls the workspace layout save path only; it does not call session create or delete APIs.
- Dropping a pane on another workspace tab moves it into that workspace, inserts it at the destination workspace's right edge, persists both affected workspace layouts, and switches the active workspace to the destination.
- Dragging a pane card from the workspace summary view onto another workspace uses the same move path as dragging from the pane-header handle.

### New terminal creation

- The pane header exposes one-click `Add new pane to the right` and `Add new pane below` actions that immediately create a default `local` pane beside the current pane.
- The dialog supports two bases:
  - blank `local`
  - clone an existing pane's settings
- Before creation, the user can choose placement:
  - workspace `top`, `bottom`, `left`, or `right`
  - beside an existing pane on `top`, `bottom`, `left`, or `right`
- Creation flow:
  1. frontend builds the new `PaneConfig`
  2. frontend calls `POST /api/sessions`
  3. frontend inserts the pane into the active workspace layout
  4. frontend persists with `PUT /api/workspaces/{active}/layout`
- For cloned `tmux` and `ssh_tmux` panes, `tmux_session` is regenerated to avoid collisions.

### Pane Git and PR metadata

- The pane header shows Git metadata when the pane's current working context resolves to a Git repository.
- The displayed Git context is always resolved from the pane's current live work context, not from stale historical output, subject to the 30-second server-side response cache described above.
- For normal panes, the base context is the pane's current working directory.
- For local tmux and SSH+tmux panes, the base context is the currently active tmux pane only.
- When a local, local tmux, SSH, or SSH+tmux pane has an active interactive `codex` or `claude` process working in a different Git worktree for the same repository, panemux prefers that agent worktree over the pane's base directory.
- Only interactive agents are eligible for worktree override.
- Non-interactive commands such as `codex exec`, `claude -p`, and `claude --print` must not affect the displayed Git or PR metadata.
- For interactive Codex flows across all four pane types (`local`, `ssh`, `tmux`, and `ssh_tmux`), panemux may derive the active worktree from the Codex session log when the process has that log open.
- Codex log resolution currently prefers the most recent `response_item.payload.arguments.workdir` from `exec_command` tool calls, then falls back to `turn_context.cwd`, then `session_meta.cwd`.
- This ordering exists because the interactive Codex process and its thread metadata may stay on the original pane directory even while tool calls are executing inside a sibling Git worktree.
- If future Codex versions change their session-log schema or start updating `turn_context.cwd` to the active worktree reliably, compare the three fields above before changing panemux's resolver order.
- For interactive Claude flows across all four pane types, panemux may derive the active worktree from `~/.claude/sessions/<pid>.json` plus the matching transcript under `~/.claude/projects/...`.
- Claude session metadata is used only to identify the matching transcript (`sessionId`) and project directory key.
- The project directory key is derived by replacing every path separator and every dot in the session's `cwd` with a dash (`/workspace/user/my.project` → `-workspace-user-my-project`). This is observed behavior of Claude Code's own storage layout, not a documented interface; `TestClaudeProjectDirName_ObservedEncoding` in `internal/session` writes the mapping out case by case so what panemux believes is reviewable.
- Because that rule can change with a Claude release, a local transcript that is not at the derived path is looked for with a bounded fallback: one listing of `~/.claude/projects` and at most one check per project directory, one level deep, first match in name order. The miss is logged once per session — naming both the derived path and the one used — so an encoding change is visible in the log rather than showing up only as a pane that quietly stopped reporting its worktree. A session with no transcript anywhere stays silent and behaves exactly as before.
- Remote (`ssh`, `ssh_tmux`) panes fall back too, since a pane reached over SSH loses its worktree exactly the same way. The shape differs because each remote step costs a round trip: the derived directory is read first and, only if nothing under it yields a working directory, the host is asked once — `ls -1 ~/.claude/projects/*/<sessionId>.jsonl 2>/dev/null | head -n 1`, one level down, one line back. A pane whose derived path works therefore issues exactly the commands it always did.
- That answer is remembered per host and session for 30 seconds, so a host whose encoding differs pays for one probe rather than one per metadata refresh — and so does a session that has written no transcript yet, which is indistinguishable from here. It expires because of the order things happen in: the session metadata exists as soon as the remote `claude` process does, but its transcript only appears once that session produces output, so the first refresh after a pane opens often lands in that window. Against the dashboard's 10-second git-info poll that is at most one probe per three refreshes, and a transcript that appears later is picked up within one expiry. A transcript that moves is found again the same way.
- A probe that *failed* — a dropped exec channel, a connection that went away mid-refresh — is not remembered at all. It is not the host answering "there is nothing here", and recording it as one would retire the fallback on a network blip.
- The probe resolves the project *directory*, not just the parent transcript, so the session's subagent transcripts are found under it too. When it answers with the directory panemux already looked under — which happens whenever nothing was resolved for an unrelated reason, such as a transcript that has no working directory in it yet — that is not an encoding change and is not reported as one.
- The path the host reports is checked against the same regex allowlist every other remote path goes through before it can reach a command, and it must name this session's own transcript. See [security.md](../security.md)'s "Remote path arguments".
- Claude transcript resolution prefers the latest `Bash` tool `cd ... &&` target, then the latest top-level `cwd` recorded on transcript entries such as `user`, `assistant`, `attachment`, and `system`, then the latest non-auxiliary tool file path (`Read`/`Edit`/`Write`/etc) or file-history snapshot path.
- The `Bash` `cd` target is checked first because the top-level `cwd` field reflects the interactive Claude process's own OS-level working directory, fixed at launch and never updated for that process's lifetime — it does not track directories a Bash tool call actually `cd`'d into. A real Claude Code transcript has a non-empty top-level `cwd` on nearly every record, so preferring it over the `Bash` `cd` target made that detection unreachable in practice, permanently masking sibling-worktree divergence reached via a plain `cd` (this was reproduced directly against a real transcript, independent of any `/resume` involvement). This mirrors the same reasoning already applied to Codex's `workdir` precedence above.
- A tool file-touch path (`Read`/`Edit`/`Write`/etc) remains a weaker signal than the top-level `cwd`, since touching a single unrelated file elsewhere does not by itself indicate the agent moved its active work there.
- Claude Code also records delegated Task subagent activity in separate transcript files under `<sessionId>/subagents/*.jsonl`, next to the parent transcript. A subagent that does worktree-relative work there never updates the parent transcript's own `cwd`, so panemux additionally reads every subagent transcript file and resolves each one with the same rule above, independently of the parent.
- panemux does not apply any recency or time-window filter when considering subagent transcripts: every distinct worktree signaled by the parent transcript or any subagent transcript for the current session is a candidate, regardless of how long ago that transcript file was last written.
- This resolver intentionally does not depend on Claude `hooks` or `statusLine` configuration, so no extra Claude-side setup is required for pane-header Git or PR detection.
- If the agent exits, no longer has an eligible worktree, or the resolved worktree is not a sibling worktree of the pane's current repository, panemux falls back to the pane's own working directory immediately.
- For SSH panes, panemux resolves interactive agent processes from the remote process list on the current SSH connection.
- For SSH+tmux panes, panemux resolves interactive agent processes only from the currently active remote tmux pane.
- If the resolved repository origin can be converted into a browser URL, the header shows the repository name as a link to that repository page.
- For SCP-style SSH origins such as `git@alias:owner/repo.git`, panemux uses `~/.ssh/config` `Host` aliases to resolve both the browser link hostname and GitHub PR lookup repo host when a matching alias exists; otherwise it treats the SSH host token as the hostname directly.
- If the resolved branch has a GitHub pull request, the header shows a PR link labeled `#<number>`.
- When the active agent (including its subagents) has diverged into more than one distinct sibling worktree of the same repository, panemux shows all of them instead of just one, deduplicated by the worktree's repository root; the pane's own base directory is shown only when nothing has diverged from it. Each distinct worktree gets its own independent GitHub PR lookup, so more than one PR link can be shown at once. See [ui-design.md](../ui-design.md) for how the header presents more than one worktree.
- The "last known worktree" sticky behavior applies per distinct worktree: if the active-workdir lookup transiently fails or returns nothing, panemux keeps showing the previously resolved set of worktrees until a subsequent lookup confirms they are no longer valid (e.g. the branch changed or the worktree was removed).
