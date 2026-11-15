---
type: Decision
title: "Remote message bodies use an encoded wrapper argument (2026-08, PR #162)"
description: "Encode remote message bodies across the shell boundary because agmsg only accepts positional body arguments."
---

# Remote message bodies use an encoded wrapper argument (2026-08, PR #162)

agmsg's `send.sh` accepts the body as a positional argument and has no stdin write contract. Panemux
therefore base64-encodes the body, allowlists the encoded alphabet, quotes every
`RunBoardCommand` argument, and uses a fixed remote wrapper to decode immediately before `send.sh`.
Team and agent identifiers use a narrower direct allowlist. The security argument and its explicit
CodeQL-verification caveat live in [security/agent-board.md](../../security/agent-board.md).

The [2026-11-15 review decision](qualified-forwarded-senders.md) updates the original positional-only
assumption: pinned agmsg supports `--body -`, so decoded text now goes through stdin to preserve
option-like bodies. The encoded positional argument across the shell boundary is unchanged.
