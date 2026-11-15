---
type: Decision
title: "Observed identities extend the pane response contract"
description: "Keep pane compatibility while exposing qualified names and task observation sources without additional polling."
---

# Observed identities extend the pane response contract

On 2026-11-15, #516 b1 retained the pane-keyed status and participation maps and added
required `identities` keyed by name and BoardID. The frontend schema requires the field;
mock responses are updated rather than silently defaulting a missing contract to an empty list.
The [REST specification](../../behavior/board-api.md#identity-entries) owns the payload.

Task observations take priority only under the existing name/pane correspondence and an
unambiguous live location match. Existing task and task-event collection results are retained
in the API; status reads add no host I/O or polling. Failure clears that host’s priority, and
multiple live matches fall back instead of choosing a session arbitrarily. The collection time
makes retained observations visible as observations rather than promises of freshness.

History-only names last only as long as their existing bounded rows. No independent name
cache, roster dependency, or new authorization rule is introduced. Self-reports keep their
existing lifetime and remain separately labelled when task state takes priority.

History retains source host/from/to without a new resolved-recipient field. Consequently,
past unqualified pane destinations cannot always be reconstructed after pane ownership changes.
This accepts the existing history boundary instead of expanding the relay snapshot contract.
