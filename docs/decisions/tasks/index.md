# Tasks decisions

## Decisions

* [Stage 3: summaries of a task's work and what is left (2026-09-26, issue #258)](bounded-summary-input.md) - Send bounded recognized conversation text instead of raw tool output or unknown-format fallbacks.
* [Codex sessions: state, starting and resuming (2026-09-27, issue #264)](codex-session-observation.md) - Identify Codex sessions through open rollouts and turn records instead of guessing TUI ownership from directories.
* [Codex task summaries (#312)](codex-summary-runner.md) - Use Codex's authenticated configuration while requesting no tools without promising a tool-free runtime.
* [Dashboard hosts are managed from the dashboard (2026-10-01, issue #272)](dashboard-host-management.md) - Manage collection hosts separately from OpenSSH entries while protecting passwords, dependencies and atomic saves.
* [Opening a host from its chip on the dashboard (2026-10-09, issue #314)](host-chip-terminal.md) - Create a fresh host terminal through a destination menu instead of guessing which task session to attach.
* [Issue links and autolinked references (2026-09-26, issue #255)](issue-autolinks.md) - Use configured HTTPS templates and existing closing-issue lookups without tracker queries or stored credentials.
* [Labels used before come from the record file, as tags on `GET /api/tasks` (2026-10-07, issue #310)](label-suggestions.md) - Derive used-label tags from records instead of maintaining another list or endpoint that can drift.
* [A lightweight collection and a stable wait signature for input-wait notifications (2026-10-02, issue #278)](lightweight-wait-observation.md) - Share live-only collection and stable wait signatures so notification observation avoids full-dashboard work.
* [A started agent runs through the user's login and interactive shell (2026-10-29, issue #445)](login-shell-launch.md) - Read login and interactive startup settings while keeping prompts out of argv and preserving arguments across rc changes.
* [Reading `PANEMUX_PANE_ID` on macOS through `ps -E` (2026-09-27, issue #263)](macos-pane-identity.md) - Read constrained ps output with explicit ambiguity limits instead of adding a platform-specific environment helper.
* [Dashboard hosts are listed by name; details come from `~/.ssh/config` (2026-09-29, issue #272)](named-dashboard-hosts.md) - Resolve configured host names through OpenSSH without collecting every host or accepting injected directives.
* [Opening an agent outside tmux through `PANEMUX_PANE_ID` (2026-09-26, issue #254)](pane-environment-identity.md) - Locate detached and remote agents with a constrained environment marker instead of unavailable process ancestry.
* [Input-wait notifications come from server-published task events (2026-10-02, issue #277)](server-task-events.md) - Publish per-host changes, close slow subscribers and derive attention from healthy-host state instead of regex timing.
* [Stage 1: a dashboard of agent sessions, independent of panes (2026-09-25, issue #252)](session-dashboard.md) - Observe sessions independently of panes with bounded agent-owned data, explicit liveness and visible format failures.
* [Summaries cost less: a fixed system prompt, no tools and Haiku (2026-10-10, issue #353)](summary-cost.md) - Use fixed prompts, no offered tools and Haiku while exposing the cost of unexpected model fallback.
* [Summaries kept across restarts and reused by their input (2026-10-10, issue #352)](summary-persistence.md) - Persist excerpt-keyed answers to avoid paying again after restarts or tool-output-only log updates.
* [Stage 2: starting and resuming claude tasks from the dashboard (2026-09-26, issue #257)](task-launch-resume.md) - Launch via fixed stdin scripts and prompt files, and serialize resumes without destroying existing shell sessions.
* [Stage 2: done and labels are recorded on the panemux host (2026-09-26, issue #256)](task-records.md) - Separate records from configuration and observed state while protecting unreadable files and dotfile symlinks.
* [Type in pane: the popup over the board (2026-10-02, issue #284)](task-terminal-popup.md) - Reuse terminal behavior in a focused popup while leaving Escape and Tab with the active agent terminal.
* [The board's temporary tmux attach (2026-10-02, issue #283)](temporary-task-attach.md) - Use exact attach-only sessions with disconnect grace so reopen works without creating shells or leaking clients.
* [Unreadable state files are diagnostics, not tasks (2026-10-09, issue #313)](unreadable-state-diagnostics.md) - Report unreadable files as diagnostics and their live processes as tasks instead of unusable sessionless cards.
* [Working directory suggestions come from the board's tasks, stored nowhere (2026-10-09, issue #311)](workdir-suggestions.md) - Derive directory suggestions from collected host tasks instead of storing a second list or probing every path.
