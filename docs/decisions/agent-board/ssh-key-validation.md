---
type: Decision
title: Validate SSH connection keys at load and API boundaries
description: Share the BoardID key alphabet with configuration validation and reject unaddressable keys before startup.
---

# Validate SSH connection keys at load and API boundaries (2026-11-15, #495 / #505 / PR #540)

The user chose one alphabet for `ssh_connections` keys and BoardID keys: non-empty Unicode
letters, combining marks, numbers and `._-+`, with exact spelling preserved. A single validator
in `internal/host` supplies configuration loading, API creation/editing and BoardID parsing.
The frontend displays the server's character-validation error instead of maintaining another
alphabet.

Previously, handwritten configuration could load keys containing spaces, `@` and other
characters refused by the API's ASCII-only creation rule. An interim relay change excluded
such hosts and warned. That fallback is removed: a disallowed key now fails configuration
loading and prevents startup, even when Agent Board is disabled. Errors quote the offending
key and state the permitted characters. API writes enforce the same rule.

This is an intentional compatibility change. Qualified addresses `name@ssh:<key>` must be
safe and unambiguous for every configured host; a host that could receive a pane message but
could not form a valid reply address would silently lose replies. Rename unsupported keys and
pane connection references before startup; update the matching OpenSSH alias too when a
name-only entry depends on it. Unicode and plus-sign keys remain supported, as verified with
public agmsg send/history calls.

The current contract is in [SSH connections](../../behavior/ssh.md#defining-connections-in-ssh_connections),
[relay](../../agent-board/relay.md) and [Agent Board security](../../security/agent-board.md).
