# Runtime decisions

## Decisions

* [2026-10-11: One browser authority boundary for APIs and WebSockets (#360)](browser-authority-boundary.md) - Guard APIs before routing and preflight so other loopback origins cannot trigger side effects with simple requests.
* [Layout responses are normalized before frontend parsing (2026-09-03, PR #198)](layout-normalization.md) - Normalize hand-written layouts at the API boundary before strict parsing without forcing an immediate configuration save.
* [Command-center prompts use a panemux-owned history entry (2026-08-17)](prompt-history.md) - Write prompts explicitly because the CLI stream does not repeat the instruction that began each turn.
* [Browser interception in new remote tmux sessions (2026-10-09, issue #339)](remote-tmux-browser-shim.md) - Bootstrap interception in new remote tmux sessions without changing global environment or existing shells.
* [Command-center terminal frames carry non-fatal warnings (2026-09-11 through 2026-09-13, PRs #228 and #234)](terminal-frame-warnings.md) - Attach history warnings to the single terminal frame instead of violating stream completion semantics.
* [WebSocket route separation and browser authority guard (#297, #298)](websocket-authority.md) - Separate fixed streams from pane IDs and validate browser authority without trusting forwarded headers.
