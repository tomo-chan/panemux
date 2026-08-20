# Scenario Coverage

This document is the use-case-level view of what panemux is tested to do, from installing it to
using each feature. It exists because the layer above unit tests was, for a while, exercised only in
one-off manual sessions whose results lived nowhere: a reviewer could not tell which use cases had
ever been walked end to end, and a regression in one of them would not have been noticed.

It is a **map, not a test runner**. Each scenario names where it is verified — an automated test to
run, or the manual steps to follow when no automation exists — so the honest state of coverage is
visible rather than assumed.

## How to use it

- **Before releasing, or after changing Agent Board, the command center, or config loading**: run
  the automated rows (`make check` plus `make test-e2e`), then walk the manual rows for whatever you
  touched.
- **When adding a feature**: add its scenarios here in the same change, with their verification
  column filled in. A row whose verification is `manual` is a legitimate answer; a row that is
  silently absent is not.
- **When automating a manual row**: move it, and delete it from [Not covered](#not-covered) if it
  was listed there.

Verification column values:

| Value | Meaning |
|---|---|
| `auto` | Runs in `make check` or `make test-e2e`. The named file is where it lives. |
| `auto (opt-in)` | Automated, but excluded from `make check` because it needs something the repo cannot assume. The command to run it is given. |
| `manual` | No automation. The steps are written out in full so anyone can walk it. |

## A. Install and first run

| # | Scenario | Expected | Verification |
|---|---|---|---|
| A1 | Install from a pre-built binary | `panemux` runs with no config file present | `manual`: download per [README](../README.md#pre-built-binary), run `./panemux --port 9090`, expect a default single-pane workspace |
| A2 | Build from source | `make install-deps && make build` produces `bin/panemux` with the frontend embedded | `manual`, and partly `auto` — CI builds on every PR (`.github/workflows/ci.yml`) |
| A3 | First run with no config | A config file and an auth token are created, and the token is **not** written into `config.yaml` | `auto`: `internal/config` — `TestEnsureAuthToken_GeneratesAndPersists_WhenAbsent`, `TestFinishLoad_NeverCallsEnsureAuthToken` |
| A4 | Second run | The token persisted on first run is reused, not regenerated | `auto`: `internal/config` — `TestEnsureAuthToken_ReadsExisting_WhenPresent` |
| A5 | Operator sets their own token in `config.yaml` | The explicit value wins over the generated file | `auto`: `internal/config` — `TestEnsureAuthToken_DoesNotOverride_ExplicitYAMLToken` |
| A6 | Non-loopback `server.host` with no token | Startup fails validation with a clear message | `auto`: `internal/config` — `TestValidate_NonLoopbackHost_EmptyToken_Error` and its passing counterpart |

## B. Configure Agent Board

| # | Scenario | Expected | Verification |
|---|---|---|---|
| B1 | No pane enables `agent_board` | No Agent Board entry point anywhere in the UI, and the shortcut does nothing | `auto`: `frontend/e2e/agent-board-disabled.spec.ts` (both tests) |
| B2 | A pane enables `agent_board` | The Agent Board button appears and `Cmd/Ctrl+Shift+B` opens the panel | `auto`: `frontend/e2e/agent-board.spec.ts` |
| B3 | Enabling the board through the pane settings dialog | The pane gains `agent_board.enabled`, and saving does not require a session restart | `auto`: `frontend/src/hooks/usePaneSettings.test.ts`, `frontend/src/components/PaneSettingsDialog.test.tsx` |
| B4 | Editing the layout afterwards | A layout `PUT` does not delete the pane's `agent_board` block | `auto`: `frontend/src/schemas/index.test.ts` (the layout round-trip that used to strip it) |
| B5 | An invalid `agent_board.mode` | Startup fails validation; every valid value is accepted | `auto`: `internal/config` — `TestValidate_AgentBoardMode_InvalidValue_Error`, `TestValidate_AgentBoardMode_ValidValues_NoError` |
| B6 | `agent_board.team` set to the reserved `_system` | Rejected | `auto`: `internal/config` — `TestValidate_AgentBoardTeam_ReservedSystemID_Error` |
| B7 | Changing a pane's mode after startup | The next bootstrap uses the new mode, read live rather than from a startup snapshot | `auto`: root package — `TestBootstrapWatcherReadsModeLive` |

## C. A pane joins the board

| # | Scenario | Expected | Verification |
|---|---|---|---|
| C1 | agmsg is not installed on the host | No PTY write, one warning, retried on later ticks — the pane's shell is unaffected | `auto`: root — `TestBootstrapWatcher_AgmsgNotPresent_NoWrite_WarnsOnce` |
| C2 | agmsg is present and an agent is detected | The onboarding instruction is written into the pane once | `auto`: root — `TestBootstrapWatcher_PersistSuccessfulBootstrap`, `TestBootstrapWatcher_AlreadyBootstrapped_NoRewrite` |
| C3 | No agent running in the pane | Nothing is written | `auto`: root — `TestBootstrapWatcher_NoAgentDetected_NoWrite` |
| C4 | The instruction's content | Names the pane ID as the agmsg `agent_id`, defines what a `summary` is, and never uses a slash-command prefix | `auto`: root — `TestBuildBootstrapInstruction_*` |
| C5 | Two board panes in one project directory | Each claims its own identity, so neither receives the other's messages | `auto`: root — `TestBuildBootstrapInstruction_ClaimsItsOwnIdentity` (the instruction) plus C6 (the agmsg behavior it relies on) |
| C6 | agmsg really does need that claim, and honors it | Without a claim a second session resolves both panes; after `actas-claim.sh` it skips the claimed one | `auto (opt-in)`: `make test-agmsg-contract AGMSG_PATH=~/.agents/skills/agmsg` — `internal/board/agmsg_contract_test.go` |
| C7 | A remote (SSH) host | Presence is probed over the existing exec channel, and a transport error is distinguished from "absent" | `auto`: root — `TestBootstrapWatcher_RemotePresenceCheck_YesWritesNoDoesNot`, `..._TransportError_DistinctFromNo` |
| C8 | The PTY write fails or is short | A short write is never retried; a clean failure is retried up to the limit | `auto`: root — `TestBootstrapWatcher_ShortWrite_GivesUpImmediately_NeverRetries`, `..._WriteError_RetriedUpToLimitThenGivesUp` |

## D. Read the board

| # | Scenario | Expected | Verification |
|---|---|---|---|
| D1 | A pane reports status | Its card shows state, summary, last tool and how long ago | `auto`: `frontend/e2e/agent-board-agmsg.spec.ts` |
| D2 | A board-enabled pane never reports | It is still listed, as `not joined` | `auto`: `frontend/e2e/agent-board.spec.ts`, `frontend/e2e/agent-board-agmsg.spec.ts` |
| D3 | A long summary | Rendered in full over several lines, not clipped to one | `auto`: `frontend/e2e/agent-board-agmsg.spec.ts` (asserts the rendered height) |
| D4 | repo / branch / PR | Not rendered, and the card contains no link | `auto`: `frontend/e2e/agent-board-agmsg.spec.ts`, `frontend/src/components/BoardDashboardPanel.test.tsx` |
| D5 | A cross-pane message arrives while the panel is open | It appears without reopening the panel | `auto`: `frontend/e2e/agent-board-agmsg.spec.ts` |
| D6 | A `board_status` row | Never rendered as a message in the feed | `auto`: `frontend/e2e/agent-board-agmsg.spec.ts`, `frontend/src/hooks/useBoardStatus.test.ts` |
| D7 | Polling and auth | Board APIs are polled only while the panel is open, always with a bearer token | `auto`: `frontend/e2e/agent-board.spec.ts` |
| D8 | Closing the panel | Escape, backdrop click and the close button all return focus where it was | `auto`: `frontend/e2e/agent-board.spec.ts` (three tests) |
| D9 | A stale report | `stale` pill, card dimmed, still shown | `auto`: `frontend/src/components/BoardDashboardPanel.test.tsx`, `frontend/src/utils/boardStatusColors.test.ts` |

## E. Command center

| # | Scenario | Expected | Verification |
|---|---|---|---|
| E1 | Opening the palette | `Cmd/Ctrl+Shift+K` opens it even while a terminal pane has focus, and the input takes focus | `auto`: `frontend/e2e/command-center.spec.ts` |
| E2 | Asking a question | The answer streams into the turn; frame bookkeeping is not rendered | `auto`: `frontend/e2e/command-center.spec.ts` |
| E3 | A slow query | The turn shows as in flight until the subprocess exits | `auto`: `frontend/e2e/command-center.spec.ts` |
| E4 | History | The turn is readable in the history panel afterwards, from the persisted file | `auto`: `frontend/e2e/command-center.spec.ts`, `frontend/src/utils/streamJson.test.ts` |
| E5 | WebSocket auth | The wrong token is refused; the configured token is accepted | `auto`: `frontend/e2e/command-center.spec.ts` (both directions) |
| E6 | Subprocess containment | Acting tools are denied by name, slash commands are disabled, only panemux's own narrowing settings are sent, and a fresh session id is minted | `auto`: `internal/commandcenter` — `TestRunnerDeniesActingToolsByName`, `TestRunnerDisablesSlashCommands`, `TestRunnerSendsOnlyPanemuxOwnSettings`, `TestRunnerFirstRunMintsAndPersistsItsOwnSessionID` |
| E7 | A real `claude` binary answers a real board question | The reply reflects actual board state | `manual`: enable `command_center`, start panemux with `claude` on PATH, ask "which panes are on the board?" and compare the answer against the dashboard |
| E8 | Command center disabled | No palette, no history button, and `/ws/board-command` is not registered at all | `auto`: `internal/server/board_routes_test.go`; the UI half is covered by `frontend/src/App.test.tsx` |

## F. Documentation

Documentation is part of the product here: an operator cannot use Agent Board without following
[README](../README.md#agent-board), because panemux never installs agmsg itself.

| # | Scenario | Expected | Verification |
|---|---|---|---|
| F1 | Following the README's Agent Board setup from scratch | An operator reaches a working board without reading source | `manual`: follow [README](../README.md#agent-board) top to bottom on a clean machine |
| F2 | The prerequisites section | Names agmsg, says panemux never installs it, and links to it | `manual`: read [README](../README.md#prerequisites) |
| F3 | Config examples match the schema | Every key in `config.example.yaml` is accepted by validation | `manual`: `./bin/panemux --config config.example.yaml` starts without a **validation** error — warnings about missing shells or SSH keys are environmental and expected |
| F4 | Delivery-mode documentation | Describes what each mode does and its one setup step | `manual`: read [README](../README.md#delivery-mode-and-the-one-setup-step-it-needs) |
| F5 | Security claims are current | Every claim in [security.md](security.md) is either verified or explicitly marked unverified | `manual`: reread the sections touching whatever changed |
| F6 | Design docs match shipped behavior | Status notes in [agent-board.md](agent-board.md) and [ui-design.md](ui-design.md) reflect what actually ships | `manual`: check the status note of any section you relied on |

## Not covered

Stated explicitly, because an absent row reads as an oversight and these are decisions:

- **Remote (`ssh` / `ssh_tmux`) board panes have no e2e scenario.** All three board fixtures use
  `type: local`. The remote paths are unit-tested (C7) but never walked end to end, because doing so
  needs a second host the suite cannot assume.
- **The command center's real `claude` binary is only exercised manually** (E7). The e2e fixture
  stubs the binary, deliberately: the shipped argv is pinned against the real CLI's documented
  behavior in `internal/commandcenter/runner_test.go`, and a stub cannot reproduce that parsing.
- **The agmsg contract tests are opt-in** (C6). They need a real agmsg install and `sqlite3`, so
  `make check` would stop being hermetic. No CI job runs them yet.
- **Install scenarios A1/A2 are manual.** CI builds the binary on every PR, but nobody automatically
  downloads a release artifact and runs it.
- **Every row in section F is manual.** Documentation accuracy is not mechanically checkable here.

## Related documents

- Developer workflow and the TDD rules these scenarios sit on top of: [../DEVELOPMENT.md](../DEVELOPMENT.md)
- Agent Board design, including its own testing plan and agmsg compatibility contract: [agent-board.md](agent-board.md)
- Behavior and API specification: [behavior.md](behavior.md)
- Security requirements: [security.md](security.md)
