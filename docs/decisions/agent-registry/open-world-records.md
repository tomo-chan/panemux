---
type: Decision
title: "Preserve records for agents this build does not list"
description: "Preserve unknown-agent records as original JSON without displaying them or blocking known-agent writes."
---

# Preserve records for agents this build does not list

Recorded 2026-10-18, issue #375 (stage D of #348).

Rejecting a whole record file for one newer agent blocked every write after a downgrade.
Keep unknown-agent entries as their original JSON so newer fields survive, but serve and accept
new records only for agents with the relevant capability. Malformed IDs still fail: accepting
those would admit files no build could write. Aliases are not normalized because keys could collide.
The summary cache follows its own capability. See [registry](../../agent-registry.md) for current rules.

The same change introduced [board participation adapters](board-participation-adapters.md),
a separate ownership choice later replaced when PTY onboarding was retired.
