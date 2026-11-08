---
type: Decision
title: "Remote message bodies use an encoded wrapper argument (2026-08, PR #162)"
description: "Rationale for Remote message bodies use an encoded wrapper argument."
---

# Remote message bodies use an encoded wrapper argument (2026-08, PR #162)

agmsg's `send.sh` accepts the body as a positional argument and has no stdin write contract. Panemux
therefore base64-encodes the body, allowlists the encoded alphabet, quotes every
`RunBoardCommand` argument, and uses a fixed remote wrapper to decode immediately before `send.sh`.
Team and agent identifiers use a narrower direct allowlist. The security argument and its explicit
CodeQL-verification caveat live in [security/agent-board.md](../../security/agent-board.md).
