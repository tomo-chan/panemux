---
type: Decision
title: "Dashboard hosts are managed from the dashboard (2026-10-01, issue #272)"
description: "Manage collection hosts separately from OpenSSH entries while protecting passwords, dependencies and atomic saves."
---

# Dashboard hosts are managed from the dashboard (2026-10-01, issue #272)

Dashboard hosts come from ssh_connections; Add SSH Host writes OpenSSH configuration. Separate
management makes that distinction visible. Passwords never round-trip: has_password, keep-on-empty
and explicit clear avoid leaking secrets or confusing a mask with a real password.
Entries replace fields rather than merge, except passwords. Renaming was rejected because it would
require atomic reference rewrites across panes. Deletion checks effective resolution including
ProxyJump dependencies, not names alone. Edits invalidate in-flight dials as well as saved connections.
Review found a map-only lock let layout saves persist rolled-back host changes. One configuration
lock covers snapshots through writes and readers. Existing hand-written names stay editable;
invalid saved ports must not make the list unreadable.
See [SSH connection management API](../../behavior/rest-api.md#apiconfigssh-connections) and [SSH security](../../security/task-collection.md#collection-and-metadata-execution).
