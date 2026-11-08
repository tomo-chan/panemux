---
type: Guide
title: "Behavior: task dashboard"
description: "Task dashboard model and routes to collection, actions, APIs, summaries, and host tools."
---

# Behavior: task dashboard

## Task Dashboard

The dashboard lists coding-agent sessions on the panemux host and every `ssh_connections` host,
independently of panes. A pane is the window used to watch or answer a task. A task's observed
state never says whether its work is done: done and labels are records a person sets.

Use the focused contracts below. [Task events](task-events.md) owns live state delivery and
receiver behavior; [UI design](../ui-design/task-dashboard.md) owns presentation. Command safety
is authoritative in the per-boundary concepts listed in the [security map](../security.md#document-map).

## Document map

- [Task collection and state](task-collection.md) — Task identity, host collection, state derivation, diagnostics, and agent availability.
- [Task metadata and records](task-records.md) — Repository and issue metadata, autolinks, and persistent done and label records.
- [Task launch and resume](task-launch.md) — Starting and resuming agent sessions, shell environments, and deferred labels.
- [Task summaries](task-summaries.md) — Summary scheduling, conversation excerpts, persistence, and agent-specific behavior.
- [Task navigation and terminals](task-navigation.md) — Task locations, pane navigation, temporary terminals, and dashboard view-state behavior.
- [Codex permission notifications](task-codex-permissions.md) — Permission hook installation, automatic updates, records, and answered waits.
- [Task and host terminal REST API](task-api.md) — Task and host terminal payloads, response fields, status codes, and failure contracts.
- [Host tool management](host-tools.md) — Host tool update coordination and manual and automatic agmsg management.

## Former sections

These section names preserve routes from earlier links; the linked concept owns the contract.

### What a task is
See [Task collection and state](task-collection.md#what-a-task-is).

### Hosts and connections
See [Task collection and state](task-collection.md#hosts-and-connections).

### Collection
See [Task collection and state](task-collection.md#collection).

### States
See [Task collection and state](task-collection.md#states).

### Unreadable state files
See [Task collection and state](task-collection.md#unreadable-state-files).

### Agent availability
See [Task collection and state](task-collection.md#agent-availability).

### Wait signature
See [Task collection and state](task-collection.md#wait-signature).

### Codex sessions
See [Task collection and state](task-collection.md#codex-sessions).

### Codex permission requests
See [Codex permission notifications](task-codex-permissions.md#codex-permission-requests).

### Where a task runs
See [Task navigation and terminals](task-navigation.md#where-a-task-runs).

### The pane of an agent outside tmux
See [Task navigation and terminals](task-navigation.md#the-pane-of-an-agent-outside-tmux).

### Repository, branch, pull request, issues and references
See [Task metadata and records](task-records.md#repository-branch-pull-request-issues-and-references).

### Done and labels
See [Task metadata and records](task-records.md#done-and-labels).

### Starting a task
See [Task launch and resume](task-launch.md#starting-a-task).

### Resuming a task
See [Task launch and resume](task-launch.md#resuming-a-task).

### Summaries
See [Task summaries](task-summaries.md#summaries).

### Saved summaries
See [Task summaries](task-summaries.md#saved-summaries).

### Codex summaries
See [Task summaries](task-summaries.md#codex-summaries).

### Opening a task
See [Task navigation and terminals](task-navigation.md#opening-a-task).

### Opening a host
See [Task navigation and terminals](task-navigation.md#opening-a-host).

### `GET /api/tasks`
See [Task and host terminal REST API](task-api.md#get-apitasks).

### `POST /api/tasks/hosts/{name}/reconnect`
See [Task and host terminal REST API](task-api.md#post-apitaskshostsnamereconnect).

### `PUT /api/tasks/records`
See [Task and host terminal REST API](task-api.md#put-apitasksrecords).

### `POST /api/tasks`
See [Task and host terminal REST API](task-api.md#post-apitasks).

### `POST /api/tasks/resume`
See [Task and host terminal REST API](task-api.md#post-apitasksresume).

### `POST /api/tasks/attach`
See [Task and host terminal REST API](task-api.md#post-apitasksattach).

### `POST /api/tasks/summary`
See [Task and host terminal REST API](task-api.md#post-apitaskssummary).

### `POST /api/tasks/input-given`
See [Task and host terminal REST API](task-api.md#post-apitasksinput-given).

### `GET` and `POST /api/tasks/codex-hook`, `POST /api/tasks/codex-hook/auto-update`
See [Task and host terminal REST API](task-api.md#get-and-post-apitaskscodex-hook-post-apitaskscodex-hookauto-update).

### `POST /api/hosts/session-name`
See [Task and host terminal REST API](task-api.md#post-apihostssession-name).

### `POST /api/hosts/terminal`
See [Task and host terminal REST API](task-api.md#post-apihoststerminal).

### Manual agmsg host tool
See [Host tool management](host-tools.md#manual-agmsg-host-tool).

### Global agmsg automatic updates
See [Host tool management](host-tools.md#global-agmsg-automatic-updates).
