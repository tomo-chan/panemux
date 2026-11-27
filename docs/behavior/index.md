# Behavior

Runtime behavior, task events, and task dashboard details (REST/WebSocket wire contracts live in
`docs/api/`; UI and frontend runtime contracts live in `docs/ui/`).

* [Behavior: the split preview](preview.md) - Split preview rendering, local and remote files, and preview lifecycle.
* [Behavior: SSH connections](ssh.md) - SSH connection configuration, session behavior, and remote host assumptions.
* [Behavior: task events](task-events.md) - Task event observation, lifecycle, and browser receiver responsibilities.
* [Task summaries: what they cost](task-summary-cost.md) - Dated measurements of task summary cost for synthetic conversations and selected models.
* [Behavior: task dashboard](tasks.md) - Task dashboard model and routes to collection, actions, summaries, and host tools.
* [Behavior: opening URLs from a pane](url-open.md) - Opening URLs from terminal panes and forwarding loopback login callbacks.

## Pane contracts

* [Pane Git and PR metadata](pane-metadata.md) - Pane worktree resolution, repository and PR links, polling, and cache behavior.

## Task contracts

* [Task collection and state](task-collection.md) - Task identity, host collection, state derivation, diagnostics, and agent availability.
* [Task metadata and records](task-records.md) - Repository and issue metadata, autolinks, and persistent done and label records.
* [Task launch and resume](task-launch.md) - Starting and resuming agent sessions, shell environments, and deferred labels.
* [Task summaries](task-summaries.md) - Summary scheduling, conversation excerpts, persistence, and agent-specific behavior.
* [Task navigation and terminals](task-navigation.md) - Task locations, pane navigation, temporary terminals, and dashboard view-state behavior.
* [Codex permission notifications](task-codex-permissions.md) - Permission hook installation, automatic updates, records, and answered waits.
* [Host tool management](host-tools.md) - Host tool update coordination and manual and automatic agmsg management.
