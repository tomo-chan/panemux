---
type: Specification
title: "Pane Git and PR metadata"
description: "Pane worktree resolution, repository and PR links, polling, and cache behavior."
---

# Pane Git and PR metadata

## Worktree resolution

- The pane header shows Git metadata when the pane's current working context resolves to a Git repository.
- The displayed Git context is always resolved from the pane's current live work context, not from stale historical output, subject to the 30-second server-side response cache described below.
- For normal panes, the base context is the pane's current working directory.
- For local tmux and SSH+tmux panes, the base context is the currently active tmux pane only.
- When a local, local tmux, SSH, or SSH+tmux pane has an active interactive `codex` or `claude` process working in a different Git worktree for the same repository, panemux prefers that agent worktree over the pane's base directory.
- Only interactive agents are eligible for worktree override. The pane's agent is the newest descendant of its shell that is an interactive session by the task board's own Task rule ([Detection rules](../agent-registry.md#detection-rules)), among the agents that declare `workdir`: Claude Code run as a native binary, under a `claude/versions/<version>` directory, as `claude-code`, or by `node`, `bun` or `deno`, and `codex` whose first positional argument is not a non-interactive subcommand.
- Non-interactive commands must not affect the displayed Git or PR metadata: `claude -p` and `claude --print`, and `codex` with a non-interactive subcommand however its options precede it (`codex exec`, `codex -m <model> exec`, `codex -c key=value exec`, `codex e`, `codex review`, `codex login`, `codex mcp-server`, `codex app-server`, `codex apply`). An `exec` that is not codex's subcommand — the value of an option (`codex -p exec`, a profile named exec; `codex -i a.png exec`, since `-i/--image` takes every argument up to the next option) or a word of the prompt — does not make the session non-interactive.
- The agent's own working directories are resolved per session, not per pane: `internal/session`'s Workdir capability finds the session the agent process runs (`locate`, giving its session key, PID, recorded cwd and log) and then every working directory of that session, its subagents' included (`workdirs`). Only when that resolves nothing does the pane fall back to the newest descendant process cwd that differs from the pane's base directory; the fallback is the pane's own.
- For interactive Codex flows across all four pane types (`local`, `ssh`, `tmux`, and `ssh_tmux`), panemux may derive the active worktree from the Codex session log when the process has that log open.
- Codex log resolution currently prefers the most recent `response_item.payload.arguments.workdir` from `exec_command` tool calls, then falls back to `turn_context.cwd`, then `session_meta.cwd`.
- This ordering exists because the interactive Codex process and its thread metadata may stay on the original pane directory even while tool calls are executing inside a sibling Git worktree.
- If future Codex versions change their session-log schema or start updating `turn_context.cwd` to the active worktree reliably, compare the three fields above before changing panemux's resolver order.
- For interactive Claude flows across all four pane types, panemux may derive the active worktree from `~/.claude/sessions/<pid>.json` plus the matching transcript under `~/.claude/projects/...`.
- Claude session metadata is used only to identify the matching transcript (`sessionId`) and project directory key. It is read by the same parser as the task board's ([Unreadable state files](task-collection.md#unreadable-state-files)), keys in their exact case. The first file whose `pid` is the agent process's decides: when its `sessionId` is not valid or it has no `cwd`, the pane falls back as below rather than looking for another file naming the same pid. A file whose `cwd` or `sessionId` is of another type (`"cwd": 5`) is passed over for the next one.
- The project directory key is derived by replacing every path separator and every dot in the session's `cwd` with a dash (`/workspace/user/my.project` → `-workspace-user-my-project`). This is observed behavior of Claude Code's own storage layout, not a documented interface; `TestClaudeProjectDirName_ObservedEncoding` in `internal/session` writes the mapping out case by case so what panemux believes is reviewable.
- Because that rule can change with a Claude release, a local transcript that is not at the derived path is looked for with a bounded fallback: one listing of `~/.claude/projects` and at most one check per project directory, one level deep, first match in name order. The miss is logged once per session — naming both the derived path and the one used — so an encoding change is visible in the log rather than showing up only as a pane that quietly stopped reporting its worktree. A session with no transcript anywhere stays silent and behaves exactly as before.
- Remote (`ssh`, `ssh_tmux`) panes read the derived directory first. Only if nothing under it yields a working directory do they probe one level down for the first matching transcript: `ls -1 ~/.claude/projects/*/<sessionId>.jsonl 2>/dev/null | head -n 1`. A working derived path needs no fallback probe.
- The remote project-directory probe result, including no transcript, is cached per host and session for 30 seconds. A transcript can appear after session metadata or move later, so expiry allows it to be found on a later refresh (at most one probe per three 10-second polls).
- A probe that *failed* — a dropped exec channel, a connection that went away mid-refresh — is not remembered at all. It is not the host answering "there is nothing here", and recording it as one would retire the fallback on a network blip.
- The probe resolves the project *directory*, not just the parent transcript, so the session's subagent transcripts are found under it too. When it answers with the directory panemux already looked under — which happens whenever nothing was resolved for an unrelated reason, such as a transcript that has no working directory in it yet — that is not an encoding change and is not reported as one.
- The path the host reports is checked against the same regex allowlist every other remote path goes through before it can reach a command, and it must name this session's own transcript. See [remote path requirements](../security/ssh-execution.md#remote-path-arguments-ssh-working-directory).
- Claude transcript resolution prefers the latest `Bash` tool `cd ... &&` target, then the latest top-level `cwd` recorded on transcript entries such as `user`, `assistant`, `attachment`, and `system`, then the latest non-auxiliary tool file path (`Read`/`Edit`/`Write`/etc) or file-history snapshot path.
- The top-level Claude `cwd` reflects the process's launch directory, so it is weaker than a `Bash` `cd` target.
- A tool file-touch path (`Read`/`Edit`/`Write`/etc) remains a weaker signal than the top-level `cwd`, since touching a single unrelated file elsewhere does not by itself indicate the agent moved its active work there.
- Claude Code also records delegated Task subagent activity in separate transcript files under `<sessionId>/subagents/*.jsonl`, next to the parent transcript. A subagent that does worktree-relative work there never updates the parent transcript's own `cwd`, so panemux additionally reads every subagent transcript file and resolves each one with the same rule above, independently of the parent.
- panemux does not apply any recency or time-window filter when considering subagent transcripts: every distinct worktree signaled by the parent transcript or any subagent transcript for the current session is a candidate, regardless of how long ago that transcript file was last written.
- This resolver intentionally does not depend on Claude `hooks` or `statusLine` configuration, so no extra Claude-side setup is required for pane-header Git or PR detection.
- When the active-workdir lookup fails or returns no candidates (including after the agent exits), panemux rechecks the last preferred worktrees against the pane's current repository. Valid distinct sibling worktrees remain preferred; if none remains valid, it clears the preference and uses the pane's own directory. A lookup returning candidates replaces that set; candidates outside the repository or at its base root do not override it.
- For SSH panes, panemux resolves interactive agent processes from the remote process list on the current SSH connection.
- For SSH+tmux panes, panemux resolves interactive agent processes only from the currently active remote tmux pane.
- If the resolved repository origin can be converted into a browser URL, the header shows the repository name as a link to that repository page.
- For SCP-style SSH origins such as `git@alias:owner/repo.git`, panemux uses `~/.ssh/config` `Host` aliases to resolve both the browser link hostname and GitHub PR lookup repo host when a matching alias exists; otherwise it treats the SSH host token as the hostname directly.
- If the resolved branch has a GitHub pull request, the header shows a PR link labeled `#<number>`.
- When the active agent (including its subagents) has diverged into more than one distinct sibling worktree of the same repository, panemux shows all of them instead of just one, deduplicated by the worktree's repository root; the pane's own base directory is shown only when nothing has diverged from it. Each distinct worktree gets its own independent GitHub PR lookup, so more than one PR link can be shown at once. See [Pane Header](../ui-design/panes.md#multiple-worktrees) for its presentation.

## Polling and caching

For pane-header Git status, local and local tmux panes inspect the local filesystem, while `ssh`
and `ssh_tmux` panes run the equivalent Git inspection on the remote host. This allows headers to
show branch/repository info even when the repository exists only on the SSH target.

Workspace tab summaries and their integrated pane groups use the same Git metadata source and also
poll `/api/sessions` to summarize pane connection state across every workspace, including inactive
ones.

Pane-header Git and PR metadata is fetched immediately when a pane becomes visible, then refreshed
every 10 seconds only while both the browser tab and that pane remain visible.

The backend caches each session's git-info response for 30 seconds so that this steady-state polling
(and any other concurrent viewer of the same session, such as another browser tab) does not repeat
process/transcript scanning, remote git inspection over SSH, or `gh pr view` lookups on every request.
A pane's displayed Git/PR metadata may therefore lag the true state by up to 30 seconds; the cache is
cleared whenever a session is deleted or recreated so a new session never inherits another session's
cached response. Explicit refresh triggers — clicking or focusing a pane, restoring the browser tab,
or opening VS Code — only skip the frontend's own request if it already has a response no older than
10 seconds, matching the steady-state poll interval, so an explicit refresh is bounded by that 10
seconds plus the server-side cache above rather than compounding a larger, independent client-side
window on top of it.

Workspace-summary session-state and Git metadata polling are frontend-only and best-effort. While
the browser tab is visible, the frontend polls every 10 seconds so it can summarize all known
panes across all workspaces without mounting hidden terminal instances. The integrated summary view
marks the currently focused pane so the overview stays aligned with the live terminal focus state.
For `top` and `bottom` workspace bars, pane cards are shown in a hover/focus overlay anchored to
the workspace tab. For `left` and `right` workspace bars, pane cards stay expanded inline beneath
their workspace tab. In vertical mode, the workspace tabs and inline cards scroll inside the bar,
while the `+` action and tab-position controls stay pinned at the bottom.

When a pane is hidden behind maximize, its header Git/PR polling stops until it becomes visible
again. Restoring the browser tab or making a pane visible again triggers an immediate refresh so
newly created PR links become clickable without waiting for the next steady-state poll.

The VS Code open action uses the same preferred worktree resolution as the header; when several
worktrees diverge, it uses the first.

To avoid repeatedly transferring and parsing unchanged interactive-agent logs, panemux reuses the
last parsed Codex or Claude worktree result while the underlying session-log or transcript file
fingerprint remains unchanged. For SSH-backed panes this check uses remote file metadata first and
only reads the full `jsonl` contents again when the fingerprint changes.

Remote Git inspection currently depends on `git rev-parse --path-format=absolute --git-common-dir`
on the SSH target, which requires Git 2.31 or newer. Older remote Git versions degrade to "no git
info" in the pane header for SSH-backed panes.
