# Decision Log

This log records *why* a consequential design or implementation choice was made, what it replaced,
and the sequence in which related choices changed. The rest of `docs/` describes the current state.

Newest entries appear first within each topic. Dates and pull requests are included when repository
history identifies them. A decision remains here after its consequences are reflected in the
current specification; superseded decisions are retained and labelled as such.

## Documentation structure

### Current-state guides and historical decisions are separate (2026-09-23)

Top-level topic documents had accumulated three different kinds of information: current rules,
deep implementation detail, and a narrative of issues and rollout phases. Readers looking for the
current contract had to infer it from phrases such as "landed", "earlier revision", and "Phase 3".

The documentation now has three reading levels: [overview](overview.md), concise topic guides, and
same-topic deep dives. This log owns chronology, rejected alternatives, and migration reasoning.
The structure and writing rules are indexed in the [documentation index](README.md).

## Architecture

### Domain config and file context use separate types (2026-09-20, issue #66, PR #240)

`config.Data` is the serializable domain model and preserves user-facing YAML section order.
`config.Config` embeds it and adds load/save context such as file and SSH-config paths. The earlier
single type mixed persisted data with runtime context; a separate serialization struct then had to
repeat every config section and silently dropped a section when that copy was not updated.

### Filesystem globals use named seams (2026-09-10 through 2026-09-13, PRs #224, #231, #234)

Home-directory, cache-directory, and persistent-write behavior had been overridden independently by
callers or manipulated through process-wide environment variables in tests. The repository adopted
`internal/homedir`, `internal/cachedir`, and `internal/fileops` as one named seam per global
capability. The separation is deliberate: home and cache lookup have different platform semantics,
and atomic file replacement has different safety properties from either lookup.

These seams make failure paths injectable and give lint a single allowed location for direct OS
lookups. They are not safe for parallel substitution; tests that override them remain non-parallel.

### Loopback callbacks use a panemux-host forward (2026-08-23, PR #177)

CLI login flows in remote panes open `localhost` callback URLs on the SSH host, while the browser
runs on the panemux host. Panemux chose an on-demand local listener backed by SSH `direct-tcpip`
instead of exposing a general proxy or requiring users to preconfigure `ssh -L`. The scope remains
loopback-only and the listener exists only for the callback flow.

## Runtime and API contracts

### Command-center terminal frames carry non-fatal warnings (2026-09-11 through 2026-09-13, PRs #228 and #234)

A failed history write was first represented as `error` followed by `done`, violating the protocol's
one-terminal-frame rule; limiting the warning to successful turns then hid it whenever the query
also failed. `warnings` now rides whichever single terminal frame ends the turn. Malformed stream
output cancels the subprocess before its remaining output is drained, preventing the drain from
holding the busy flag and client error until the full timeout.

### Layout responses are normalized before frontend parsing (2026-09-03, PR #198)

Valid hand-written config can omit `direction`/`children` or put a lone pane at the layout root,
while the browser contract requires a normalized recursive node. The API now fills missing keys,
relocates a lone root pane into its implied child, and validates that pane exactly like every other
pane. The normalized form is persisted only on the next explicit save.

### Command-center prompts use a panemux-owned history entry (2026-08-17)

The Claude stream contains `stream_event`, `system`, `assistant`, and `result` frames but does not
repeat the prompt that produced them. Deriving history from the captured stream alone therefore
made each turn unreadable. Panemux now writes the prompt first as `panemux_prompt`, a type the CLI
does not emit, then appends the captured subprocess frames. A subprocess failure still records the
prompt; a request that never starts a subprocess does not create a turn.

## Task dashboard

### Stage 1: a dashboard of agent sessions, independent of panes (2026-09-25, issue #252)

The design, its stages and its open questions are agreed in issue #252. The choices stage 1 made
while being built:

- **Claude Code's own files are read as they are.** `~/.claude/sessions/<pid>.json` and
  `~/.claude/projects/*/<session>.jsonl` are not a published format. They are the only record of a
  session that does not depend on panemux having started it, which is the point of the feature, so
  the dependency is accepted and made to fail visibly: every field is optional, an unreadable state
  file stays on the board as `unknown` instead of disappearing, and only the first `"cwd"` of a log
  is read. Codex has no equivalent that has been checked, so codex tasks are running processes and
  nothing more (issue #252, open question 5).
- **Liveness is "the pid is alive and its program is claude", and `procStart` is not used.** A
  state file's pid alone is not enough, because pids restart after a host reboot. Claude
  Code 2.1.282's `procStart` matched field 22 of `/proc/<pid>/stat` (clock ticks since boot) for the
  one process checked on Linux, while the observation recorded in the issue did not match (425
  against 419), so how it is computed is still unknown and nothing relies on it. "Its program" is a
  path component named `claude` or `claude-code` in argv[0], or in the script `node`/`bun`/`deno`
  runs, because how Claude Code shows in `ps` depends on how it was installed — a native binary
  under `…/claude/versions/<version>`, or `node` running the npm package. The first version matched
  `claude` anywhere in the command line; review found that an editor or `tail` with a file under
  `~/.claude` as its argument then passed for a waiting agent.
- **A running claude process with no state file is `unknown`, not `stop`** (review of PR #253).
  Building running tasks only from state files meant a Claude Code release that moved those files
  would have shown every running agent as stopped. Such a process is taken to be writing the newest
  log in its directory, so that session is not listed twice.
- **Codex's interactive sessions are recognized by subcommand**, from `codex --help` of codex-cli
  0.157.0: no subcommand (a prompt), `resume` and `fork` are interactive, every other subcommand is
  not. The first version excluded any process with an `exec` token anywhere, which hid a prompt
  containing the word and listed `codex app-server`.
- **Only the collecting user's processes are listed** (`ps -U`), so on a shared host another
  user's agents are not shown as one's own tasks.
- **A slow collection keeps its connection** while the connection answers an SSH keepalive. The
  first version dropped the connection on any timeout, so a host whose collection took longer than
  15 seconds was redialed every 10 seconds and never showed a result. Keeping the connection stops
  the redialing only: a host whose script alone takes longer than 15 seconds every time still never
  shows a result, and its script is started again at every collection. What the remote script does
  once panemux closes the exec channel (no signal is sent) has not been checked.
- **The task routes refuse cross-site requests.** They stay unauthenticated like the rest of
  `/api/*`, but `GET /api/tasks` is the first GET with a heavy side effect — dialing every host — so
  a page on another site must not be able to trigger it with an `<img>`.
- **A stopped task reports its repository but not its branch or PR**: git metadata is read from the
  directory as it is now, which says nothing about the branch a stopped session worked on. So
  `gh pr view` runs only for a directory a running task uses (review of PR #253); the first version
  ran it for every directory and threw the stopped-only results away. A lookup a request abandoned
  is not cached, so an aborted `gh pr view` does not hide a running task's PR for 30 seconds.
- **The local collection is killed as a process group** (review of PR #253). The first version
  relied on `exec.CommandContext` killing `sh` alone; a probe the script had started kept its
  stdout open, so the collection outlived its 15 seconds until that probe finished.
- **`/resume` and exit were checked on a real machine** (macOS, 2026-09-26; the Claude Code
  version was not recorded). `/resume` keeps the same process and the
  same `<pid>.json`, and rewrites that file's `sessionId` to the resumed session (`updatedAt`
  moves with it) — so the switched-from session shows as stopped and the switched-to one as running
  in the same place, as issue #252 expected. `/exit` removes both `<pid>.json` and its `.key` file,
  so a normal exit leaves no state file behind; the liveness check still guards against the files a
  crash or a host restart can leave.
- **One fixed script per host, fed on stdin.** Every host runs the same constant script through
  `sh -s`: one round trip per collection, identical parsing for local and remote, no remote value
  ever quoted into a command, and nothing that depends on the remote login shell being POSIX. The
  alternative — one exec per probe, as the pane header's lookups do — would cost several round trips
  per host every 10 seconds.
- **The pane a task belongs to is found in the browser, not by the API.** The browser already holds
  the workspaces, so a pane the dashboard has just created is matched at once rather than after the
  next collection, and the API has no layout state to keep consistent with it.
- **Stopped sessions are the last 7 days, at most 50 per host.** Decided for stage 1 while it was
  built; conversation logs are never deleted, so without a bound every past session would be
  listed. Open question 1 still decides retention and recording for stage 2.
- **panemux opens on the workspaces**, as before, rather than on the dashboard the issue's mockup
  starts on.
- **The layers switch with `Cmd/Ctrl+Shift+S`, and the letter is configurable** as
  `display.task_dashboard_shortcut` (open question 6). `S` was chosen for stage 1 while it was
  built; the environment it was built in could not reach a list of Chrome's own shortcuts. On a real,
  non-headless browser on macOS (2026-09-26), `Cmd+Shift+S` switched layers and was not taken by the
  browser; `Ctrl+Shift+S` on Linux or Windows was not checked there. It is a config setting rather
  than a per-browser one so every browser on one panemux behaves the same; `K` and `B` are refused
  because the palette and the board already use them. `GET /api/display` reports the effective
  letter so the default lives in one place, and an unset value is never written back.
- **The UI text is English**, like the rest of panemux's interface, although the issue's mockup is
  written in Japanese.

### Issue and Jira links (2026-09-26, issue #255)

- **An issue is one the task's pull request closes**, read from `gh pr view --json
  closingIssuesReferences` in the same `gh` call that already found the pull request, so the
  dashboard runs no extra command and still looks up only directories a running task uses. That
  the field exists and what it exports (`id`, `number`, `url`, and the repository's `name` and
  owner `login`; the query asks for the first 100) was checked in the source of gh 2.101.0
  (`api/export_pr.go`, `api/query_builder.go`), not against a real pull request: the environment it
  was built in had no working GitHub token. Whether an issue linked by hand in the pull request's
  sidebar, rather than by a closing keyword, is included has not been checked either. Issue titles
  are not shown, since that export carries none.
- **An older `gh` keeps the PR link** (review of PR #261). `closingIssuesReferences` first appears
  in gh 2.72.0 (it is absent from `api/query_builder.go` at v2.71.2), and gh refuses a whole call
  that names a field it does not know, before contacting GitHub. The first version therefore lost
  the task's PR link on an older `gh` while the pane header, which asks for `url,number` only,
  still showed it. A call refused with `Unknown JSON field` is now repeated with `url,number`. Only
  that refusal is retried: a branch without a pull request also makes `gh` fail, and is the common
  case, so a retry on any failure would double the `gh` runs.
- **A Jira key is `[A-Z][A-Z0-9_]+-[1-9][0-9]*` with no ASCII letter or digit touching it**, found
  in the branch name and then the pull request title. Keys are case-sensitive, so a lower-case
  branch such as `pay-418-retry` gives none; the form still matches words such as `UTF-8` or
  `SHA-256` in a title, and cuts `CVE-2024-45337` to `CVE-2024`. Every key found is linked, not
  just the first.
- **`task_dashboard.jira_projects` limits keys to the listed projects** (review of PR #261). The
  false positives above come from the key shape alone. A project allowlist removes them without
  calling Jira, so it keeps the "linked, never queried" rule. The alternative considered was to
  drop a key followed by `-<digit>`: it would have removed only the `CVE-2024` case, left `UTF-8`
  and `SHA-256`, and hidden a real key in a branch such as `PAY-418-2-retry`. The list is optional,
  and without it every key of the right shape is linked as before.
- **Jira is linked, never queried.** `task_dashboard.jira_url` names the site and a key becomes
  `<jira_url>/browse/<key>`; no Jira API is called and no ticket title is fetched, so panemux holds
  no Jira credentials. The setting is a new top-level `task_dashboard` section rather than part of
  `display`, since it says where links point rather than how anything looks. It must be an `https`
  URL, because it becomes the address of a link the operator clicks.
- **The Jira site's host and port are held to what the browser parses** (review of PR #261).
  `url.Parse` accepted `https://jira.example.invalid:99999`, `https://ex<ample.com` and
  `https://xn--/`, which the browser's `new URL()` refuses; one such link made the browser reject
  the whole task list. Go has no WHATWG URL parser, so the check is narrower than the browser
  instead of equal to it: an IP or ASCII letters, digits and `-`, no punycode label (there is no
  decoder in the module to check one with), no numeric last label unless the host is an IPv4
  address, and a port 1–65535. `testdata/jira-url-validation.json` is read by the Go test and by
  the frontend's schema test, so a site Go accepts that the browser does not fails a test.

## Agent Board

### Compatibility is checked against a real agmsg release (2026-08-23, PR #176)

Fixture tests could not detect false assumptions about `watch.sh` output or the shape of event-log
message IDs. A second contract tier now installs the pinned agmsg version in CI and runs daily
against the latest release. Its first live run found that treating IDs as numbers prevented the
relay cursor from advancing; the current cursor contract treats IDs as opaque ordered values from
the returned stream.

The design borrows Pact's consumer-owned contract principle, but not Pact tooling: agmsg is a CLI
dependency without an HTTP/message provider verification loop or broker. Direct Go tests against
the installed scripts verify the actual boundary with less machinery.

### Contract incidents favor behavior over environment and wording (2026-08-23 onward, PR #176)

The live contract first asserted a `watch.sh` diagnostic that is absent on its successful path; it
now observes message delivery. Its first CI run also depended on finding an agent in the test
process ancestry, so locks looked live locally and stale on runners. Tests now set agmsg's documented
`AGMSG_AGENT_PID` override and verify that a dead owner can be replaced.

The scheduled canary later failed when agmsg v1.3.1 changed a diagnostic sentence while preserving
the behavior Agent Board needs. The assertion was narrowed to the stable fact that the dropped pair
is named. The canary remains daily because the measured median between 22 releases from v1.0.2
through v1.2.2 was 2.9 days; a weekly sample could span several releases and obscure the cause.

### Version coverage normalizes install provenance (2026-08, PR #176)

An installed `VERSION` may be a bare release, a `v`-prefixed tag, or `git describe` provenance.
Literal comparison to the tested pin falsely warned on supported installs. Panemux now normalizes
those forms and treats later patches on the tested major/minor line as covered. This is a warning-noise
policy, not a semver promise; the real-install contract remains authoritative. Patch tolerance also
avoids forcing pin churn at agmsg's observed multi-day release cadence.

### Usage remains outside the Agent Board status contract (2026-08)

Account-wide usage belongs to the agent provider. Per-pane usage may still be derived by summing the
documented `usage` field already stored in each pane's transcript: that is direct field reading, not
inference from private state. If added, it belongs to panemux's existing pane-inspection surface,
not the cooperative agmsg status schema.

### The own-send ledger is the model-checking pilot (2026-09, PR #243, issue #168)

The ledger was chosen before `Relay.processRow` because it is the smallest self-contained state
machine and had already suffered a multiset-versus-set bug. It established the TLA+, generated
transition graph, and Go conformance tiers at low cost. `Relay.processRow` is the intended next
model; dynamic executor selection is a search-and-retry algorithm better suited to fuzz or property
tests.

### Same-project panes claim an agmsg actas lock (2026-08-22, PR #171)

agmsg's default watcher resolves every identity registered for one project and agent type. Two
Claude panes in the same directory therefore subscribed to each other's messages even though their
board identities were distinct. Bootstrap now follows agmsg's own template: join, claim the pane ID
against the Claude session ID, then run `watch.sh` with the pane ID as its narrowing argument. A
held lock is reported instead of disturbing the owning watcher.

### Dashboard capability is independent from command-center capability (2026-08-22, PR #171)

The dashboard can be enabled without the command center and vice versa. A single
`command_center_enabled` flag could not represent both cases, so `/api/session-token` also returns
`agent_board_enabled`. The dashboard remains read-only; broadcast composition stays in the command
center/API surface.

### Command center pins its own execution context (2026-08-22, PR #171)

Live CLI verification showed that plain `claude -p` can report the ambient interactive session ID,
and inherited settings can broaden an `--allowedTools` policy. The command center now mints and
persists its own session ID, disables ambient setting sources, uses an empty per-query directory,
passes its instructions explicitly, restricts MCP configuration, and combines an allowlist with an
explicit acting-tool denylist. The current security contract and evidence are in
[security/command-center.md](security/command-center.md).

### Browser clients obtain the board token from a dedicated bootstrap endpoint (2026-08-14, PR #170)

The original command-center design covered how its subprocess received a bearer token but not how
the bundled frontend learned a randomly generated token. `GET /api/session-token` was added as the
bootstrap contract. It is deliberately outside the authenticated `/api/board/*` subtree and is
limited to the local/trusted deployment model documented in [security/auth.md](security/auth.md).

### Command-center subprocess arguments are structurally separated (2026-08-14, PR #170)

Adversarial testing against the real `claude` CLI found two independent argv defects: a
dash-prefixed prompt could be parsed as a flag, while ordinary prompts failed because of variadic
flag placement. The runner now constructs a fixed option segment, uses the CLI's option terminator,
and keeps the prompt in the operand position. Exact argv-shape tests are intentionally retained as
a security contract. See [security/command-center.md](security/command-center.md).

### Command center uses panemux's API, not agmsg scripts (2026-08, PR #162)

Direct script execution would have made the LLM responsible for shell-safe composition and would
have limited it to one host's agmsg state. A narrow MCP server exposes only board status, message
history, and broadcast, backed by panemux's authenticated REST API. `internal/board` remains the
only package that invokes agmsg scripts.

### The own-send ledger authenticates sentinel-attributed rows (2026-08, PR #162)

`send.sh --force` does not authenticate the free-text `from` value, so trusting every row from the
reserved `_system` identity allowed impersonation. Panemux now accepts such a row only when it
matches a recent send recorded by panemux itself. Model checking later formalized this multiset
behavior; see [agent-board/model-checking.md](agent-board/model-checking.md).

### Remote message bodies use an encoded wrapper argument (2026-08, PR #162)

agmsg's `send.sh` accepts the body as a positional argument and has no stdin write contract. Panemux
therefore base64-encodes the body, allowlists the encoded alphabet, quotes every
`RunBoardCommand` argument, and uses a fixed remote wrapper to decode immediately before `send.sh`.
Team and agent identifiers use a narrower direct allowlist. The security argument and its explicit
CodeQL-verification caveat live in [security/agent-board.md](security/agent-board.md).

### Agent Board uses agmsg only (2026-08-08, PR #162)

Supersedes an early dual-backend design with a panemux-owned SQLite protocol for Claude panes.
Maintaining two protocols added no capability: agmsg already supplied the messaging contract and
interoperability with multiple agent types. Panemux therefore owns relay/cache behavior but no
message schema or database.

Claude Code's native cross-session messaging was also evaluated and rejected: it is Claude-only,
does not fit arbitrary SSH hosts, and exposes no documented process API that panemux can call.

## Quality gateway

The quality gateway has a denser numbered record. [quality-gateway/decisions.md](quality-gateway/decisions.md)
contains decisions D1–D12 in decision order, including evidence and rejected alternatives. The
rollout sequence is retained there rather than in the current [quality-gateway guide](quality-gateway.md).

Key milestones were:

| Date | Change | Decision |
|---|---|---|
| 2026-08-28 | Real-router tests, wider coverage scope, agent hooks, red-check, and scenario checks | D1–D6 |
| 2026-08-30 | Diff-scoped block coverage and mutation measurement | D7–D8 |
| 2026-09-03 | Go-produced contract fixtures and accessibility ceilings | D10–D11 |
| 2026-09-15 | Mutation findings became a blocking gate | D9 |
| 2026-09-21 | TLA+ transition export plus Go conformance replay | D12 |
