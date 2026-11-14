---
type: Decision
title: "Decision Log"
description: "Compatibility routes to decisions grouped by topic."
---

# Decision Log

See the [decision index](decisions/index.md) for topic listings.

## Release recovery uses the original source with GNU coreutils (2026-11-02, PR #479)

[Moved to release-source-recovery](decisions/maintenance/release-source-recovery.md).

## agmsg pin bump proposals (2026-10-31, issue #437)

[Moved to agmsg-pin-proposals](decisions/maintenance/agmsg-pin-proposals.md).

## Documentation structure

[Moved to development](decisions/development/index.md).

### The screenshots' fixed paths are links into the run's own root (2026-10-10, issue #328)

[Moved to screenshot-owned-roots](decisions/development/screenshot-owned-roots.md).

### The screenshot-helper tests choose where tmux's sockets go (2026-10-10, issue #343)

[Moved to screenshot-socket-paths](decisions/development/screenshot-socket-paths.md).

### Retaking the screenshots is checked by diff, not by pixels (2026-10-03, issue #285)

[Moved to screenshot-diff-check](decisions/development/screenshot-diff-check.md).

### Screenshots are generated, and the task dashboard's tasks are a fixture (2026-10-03, issue #285)

[Moved to screenshot-fixtures](decisions/development/screenshot-fixtures.md).

### Current-state guides and historical decisions are separate (2026-09-23)

[Moved to current-state-and-history](decisions/development/current-state-and-history.md).

## Architecture

[Moved to architecture](decisions/architecture/index.md).

### Domain config and file context use separate types (2026-09-20, issue #66, PR #240)

[Moved to domain-config-context](decisions/architecture/domain-config-context.md).

### Filesystem globals use named seams (2026-09-10 through 2026-09-13, PRs #224, #231, #234)

[Moved to filesystem-seams](decisions/architecture/filesystem-seams.md).

### Loopback callbacks use a panemux-host forward (2026-08-23, PR #177)

[Moved to loopback-callback-forward](decisions/architecture/loopback-callback-forward.md).

## Runtime and API contracts

[Moved to runtime](decisions/runtime/index.md).

### Browser interception in new remote tmux sessions (2026-10-09, issue #339)

[Moved to remote-tmux-browser-shim](decisions/runtime/remote-tmux-browser-shim.md).

### Command-center terminal frames carry non-fatal warnings (2026-09-11 through 2026-09-13, PRs #228 and #234)

[Moved to terminal-frame-warnings](decisions/runtime/terminal-frame-warnings.md).

### Layout responses are normalized before frontend parsing (2026-09-03, PR #198)

[Moved to layout-normalization](decisions/runtime/layout-normalization.md).

### Command-center prompts use a panemux-owned history entry (2026-08-17)

[Moved to prompt-history](decisions/runtime/prompt-history.md).

## Task dashboard

[Moved to tasks](decisions/tasks/index.md).

### A started agent runs through the user's login and interactive shell (2026-10-29, issue #445)

[Moved to login-shell-launch](decisions/tasks/login-shell-launch.md).

### Opening a host from its chip on the dashboard (2026-10-09, issue #314)

[Moved to host-chip-terminal](decisions/tasks/host-chip-terminal.md).

### Unreadable state files are diagnostics, not tasks (2026-10-09, issue #313)

[Moved to unreadable-state-diagnostics](decisions/tasks/unreadable-state-diagnostics.md).

### Working directory suggestions come from the board's tasks, stored nowhere (2026-10-09, issue #311)

[Moved to workdir-suggestions](decisions/tasks/workdir-suggestions.md).

### Labels used before come from the record file, as tags on `GET /api/tasks` (2026-10-07, issue #310)

[Moved to label-suggestions](decisions/tasks/label-suggestions.md).

### Input-wait notifications come from server-published task events (2026-10-02, issue #277)

[Moved to server-task-events](decisions/tasks/server-task-events.md).

### A lightweight collection and a stable wait signature for input-wait notifications (2026-10-02, issue #278)

[Moved to lightweight-wait-observation](decisions/tasks/lightweight-wait-observation.md).

### Type in pane: the popup over the board (2026-10-02, issue #284)

[Moved to task-terminal-popup](decisions/tasks/task-terminal-popup.md).

### The board's temporary tmux attach (2026-10-02, issue #283)

[Moved to temporary-task-attach](decisions/tasks/temporary-task-attach.md).

### Dashboard hosts are managed from the dashboard (2026-10-01, issue #272)

[Moved to dashboard-host-management](decisions/tasks/dashboard-host-management.md).

### Dashboard hosts are listed by name; details come from `~/.ssh/config` (2026-09-29, issue #272)

[Moved to named-dashboard-hosts](decisions/tasks/named-dashboard-hosts.md).

### Codex sessions: state, starting and resuming (2026-09-27, issue #264)

[Moved to codex-session-observation](decisions/tasks/codex-session-observation.md).

### Stage 1: a dashboard of agent sessions, independent of panes (2026-09-25, issue #252)

[Moved to session-dashboard](decisions/tasks/session-dashboard.md).

### Opening an agent outside tmux through `PANEMUX_PANE_ID` (2026-09-26, issue #254)

[Moved to pane-environment-identity](decisions/tasks/pane-environment-identity.md).

### Issue links and autolinked references (2026-09-26, issue #255)

[Moved to issue-autolinks](decisions/tasks/issue-autolinks.md).

### Reading `PANEMUX_PANE_ID` on macOS through `ps -E` (2026-09-27, issue #263)

[Moved to macos-pane-identity](decisions/tasks/macos-pane-identity.md).

### Stage 2: done and labels are recorded on the panemux host (2026-09-26, issue #256)

[Moved to task-records](decisions/tasks/task-records.md).

### Stage 2: starting and resuming claude tasks from the dashboard (2026-09-26, issue #257)

[Moved to task-launch-resume](decisions/tasks/task-launch-resume.md).

### Stage 3: summaries of a task's work and what is left (2026-09-26, issue #258)

[Moved to bounded-summary-input](decisions/tasks/bounded-summary-input.md).

### Summaries kept across restarts and reused by their input (2026-10-10, issue #352)

[Moved to summary-persistence](decisions/tasks/summary-persistence.md).

### Summaries cost less: a fixed system prompt, no tools and Haiku (2026-10-10, issue #353)

[Moved to summary-cost](decisions/tasks/summary-cost.md).

## Agent Board

[Moved to agent-board](decisions/agent-board/index.md).

### Pane-independent relay by source host (2026-11-07, #495 / #505)

[Moved to pane-independent relay](decisions/agent-board/pane-independent-relay.md).

### Live pane eligibility after configuration edits (2026-10-29, #435 / PR #447 review)

[Moved to live-pane-eligibility](decisions/agent-board/live-pane-eligibility.md).

### Keep source data separate from diagnostics (2026-10-31, #435 review round 2)

[Moved to source-output-separation](decisions/agent-board/source-output-separation.md).

### Scope automatic-update success to its host run (2026-10-31, #435 B3 review)

[Moved to update-result-lifetime](decisions/agent-board/update-result-lifetime.md).

### Automatic-update event delivery (2026-10-31, #435 B3 review)

[Moved to update-event-delivery](decisions/agent-board/update-event-delivery.md).

### Global agmsg automatic updates (2026-10-31, #435 B3)

[Moved to global-agmsg-updates](decisions/agent-board/global-agmsg-updates.md).

### Manual installer diagnostics and non-release versions (2026-10-31, #435 review)

[Moved to installer-diagnostics](decisions/agent-board/installer-diagnostics.md).

### Manual pinned agmsg installation (2026-10-30, #435 B2)

[Moved to pinned-agmsg-install](decisions/agent-board/pinned-agmsg-install.md).

### Split installer work into three PRs (2026-10-30, #435 / #438)

[Moved to installer-pr-sequence](decisions/agent-board/installer-pr-sequence.md).

### Global board participation and installer split (2026-10-29, #435 / #438)

[Moved to global-board-participation](decisions/agent-board/global-board-participation.md).

### Isolate the board message transport (2026-10-26, #438 / #441)

[Moved to message-transport-port](decisions/agent-board/message-transport-port.md).

### Host tool management and the Codex hook adapter (2026-10-26, #438 / #440)

[Moved to host-tool-port](decisions/agent-board/host-tool-port.md).

### One host identity and shared execution (2026-10-25, #438 / #439)

[Moved to shared-host-execution](decisions/agent-board/shared-host-execution.md).

### Compatibility is checked against a real agmsg release (2026-08-23, PR #176)

[Moved to real-agmsg-contract](decisions/agent-board/real-agmsg-contract.md).

### Contract incidents favor behavior over environment and wording (2026-08-23 onward, PR #176)

[Moved to behavioral-contract-assertions](decisions/agent-board/behavioral-contract-assertions.md).

### Version coverage normalizes install provenance (2026-08, PR #176)

[Moved to version-provenance](decisions/agent-board/version-provenance.md).

### Usage remains outside the Agent Board status contract (2026-08)

[Moved to usage-contract-boundary](decisions/agent-board/usage-contract-boundary.md).

### The own-send ledger is the model-checking pilot (2026-09, PR #243, issue #168)

[Moved to ledger-model-pilot](decisions/agent-board/ledger-model-pilot.md).

### Same-project panes claim an agmsg actas lock (2026-08-22, PR #171)

[Moved to same-project-identity-claim](decisions/agent-board/same-project-identity-claim.md).

### Dashboard capability is independent from command-center capability (2026-08-22, PR #171)

[Moved to independent-board-capability](decisions/agent-board/independent-board-capability.md).

### Command center pins its own execution context (2026-08-22, PR #171)

[Moved to command-center-context](decisions/agent-board/command-center-context.md).

### Browser clients obtain the board token from a dedicated bootstrap endpoint (2026-08-14, PR #170)

[Moved to board-token-bootstrap](decisions/agent-board/board-token-bootstrap.md).

### Command-center subprocess arguments are structurally separated (2026-08-14, PR #170)

[Moved to command-argument-separation](decisions/agent-board/command-argument-separation.md).

### Command center uses panemux's API, not agmsg scripts (2026-08, PR #162)

[Moved to command-center-api](decisions/agent-board/command-center-api.md).

### The own-send ledger authenticates sentinel-attributed rows (2026-08, PR #162)

[Moved to sentinel-send-ledger](decisions/agent-board/sentinel-send-ledger.md).

### Remote message bodies use an encoded wrapper argument (2026-08, PR #162)

[Moved to encoded-message-wrapper](decisions/agent-board/encoded-message-wrapper.md).

### Agent Board uses agmsg only (2026-08-08, PR #162)

[Moved to agmsg-only](decisions/agent-board/agmsg-only.md).

## Quality gateway

[Moved to quality-gateway](decisions/quality-gateway/index.md).

### golangci-lint caches inside each checkout (2026-10-09, issue #325)

[Moved to checkout-lint-cache](decisions/quality-gateway/checkout-lint-cache.md).

### Agent hooks check the checkout that holds the change (2026-10-11, issue #350)

[Moved to worktree-aware-hooks](decisions/quality-gateway/worktree-aware-hooks.md).

## WebSocket route separation and browser authority guard (#297, #298)

[Moved to websocket-authority](decisions/runtime/websocket-authority.md).

## Codex task summaries (#312)

[Moved to codex-summary-runner](decisions/tasks/codex-summary-runner.md).

## 2026-10-11: One browser authority boundary for APIs and WebSockets (#360)

[Moved to browser-authority-boundary](decisions/runtime/browser-authority-boundary.md).

## 2026-10-15: The split preview's isolation and bounds (#385)

[Moved to file-preview-isolation](decisions/preview/file-preview-isolation.md).

## 2026-10-16: Localhost pages in the split preview, and one URL opener (#386)

[Moved to localhost-preview](decisions/preview/localhost-preview.md).

## Agent registry

[Moved to agent-registry](decisions/agent-registry/index.md).

### One registry declares agents; capabilities are ports in their consumers (2026-10-12, issue #372)

[Moved to static-agent-registry](decisions/agent-registry/static-agent-registry.md).

### Collection is composed from per-agent collect adapters (2026-10-16, issue #373)

[Moved to collect-adapters](decisions/agent-registry/collect-adapters.md).

### Agent availability: a separate probe and an anti-corruption layer for formats (2026-10-16, issue #373)

[Moved to availability-probe](decisions/agent-registry/availability-probe.md).

### Launch and resume through per-agent adapters (2026-10-18, issue #374)

[Moved to launch-resume-adapters](decisions/agent-registry/launch-resume-adapters.md).

### Records keep agents this build does not list; Board joining through adapters (2026-10-18, issue #375)

[Record preservation](decisions/agent-registry/open-world-records.md) and
[board participation adapters](decisions/agent-registry/board-participation-adapters.md).

### Pane workdirs per session, on the Task rule (2026-10-18, issue #375)

[Moved to session-workdirs](decisions/agent-registry/session-workdirs.md).

### The dashboard on the agent capabilities (2026-10-23, issue #376)

[Moved to capability-driven-dashboard](decisions/agent-registry/capability-driven-dashboard.md).

### A third agent behind a build tag (2026-10-24, issue #377)

[Moved to tagged-fixture-agent](decisions/agent-registry/tagged-fixture-agent.md).

### Summary adapters (2026-10-24, issue #347)

[Moved to summary-adapters](decisions/agent-registry/summary-adapters.md).

### Capabilities in the task snapshot (2026-10-23, issue #376)

[Moved to snapshot-capabilities](decisions/agent-registry/snapshot-capabilities.md).

## 2026-10-17: Updating the codex permission hook by itself (#294, #402)

[Moved to codex-hook-updates](decisions/maintenance/codex-hook-updates.md).

## 2026-10-16: Installing the codex permission hook from the host menu (#294, #396)

[Moved to codex-hook-install](decisions/maintenance/codex-hook-install.md).

## 2026-10-15: Codex approval waits from a PermissionRequest hook (#294, #384)

[Moved to codex-permission-observation](decisions/maintenance/codex-permission-observation.md).

## 2026-10-16: Previewing an SSH host's files and localhost URLs (#387)

[Moved to remote-preview](decisions/preview/remote-preview.md).

## 2026-11-01: Mermaid inside isolated Markdown previews (#471)

[Moved to mermaid-runtime](decisions/preview/mermaid-runtime.md).

## 2026-11-02: Mermaid measurement and final-output sanitization (#477 review)

[Moved to mermaid-sanitization](decisions/preview/mermaid-sanitization.md).

### 2026-11-07: retire Agent Board PTY onboarding (#503, #495 S1)

[Moved to retire-pty-onboarding](decisions/agent-board/retire-pty-onboarding.md).
