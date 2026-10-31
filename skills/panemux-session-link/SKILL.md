---
name: panemux-session-link
description: Create panemux links for the current agent session, or specified sessions identified by host, agent and session_id. Use when asked to link back to sessions, including managing several sessions together.
---

Create a browser URL for an agent session using panemux's session-link query contract.
Default to the current session; when the user specifies other sessions, use their keys instead.
Do not publish links to an Issue, chat, or other external destination without authorization.

## Inputs

- **Base URL**: use an explicit URL supplied by the user, or `PANEMUX_BASE_URL` when configured
  in this agent's environment. Otherwise ask for the address they use in the browser.
  Panemux does not currently export this variable. Do not guess it from a server bind address,
  SSH hostname, default port, or forwarded headers. A URL on `localhost` refers to the machine
  opening the link; a remote agent may need a different API address from the user's browser URL.
- **Host**: the exact `ssh_connections` key; empty string means the panemux host. Obtain it from
  an explicit key or a task API result. For the current session, ask if its panemux host mapping
  is unknown. The machine hostname and `PANEMUX_PANE_ID` do not establish this mapping.
- **Agent / session ID**, for the current session:
  - Codex: canonical agent `codex`, session ID from `CODEX_THREAD_ID`.
  - Claude Code: canonical agent `claude`, current session ID substituted here by Claude Code:
    `${CLAUDE_SESSION_ID}`. This is a **skill string substitution**, not a promised shell
    environment variable. If it is still literal or empty, do not treat it as an ID: ask for
    the key or use a confirmed API match below. Do not create another Claude session to get it.
  - Other agents: obtain an explicitly confirmed session ID. Never use a pane ID, PID, tmux
    name, latest file, directory match, or another session's history as a substitute.

For explicitly specified sessions, accept all three key elements, including an explicitly empty
local host. Do not require the session to be running or to have a pane. A session ID is opaque,
not restricted to UUIDs. An ID still being assigned cannot be linked yet.

## Finding arbitrary sessions

If a key is incomplete, the existing `GET /api/tasks` can list candidates. Use a reachable panemux
API base URL confirmed by the user; it may differ from the browser base URL. A CLI HTTP client
without `Origin`, `Referer` or Fetch Metadata headers is allowed by panemux's request guard.
The core tasks API does not require the Agent Board bearer token. Do not copy a token into the
URL, weaken the browser guard, or change the server's access configuration. A deployment's proxy
may impose its own authentication; use only its authorized mechanism, otherwise ask the user.

Make only the requested lookup (not background polling), for example `curl --fail --silent
--show-error '<confirmed-api-base>/api/tasks'`. Do not invent or spoof browser headers to get
around a refusal. Inspect `hosts` as well as `tasks`: a host with `connecting` / `error` is not
proof a session is missing. Collection includes running sessions and only the stopped sessions
within its existing history limits. Distinguish a connection/refusal error from no match.

Use the returned `host`, canonical `agent`, and nonempty `session_id`. Show concise candidates
when several sessions match the user's description and ask which ones; do not silently pick one.
Do not print full task responses or session histories. A task with an unknown ID cannot supply a
stable key. Explicit complete keys can be formatted without contacting the API.

## Construct and return

Use a URL library, not string concatenation. Accept only an http/https browser base with a host
and no embedded credentials. Keep its origin and app pathname, discard existing query and
fragment, and set exactly these three parameters:

| Parameter | Value |
|---|---|
| `session_host` | Exact host key, empty for local |
| `session_agent` | Canonical ID matching `^[a-z][a-z0-9]*(-[a-z0-9]+)*$` |
| `session_id` | Nonempty session ID |

UTF-8 percent encode each value once (form encoding: space `+`, literal plus `%2B`). For example:

```text
https://panemux.example/?session_host=&session_agent=claude&session_id=7c21e0a4
```

Return the link, naming the session when returning several. Include no token, cwd, tmux name or
pane ID in the URL. Opening it follows Dashboard Open; a stopped session requires manual Resume.
A formatted link is not proof the session currently exists or is reachable. Do not open or Resume
the session just to construct a link.

Claude substitution contract: https://code.claude.com/docs/en/skills#available-string-substitutions
Full URL and collection contract: see `docs/behavior/tasks.md`, “Session links”, in the panemux
repository. This distributable skill works without a local copy of that document.
