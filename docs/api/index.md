# API

HTTP REST and WebSocket wire contracts between the Go server, browser frontend, and local clients,
grouped by domain.

## HTTP and WebSocket contracts

* [Workspace REST and Terminal WebSocket API](workspace.md) - Core workspace, session, SSH config, split preview, and URL-open REST routes, plus the terminal WebSocket protocol.
* [Task Dashboard REST and Event Stream API](tasks.md) - Task and host terminal REST routes, Codex hook endpoints, and the `/ws/tasks/events` wire protocol.
* [Agent Board REST and Command Center WebSocket API](agent-board.md) - Session-token bootstrap, authenticated `/api/board/*` REST routes, and the `/ws/board/command` WebSocket protocol.
