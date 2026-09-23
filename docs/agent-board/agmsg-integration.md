# Agent Board: integration with agmsg

> Part of the current [Agent Board design](../agent-board.md).

## Integration with agmsg

Reads and writes go through two different scripts under agmsg's `scripts/` directory, verified
against agmsg's source (not inferred). panemux never reads or writes agmsg's `messages.db` or
`teams/*/config.json` directly, matching agmsg's own README guidance that those are internal.

**Reads — `scripts/api.sh`, read-only.** Its `case "$VERB"` block implements only `get`:

| Call | Returns |
|---|---|
| `api.sh get teams` | `{"name": "<team>"}` per line, one per team under `teams/` |
| `api.sh get teams <team> members` | `{"name","types","project"}` per line |
| `api.sh get teams <team> messages [--agent <name>] [--limit N] [--before-id <id>]` | `{"type":"message_sent","id","team","from","to","body","at"}` per line, JSONL, oldest-first |

`id` is returned as a string (agmsg's own future-proofing against a non-integer ID scheme, per its
source comments) — panemux treats it as opaque; the current event-log driver emits UUIDv7 values.
Only `--limit` and `--before-id` are validated as plain digits; **`--agent`
is not** — it is a free-text name protected only by agmsg's own internal `_agmsg_sqlesc` SQL
escaping, not by any argument-shape check. Panemux cannot skip its own shell-escaping on reads on
the theory that
agmsg has already validated the arguments for it. Unlike agmsg's own human-facing
`inbox.sh`/`check-inbox.sh` (which mark whatever they display as read), `api.sh` never writes
`read_at` — panemux's dashboard/relay polling through it cannot cause a joined agent to miss a
message its own inbox/`Monitor` delivery would otherwise have shown it.

**There is no forward/"since" read.** `--before-id` selects `id < X` — a backwards pagination
cursor, the opposite of what an incremental poll needs — and `api.sh` has no after/since option at
all. The relay therefore does not use `--before-id` for its poll loop. Each poll calls
`api.sh get teams <team>
messages --limit <N>` with **no** `--before-id`, taking the `N` most recent rows (`N` defaults to a
few hundred; see [Package layout](architecture.md#package-layout)), and filters client-side to the rows that
*follow* the persisted cursor **in the order `api.sh` itself returned them**. **This has a real,
accepted truncation risk:** if more than `N` genuinely new rows land on one host between two poll
cycles, the oldest of that overflow are silently skipped — there is no way to detect or recover
them through `api.sh`'s documented interface.

**The cursor is matched by identity, never compared.** agmsg's own `api.sh` states the rule: its
message `id` column is TEXT because "the driver-interface spec treats every
message id as opaque", and its rows are ordered by each storage source's native counter
(`events.seq` / `messages.id`), a value the same comment says is "never compared across sources",
while one response can `UNION` both. Ordering by the response's own order is therefore the only
signal agmsg offers. Lexicographic comparison does not satisfy this contract either:
messages written in the same millisecond share their whole UUIDv7 time prefix and differ only in
random bits, so sorting them as strings reorders them (the Tier 1 fixtures capture exactly such a
set, and a test asserts that they do). Two cases treat every returned row as new: no cursor yet
(cold start), and a cursor absent from the response — which means it scrolled out of the window, and
costs one bounded re-delivery of that window, consistent with the at-least-once delivery this design
already accepts, rather than a silent skip. `--before-id` remains useful for one thing only: paging *backwards* through
older history on demand (e.g. a "load more" action in the dashboard's history view), never for the
relay's forward poll.

**Writes — `scripts/send.sh`, not `api.sh`.** Signature: `send.sh <team> <from> <to> <body>
[--force]`. Unlike `api.sh`, `send.sh` takes `body` as a **positional shell argument**, not stdin —
there is no stdin-based write path in agmsg to delegate to. **Both `from` and `to` are checked
against that team's roster unless `--force` is passed**. Every message in [Status
self-report](message-flow.md#status-self-report-and-message-flow)'s sequence diagram needs to bypass that roster check, since
`_system` (the status-report recipient) and any pane on a *different* host (the cross-host relay
target) are never registered in the sending pane's own local roster.

**Every board-related `send.sh` call always passes `--force`,
including the ones a live Claude/Codex session makes for itself.** The bootstrap instruction (see
[Bootstrap flow](bootstrap.md#bootstrap-flow)) tells the agent to call `send.sh <team> <from> <to> "<body>"
--force` directly for board messages, rather than going through `/agmsg send` (which — confirmed
against agmsg's own skill templates — has no documented way to pass `--force` through). This is a
deliberate deviation from "just use agmsg's normal onboarding flow for everything": the roster
check `send.sh` performs by default is fundamentally incompatible with addressing a reserved
sentinel identity or a pane on a host that has never heard of it, so board traffic opts out of that
check uniformly instead of trying to satisfy it. A pane can still use unforced `/agmsg send` for
its *own*, non-board conversations with other agmsg-native agents already on that host (Codex,
Gemini CLI, etc.) — that path is untouched and still roster-checked normally.

**Panes join with `join.sh <team> <agent_id> <agent_type> <project_path> [--force]`**, invoked
directly by the agent as the first step of panemux's own bootstrap instruction (see [Bootstrap
flow](bootstrap.md#bootstrap-flow)) — not via agmsg's slash-command onboarding shorthand. This is a deliberate
deviation from "run agmsg's normal first-run flow": that shorthand's own invocation prefix is not
uniform across agmsg's agent types — agmsg's per-type `type.conf` driver files set `cmd_prefix` to
`/agmsg` for some types (`claude-code`, `cursor`, `grok-build`, `copilot`) and to `$agmsg` for
others (`codex`, `gemini`, `antigravity`, `opencode`) — so a bootstrap instruction that hardcoded
either prefix would silently be wrong for roughly half of agmsg's supported agent types. Calling
`join.sh` with its own verified positional-argument signature sidesteps that divergence entirely,
at the cost of bypassing whatever additional first-run behavior agmsg's slash-command flow might
otherwise perform beyond the join itself. This registers the pane into `teams/<team>/config.json`.
Because board sends always pass `--force`, this registration is no longer what gates board
delivery — its purpose is solely to let other, non-board-aware agmsg agents on the same host
(Codex, Gemini CLI, etc.) address the pane normally, and to give the pane a working `/agmsg`/`$agmsg`
identity for its own non-board use. Once joined, agmsg's own `SessionStart`/`SessionEnd` hooks own
that pane's `Monitor` (`watch.sh`)-process lifecycle end-to-end (launch, liveness, cleanup) —
panemux has no part in it and does not need to.

**panemux's own relay and command center are never agmsg roster members, and never go through
agmsg's own identity-detection layer, so any send they originate uses `--force` for the same reason
board traffic from a live pane does.** A live Claude session's own unforced `/agmsg send` normally
resolves `from` automatically — `whoami.sh` matches environment variables or, failing that, walks
the process tree against each agent type's known process-name patterns, then `identities.sh`
reconciles that against a joined team/project — but that whole chain assumes a live process to
introspect. The relay and command center are panemux's own Go code, not a joined Claude/Codex
process, so no identity-detection result would ever exist for them to look up.

**Team naming.** All board-enabled panes on a given panemux instance join the same agmsg team by
default (`agent_board.team`, default `"panemux"`), so message addressing is just pane IDs within
one team — see [Config additions](api-and-config.md#config-additions).

**Detection, not installation.** At bootstrap time panemux checks whether agmsg is available on
that pane's host by looking for `scripts/api.sh` under agmsg's configured skill-install location.
**`command -v agmsg` must not be used for this** — the `agmsg` npm package on `PATH` is a thin
bootstrapper whose own stated purpose is to *"reserve the agmsg name on npm and give users a
convenient `npx agmsg install` entry point"*; its presence says nothing about whether agmsg itself
is installed, and *invoking* it fetches and runs agmsg's real installer — exactly the auto-install
this design forbids. There is also no single fixed "known skill-install location": `install.sh`
prompts for a command name and installs under `~/.agents/skills/<that name>/`, and agmsg separately
supports an `AGMSG_STORAGE_PATH` override. panemux's detection therefore needs an explicit,
operator-set path (e.g. `agent_board.agmsg_path`, defaulting to the common `~/.agents/skills/agmsg/`
case but overridable), not a heuristic guess. If the configured/default path doesn't contain
`scripts/api.sh`, panemux skips board bootstrap for that pane, logs a clear warning naming the pane,
and leaves the pane's shell session itself untouched. panemux never runs `npx agmsg`, `npm i -g
agmsg`, `git clone`, or any other installer on the operator's behalf, on any host.

**`~` in `agmsg_path` is expanded by panemux, never left for the remote shell to expand.** A leading
`~` in `agent_board.agmsg_path` is resolved to an absolute path before it is ever placed into a
`RunBoardCommand` argument list — for the local host this is the ordinary `os.UserHomeDir()`-based
expansion this repository already uses elsewhere (see `DEVELOPMENT.md`'s testability rule); for a
remote host, panemux resolves the remote user's home directory once per SSH connection (a single
`echo -n "$HOME"` probe over the existing exec channel, cached for the life of that connection) and
substitutes it locally before building any command string. This is not optional: `shellQuotePath`
single-quotes every argument specifically to *suppress* shell expansion of its contents (see
[Security model](security-model.md#security-model)), and tilde expansion only happens for an unquoted leading `~` —
a literal `~` placed inside single quotes reaches the remote shell as the two-character string `~`,
not the operator's home directory, silently breaking detection and every subsequent `api.sh`/
`send.sh` call. `validRemotePath`'s own regex already only accepts paths starting with `/` for the
same underlying reason (see [security.md](../security.md)), so this expansion step is what lets an
operator write the natural `~/...` form in config while still handing `RunBoardCommand` an
already-absolute, already-safe-to-quote path.

**Remote command execution assumes a working, if non-interactive, shell environment.** The SSH exec
channel `RunBoardCommand` uses runs each command as a single non-login, typically non-interactive
shell invocation — the same channel `GetCWD`/`InspectGitContext` already use — which on many systems
does not source `~/.bashrc`, `~/.profile`, or equivalent interactive-only startup files. agmsg's own
runtime dependencies (`bash`, `node`, `sqlite3`) must therefore already be reachable from that
non-interactive shell's `PATH` for board detection and every subsequent command to succeed, even if
an operator's *interactive* SSH session (where they installed agmsg by hand) has a `PATH` that
differs — for example, a `node` made available only by an interactively-sourced version manager
(`nvm`, `asdf`, etc.) is not guaranteed visible here. This mirrors an assumption panemux's existing
SSH session handling already makes for the pane's own shell startup, and is not a new category of
risk this design introduces — but it is worth stating explicitly since a working interactive SSH
session is not sufficient evidence that board detection will also work on that host.

**Version pinning.** agmsg's own compatibility promise (per its README) only covers reading through
`scripts/api.sh`; there is no equivalent promise for `send.sh`'s argument order/behavior or for
`messages.db`. Because this design's write path depends entirely on `send.sh`, panemux is taking on
more exposure to agmsg's evolution than a "we only read from it" dependency would carry — see [J2
in Known limitations](limitations.md#known-limitations) for that tradeoff stated plainly. panemux implementation
must pin a specific tested agmsg version/tag and treat any change to either script's observed
behavior as an external dependency compatibility bug, tracked the same way any other pinned
dependency's breaking change would be. **That pin is `board.TestedAgmsgVersion`
(`internal/board/agmsg_version.go`), currently `1.2.0`** — the version whose own source
(`delivery.sh`, `scripts/lib/type-registry.sh`, the per-type `type.conf` manifests) the script
argument shapes documented here were read from. panemux reads each board-enabled host's
`<agmsg_path>/VERSION` once at startup and logs a warning when that install is one it has no coverage
for; a mismatch never blocks startup, because refusing to run against an untested agmsg would turn a
possible incompatibility into a certain outage.

**What counts as "no coverage" is narrower than "not byte-equal to the pin",** and both narrowings
were paid for:

- **The VERSION file has no single canonical form.** agmsg's repository carries a bare `1.2.0`, but
  `install.sh` writes a *provenance* string into the install root instead — `git describe` output
  from a checkout (`v1.2.0`, or `v1.2.0-6-g1a2b3c4` past the tag, `-dirty` on a modified tree),
  falling back to the bare `VERSION` only for a tarball install (npx/`setup.sh`, which has no `.git`).
  A byte comparison therefore warned an operator who had installed *exactly* the tested version
  through agmsg's own documented clone path, on every startup. The leading `v` and any describe tail
  are stripped before comparing. This was found by running Tier 2 against a real install, and is
  asserted there (`TestAgmsgContract_InstalledVersionDoesNotFalselyWarn`) rather than only against
  strings someone believed `install.sh` emits.
- **A patch release at or past the pinned one, in the same minor line, is quiet.** agmsg ships
  roughly every three days (median gap 2.9 days across its first 22 releases), so an operator on a
  current install is almost never on the exact pinned patch, and moving the pin is this repository's
  work rather than anything the warning's reader can act on. The silence is bought by coverage rather
  than assumed: Tier 2's canary runs the real contract against each new agmsg release, so a patch
  release that breaks the interface surfaces in CI here. Stated plainly — agmsg makes no semver
  promise for the scripts panemux's write path depends on, so this is a deliberate trade of noise for
  canary coverage, not a claim that a patch release cannot break anything.

The rule is therefore "same minor line, at or past the pinned patch", and the direction matters:

| Installed | | Why |
|---|---|---|
| The pinned release, in any of `install.sh`'s provenance forms | quiet | It *is* the tested version |
| A **newer** patch in the same minor line | quiet | The canary verified that release when it shipped |
| An **older** patch in the same minor line | warns | Never went through the canary, and may predate whatever the pin was moved for |
| Any other minor, or any other major | warns | Exactly where agmsg's script interface can move |
| A prerelease, or a string that will not parse | warns | Not the tested build; panemux does not assume about what it cannot read |
| No `VERSION` file at all | quiet | panemux does not claim a mismatch it could not observe |

Note that the older-patch row is unreachable while the pin's patch is `0`, as it is today — no
`1.2.x` is older than `1.2.0`. It first takes effect on a pin bump, which is why `releaseCovered` is
split from the pin and tested at that boundary directly rather than only through the pinned value. An install with no `VERSION` file reads as unknown and is not warned about — panemux
does not claim a mismatch it could not observe — and must be able to *detect* such a break mechanically
rather than discover it from a pane silently failing to communicate; see [agmsg compatibility
contract](agmsg-contract.md#agmsg-compatibility-contract).

### Two panes in one project directory

Two board-enabled panes whose agents run in the **same** project directory require an agmsg actas
lock and an agent-specific watcher subscription. Board *identity* is the pane ID end to end
(`join.sh`'s `agent_id`, the `from`/`to` on every row, `BoardCache.RecordStatus`'s key, and the
relay's own `validFrom` check), and a repository path plays no part in it. Delivery must preserve
that identity as well.

Read from agmsg's own source at the pinned `1.2.0` (unchanged in `1.2.2`): `scripts/watch.sh`
resolves what it subscribes to with `identities.sh <project> <type>`, which returns **every** (team,
agent) pair registered for that project and type — both pane IDs. Handed no 4th `<agent>` argument
the watcher subscribes broadly: it drops only pairs another live session already holds an actas
exclusivity lock on, and claims none itself. Neither pane claimed one, so neither pane's messages
were private to it. agmsg states the same failure in `scripts/lib/actas-lock.sh`'s own header —
"every concurrent CC session in that project would subscribe to every registered identity's
messages" — and ships `scripts/actas-claim.sh` as the remedy, which its `claude-code` template calls
directly after `join.sh`.

The bootstrap instruction now follows that template's own call order and argument shapes: `join.sh`,
then `actas-claim.sh "$(pwd)" <type> <pane-id> "$CLAUDE_CODE_SESSION_ID"`, then — only when a
watcher is actually running — stop the existing `agmsg inbox stream` task and invoke a new Monitor
with `watch.sh "$CLAUDE_CODE_SESSION_ID" "$(pwd)" <type> <pane-id>`, whose 4th argument both narrows
the subscription and re-claims the lock. A `status=held` result means another live session owns that
pane ID; the instruction tells the agent to report it and stop rather than disturb the running
watcher.

Three scoping decisions worth stating, because each one leaves something unfixed:

- **`claude-code` only.** `actas-claim.sh`'s 4th argument is the session id, and its source is
  per-type in agmsg's own `type.conf` `detect` key: `CLAUDE_CODE_SESSION_ID` here,
  `CODEX_THREAD_ID` for codex, absent entirely for opencode and cursor. Only the `claude-code`
  invocation has been verified against agmsg's own template, so it is the only one emitted — guessing
  an env var for another type would hand the script an empty session id. Panes of other types in a
  shared directory still cross-receive.
- **No watcher is started for `turn` or `off`.** `turn` delivers through a Stop hook with no
  watcher to replace, and `off` means the operator asked for no automatic delivery — starting one
  would contradict the setting. Both still claim the lock, which is what `check-inbox.sh`'s own
  subscription filtering reads.
- **This is bootstrap-time only.** The claim happens once, when the pane is onboarded. An agent
  process that exits and restarts inside a still-open pane is not re-bootstrapped (see [Known
  limitations](limitations.md#known-limitations)), so it does not re-claim; the lock it left behind becomes
  reclaimable once agmsg's own liveness check sees the old instance is gone.

What is *not* affected by any of this: the dashboard. Status rows are keyed by pane ID and validated
against the sending host's own board-enabled pane set, so two panes in one repository have always
rendered as two cards. Since the card no longer shows `repo` or `branch` (see
[ui-design.md](../ui-design.md#agent-board-ui)), the pane title is what distinguishes them at a glance —
worth setting to something meaningful when running several agents against one repository.
