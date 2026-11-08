---
type: Decision
title: "Issue links and autolinked references (2026-09-26, issue #255)"
description: "Use configured HTTPS templates and existing closing-issue lookups without tracker queries or stored credentials."
---

# Issue links and autolinked references (2026-09-26, issue #255)

Closing issue references reuse the existing PR lookup. Only unknown-field refusal retries the
older gh field set; retrying every failure doubles ordinary no-PR queries.
Configured prefix/template pairs replace a Jira-shaped regex that misread UTF-8, SHA-256 and CVEs.
References are links, never queried, so panemux needs no tracker credentials. HTTPS templates keep
identifiers after the authority. Go URL parsing accepts authorities browsers reject; conservative
validation and a shared Go/TS corpus protect the task list from one malformed link.
See [task behavior](../../behavior/tasks.md) for current matching and URL contracts.
