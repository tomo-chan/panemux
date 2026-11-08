---
type: Decision
title: "Agent hooks check the checkout that holds the change (2026-10-11, issue #350)"
description: "Rationale for Agent hooks check the checkout that holds the change."
---

# Agent hooks check the checkout that holds the change (2026-10-11, issue #350)

Hooks can run from main while edits belong to a worktree. Using the hook path vetted the wrong
module; git top-level/prefix selects the edited checkout, resolving symlinks and requiring the same
common git directory. Unrelated repositories get formatting only, avoiding arbitrary tsc execution
or rejection of their toolchains.
The Stop hook also mistook the worktree .git file for a directory. Streaming gofmt removes that
fault and temporary-allocation dependence. Branch-owned hook delegation was rejected: checks
must not change with the reviewed branch, though its module/dependencies are used.
See [development](../../../DEVELOPMENT.md) for current hook and completion requirements.
