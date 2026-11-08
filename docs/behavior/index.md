# Behavior

Runtime behavior, API contracts, task events, and task dashboard details.

* [Behavior: Agent Board REST API](board-api.md) - Agent Board REST payloads, authorization, status codes, and operations.
* [Behavior: frontend runtime behavior](frontend.md) - Browser initialization, workspace interaction, and frontend runtime behavior.
* [Behavior: agent attention notifications](notifications.md) - Agent attention notifications and their relationship to task state and browser visibility.
* [Behavior: the split preview](preview.md) - Split preview rendering, local and remote files, and preview lifecycle.
* [Behavior: REST API](rest-api.md) - REST endpoints, request and response contracts, and failure behavior.
* [Behavior: SSH connections](ssh.md) - SSH connection configuration, session behavior, and remote host assumptions.
* [タスクイベント（日本語訳）](task-events.ja.md) - タスクイベントの観測・配信と受け手の契約を説明する英語正本の日本語訳。
* [Behavior: task events](task-events.md) - Task event observation, WebSocket delivery, and receiver responsibilities.
* [Task summaries: what they cost](task-summary-cost.md) - Dated measurements of task summary cost for synthetic conversations and selected models.
* [Behavior: task dashboard](tasks.md) - Task dashboard model and routes to collection, actions, APIs, summaries, and host tools.
* [Behavior: opening URLs from a pane](url-open.md) - Opening URLs from terminal panes and forwarding loopback login callbacks.
* [Behavior: WebSocket protocols](websocket.md) - WebSocket connection policy, terminal frames, and board and task event protocols.

## Task contracts

* [Task collection and state](task-collection.md) - Task identity, host collection, state derivation, diagnostics, and agent availability.
* [Task metadata and records](task-records.md) - Repository and issue metadata, autolinks, and persistent done and label records.
* [Task launch and resume](task-launch.md) - Starting and resuming agent sessions, shell environments, and deferred labels.
* [Task summaries](task-summaries.md) - Summary scheduling, conversation excerpts, persistence, and agent-specific behavior.
* [Task navigation and terminals](task-navigation.md) - Task locations, pane navigation, temporary terminals, and dashboard view-state behavior.
* [Codex permission notifications](task-codex-permissions.md) - Permission hook installation, automatic updates, records, and answered waits.
* [Task and host terminal REST API](task-api.md) - Task and host terminal payloads, response fields, status codes, and failure contracts.
* [Host tool management](host-tools.md) - Host tool update coordination and manual and automatic agmsg management.
