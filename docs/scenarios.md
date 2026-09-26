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
| A7 | panemux runs where the home directory cannot be resolved — a systemd unit with no `HOME`, a container with no passwd entry | Nothing is read or written relative to the working directory. A `~/` path is left as written rather than rebuilt against an empty home, the SSH key and `known_hosts` reads refuse any path that is still relative, the default-key probe is skipped, and each feature whose own state file needs a home directory turns itself off naming the step that failed | `auto`: `internal/config` — `TestExpandPaths_UnresolvableHomeDirectory_LeavesTildePathsAlone`, `TestExpandPanePaths_UnresolvableHomeDirectory_LeavesTildeCwdAlone`, `TestDefaultConfigPath_NoHomeDirectory_Errors`; `internal/session` — `TestResolveSSHConfig_UnresolvableHomeDirectory_LeavesTheIdentityFileAlone`, `TestBuildAuthMethods_UnresolvableHomeDirectory_DoesNotProbeTheWorkingDirectory`, `TestBuildAuthMethods_NonAbsoluteKeyFile_IsRefusedRatherThanReadFromTheWorkingDirectory`, `TestResolveKnownHostsFile_NonAbsolutePath_IsRefused`; `internal/board` — `TestDefaultFilePaths_UnresolvableHomeDirectory_Error`; `internal/commandcenter` — `TestDefaultPathsReportAnUnresolvableHomeDirectory`, `TestDefaultContextDirReportsAnUnresolvableHomeDirectory` |

## B. Configure Agent Board

| # | Scenario | Expected | Verification |
|---|---|---|---|
| B1 | No pane enables `agent_board` | No Agent Board entry point anywhere in the UI, and the shortcut does nothing | `auto`: `frontend/e2e/agent-board-disabled.spec.ts` (both tests) |
| B2 | A pane enables `agent_board` | The Agent Board button appears and `Cmd/Ctrl+Shift+B` opens the panel | `auto`: `frontend/e2e/agent-board.spec.ts` |
| B3 | Enabling the board through the pane settings dialog | The pane gains `agent_board.enabled`, and saving does not require a session restart | `auto`: `frontend/src/hooks/usePaneSettings.test.ts`, `frontend/src/components/PaneSettingsDialog.test.tsx` |
| B4 | Editing the layout afterwards | A layout `PUT` does not delete the pane's `agent_board` block | `auto`: `frontend/src/schemas/index.test.ts` (the layout round-trip preserves it) |
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
| C6 | agmsg really does need that claim, and honors it | Without a claim, one pane's watcher receives the *other* pane's messages; after `actas-claim.sh` it receives only its own | `auto (opt-in)`: `make test-agmsg-contract AGMSG_PATH=~/.agents/skills/agmsg` — `internal/board/agmsg_contract_test.go`; runs in CI via `.github/workflows/agmsg-contract.yml` |
| C7 | A remote (SSH) host | Presence is probed over the existing exec channel, and a transport error is distinguished from "absent" | `auto`: root — `TestBootstrapWatcher_RemotePresenceCheck_YesWritesNoDoesNot`, `..._RemotePresenceCheckTransportError_DistinctFromNo` |
| C8 | The PTY write fails or is short | A short write is never retried; a clean failure is retried up to the limit | `auto`: root — `TestBootstrapWatcher_ShortWrite_GivesUpImmediately_NeverRetries`, `..._WriteError_RetriedUpToLimitThenGivesUp` |
| C9 | A pane keeps failing the same way — a tmux server that went away, agmsg still not installed | The warning is logged once per failing streak rather than once per tick, so one broken pane cannot drown out the rest of the log; a failure that returns after a recovery is logged again — including the recovery of a restarted pane, whose new session inherits no suppression — and one kind of warning never suppresses another for the same pane | `auto`: root — `TestCheckPaneLogsADetectionFailureOncePerStreakAndDropsThePane`, `TestCheckPaneWarnsAgainAfterADetectionFailureStreakEnds`, `TestBootstrapWarningsOfDifferentKindsDoNotSuppressEachOther`, `TestClearingOneWarningKindLeavesTheOthersSuppressed`, `TestARestartedPaneWarnsAboutTheSameFailureAgain`, `TestAPaneThatDisappearsDropsItsSuppressedWarnings` |

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
| E5 | WebSocket auth | The wrong token is refused; the configured token is accepted, and the handshake echoes the subprotocol the browser offered | `auto`: `frontend/e2e/command-center.spec.ts` (both directions); through the production router in `internal/server/ws_integration_test.go` — `TestWSIntegration_BoardCommandRoute_RejectsWrongOrMissingToken`, `TestWSIntegration_BoardCommandRoute_AcceptsTheTokenAsASubprotocol` |
| E6 | Subprocess containment | Acting tools are denied by name, slash commands are disabled, only panemux's own narrowing settings are sent, and a fresh session id is minted | `auto`: `internal/commandcenter` — `TestRunnerDeniesActingToolsByName`, `TestRunnerDisablesSlashCommands`, `TestRunnerSendsOnlyPanemuxOwnSettings`, `TestRunnerFirstRunMintsAndPersistsItsOwnSessionID` |
| E7 | A real `claude` binary answers a real board question | The reply reflects actual board state | `manual`: enable `command_center`, start panemux with `claude` on PATH, ask "which panes are on the board?" and compare the answer against the dashboard |
| E8 | Command center disabled | No palette, no history button, and `/ws/board-command` is not registered at all — a probe gets chi's own 404, not the handler's 401 | `auto`: `internal/server/board_routes_test.go`, and against a real handshake in `internal/server/ws_integration_test.go` — `TestWSIntegration_BoardCommandRoute_AbsentWhenCommandCenterDisabled`; the UI half is covered by `frontend/src/App.test.tsx` |
| E9 | A query's history cannot be written | The write failure is shown as a warning on whichever terminal frame the query ends with — under the answer on a turn that succeeded, alongside the error on one that did not — and never as a failed turn, nor silently | `auto`: `internal/commandcenter` — `TestQueryAttachesAFailedHistoryWriteToWhicheverTerminalEventEndsTheTurn`, `TestQueryReportsNoWarningsWhenTheHistoryWriteSucceeds`, `TestQueryAlsoLogsAFailedHistoryWrite`; on the wire in `internal/ws` — `TestBothTerminalEventsCarryTheirWarningsOntoTheFrameAndOmitThemOtherwise`; in the UI, `frontend/src/components/CommandPalette.test.tsx` and `frontend/src/hooks/useBoardCommand.test.ts` |
| E10 | The subprocess emits a malformed `stream-json` line and then stops writing without exiting | The error frame reaches the palette, and the busy flag is released, without waiting out the query timeout: the subprocess is canceled first and its remaining output drained only afterwards | `auto`: `internal/commandcenter` — `TestRunnerMalformedStreamJSONDoesNotWaitOnASubprocessThatStopsWritingWithoutExiting`, `TestRunnerMalformedStreamJSONCancelsQueryContextImmediately`, `TestRunnerDrainsRemainingOutputAfterAScannerErrorOutsideStreamOutput` |
| E11 | The board MCP server answers a request | Every response carries exactly one of `result`/`error`, a successful one's `result` is always an object (MCP's schema rejects `null` as it rejects an absent key), and a parse error answers with an explicit null id rather than none — JSON-RPC 2.0 §5 and MCP, not merely what today's client tolerates | `auto`: `internal/boardmcp` — `TestInitializedSentAsARequestIsAnsweredWithAnEmptyResultObject`, `TestAnErrorResponseCarriesNoResultMember`, `TestServerMalformedLineReturnsParseError` |

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
| F6 | Current-state docs match shipped behavior | Topic guides state the present contract without rollout/status narration; historical rationale is in [DECISIONLOG.md](DECISIONLOG.md) | `manual`: check the topic guide and decision-log entry for any area changed |
| F7 | A reader can choose the right level of detail | [docs/README.md](README.md) routes from overview to concise topic guides and then to focused deep dives | `manual`: follow each route in the documentation index |

## G. The agmsg compatibility contract

agmsg is an external tool that promises compatibility only for reading through `api.sh`, while
panemux's write path depends on `send.sh` and its bootstrap on `join.sh`/`actas-claim.sh`/`watch.sh`.
These rows are what turns "an agmsg release broke us" from a user-discovered outage into a CI signal.
See [agent-board/agmsg-contract.md](agent-board/agmsg-contract.md#agmsg-compatibility-contract) for the two tiers.

| # | Scenario | Expected | Verification |
|---|---|---|---|
| G1 | panemux parses what agmsg really prints | Real captured `api.sh` JSONL parses into rows, status reports and history exactly as the code assumes | `auto`: `internal/board/agmsg_fixture_test.go` against `testdata/agmsg-v1.2.0/` (captured, not hand-written — see its README) |
| G2 | A message survives the round trip | What `send.sh` wrote comes back out of `api.sh` with team/from/to/body/timestamp intact, through panemux's own `LocalAgmsgClient` | `auto (opt-in)`: `TestAgmsgContract_SendThenSinceRoundTrip` |
| G3 | A body full of shell and SQL metacharacters | Stored and returned byte for byte — no expansion, no quote stripping, no SQL interpretation | `auto (opt-in)`: `TestAgmsgContract_ShellMetacharacterBodyRoundTrips` |
| G4 | The relay's poll cursor against a real store | A poll with nothing new returns nothing; the next message, and only it, comes back | `auto (opt-in)`: `TestAgmsgContract_SinceCursorAnchorsOnReturnedOrder` — the regression test for the numeric-id cursor bug |
| G5 | agmsg's message ids | Present and distinct; nothing about their ordering is assumed (today's are UUIDv7, not integers) | `auto (opt-in)`: `TestAgmsgContract_MessageIDsAreOpaque`, plus `TestAgmsgFixture_TeamMessages_IDsAreNotNumeric` in `make check` |
| G6 | `--limit` bounds a poll | Returns the *newest* n rows, oldest first — the assumption the accepted truncation tradeoff rests on | `auto (opt-in)`: `TestAgmsgContract_SinceLimitKeepsTheNewestRows` |
| G7 | A `board_status` report end to end | The `_system` sentinel survives agmsg verbatim and the body is still recognized as a status report | `auto (opt-in)`: `TestAgmsgContract_StatusRowRoundTrips` |
| G8 | A pane ID is used verbatim as the agmsg agent id | A generated ID like `pane-1787195690568-re241` registers unchanged | `auto (opt-in)`: `TestAgmsgContract_JoinUsesThePaneIDVerbatim` |
| G9 | An agmsg release changes behavior | The canary fails against agmsg's latest tag, before anyone here bumps the pin | `auto`: `.github/workflows/agmsg-contract.yml`, `schedule` trigger (daily 06:00 UTC, doing real work once per new agmsg release) |
| G10 | A PR bumps `board.TestedAgmsgVersion` | The contract runs against the new pin and blocks the merge if real behavior differs | `auto`: same workflow, `pull_request` trigger — see [maintenance.md](maintenance.md#the-agmsg-compatibility-contract-job) for the branch-protection step this depends on |
| G11 | An operator installs exactly the pinned agmsg | No version warning at startup, whichever install path they used — `install.sh` writes `v1.2.0`, not `1.2.0` | `auto`: `TestVersionMismatchWarning_InstallProvenanceForms`; `auto (opt-in)`: `TestAgmsgContract_InstalledVersionDoesNotFalselyWarn` against the real installer |
| G12 | An operator runs a newer patch of agmsg | No warning — the canary verified that release when it shipped | `auto`: `TestVersionMismatchWarning_PatchReleasesInTheTestedLineAreQuiet` |
| G12a | An operator runs a patch *older* than the pin | Warned — it never went through the canary. Unreachable while the pin's patch is 0, so the rule is tested apart from the pin | `auto`: `TestReleaseCovered` |
| G13 | An operator runs a different minor/major, or an unreadable version | Warned, naming the version found and the pin | `auto`: `TestVersionMismatchWarning_StillWarnsWhereItMatters` |

## H. The core multiplexer

The layer everything else sits on: panes, splits, layout persistence and
workspaces. It had no rows at all until issue #180's roadmap item 5, which is
the exact failure this document's own rule was written against — Agent Board
and the command center were mapped in detail while the feature panemux exists
to provide was not mapped at all.

These run against their own panemux process and fixture
(`frontend/e2e/core-multiplexer.yml`, port 4177) rather than the shared one,
because they mutate the layout: splitting, closing, resizing and deleting
workspaces would otherwise change the state every later spec sees. Sharing was
tried first and made an unrelated spec fail intermittently.

| # | Scenario | Expected | Verification |
|---|---|---|---|
| H1 | Splitting a pane horizontally | A second pane appears beside the first, with a live session of its own, and closing it restores the original layout exactly | `auto`: `frontend/e2e/core-multiplexer.spec.ts` |
| H2 | Splitting a pane vertically | The new pane is stacked below rather than beside | `auto`: `frontend/e2e/core-multiplexer.spec.ts` |
| H3 | A layout change survives a reload | Reloading the dashboard restores the same panes, in the same order — the layout is persisted, not React state | `auto`: `frontend/e2e/core-multiplexer.spec.ts` |
| H4 | Dragging a divider to resize | The panes either side change size, and the new sizes survive a reload (the save is debounced by 500ms) | `auto`: `frontend/e2e/core-multiplexer.spec.ts` |
| H4b | The same drag on a split with more than two panes, or inside a nested one | Only the two panes either side of that divider move, by the dragged distance as a share of the container's usable size — its own width or height minus its dividers. A nested split reports the change as an edit to the outer child holding it, a drag that would take either neighbour below 5% is refused, and a container that has not been laid out yet resizes nothing rather than dividing by a negative size | `auto`: `frontend/src/components/SplitContainer.test.tsx` |
| H5 | Layout tree operations | Insert, remove, swap and move produce the tree the UI expects, including at workspace edges | `auto`: `frontend/src/utils/layoutTree.test.ts` |
| H6 | Moving a pane by dragging its header | The pane moves to a workspace edge, or beside another pane, without duplicating or losing any pane | `auto`: `frontend/e2e/pane-move.spec.ts` |
| H6b | Where that drag can be dropped | Three targets, each previewing itself while the pointer is over it: a workspace edge, a divider — which resolves to the deepest pane against that boundary, either side — and another pane's body, whose nearest edge follows the pointer and is read again at the release rather than trusted from the last preview. A release onto the dragged pane itself, or with no drag in progress, moves nothing | `auto`: `frontend/src/components/SplitContainer.test.tsx`, `frontend/src/components/TerminalPane.test.tsx` |
| H7 | Closing the last pane in a workspace | The pane's session is terminated and removed from the persisted layout | `auto`: `internal/api` — `TestDeleteSession*`; `frontend/src/hooks/useLayout.test.ts` |
| H8 | Adding, renaming and deleting a workspace | The tab appears, the rename survives a reload, and deleting asks for confirmation first — in panemux's own dialog, where cancelling or Escape leaves the workspace alone | `auto`: `frontend/e2e/core-multiplexer.spec.ts`; `frontend/src/App.test.tsx`; `frontend/src/components/ConfirmDialog.test.tsx` |
| H8b | Answering that confirmation with the keyboard alone | Tab and Shift+Tab cycle between the dialog's own buttons rather than reaching the workspace controls behind it, and Escape cancels even when the focused element stops the keystroke propagating | `auto`: `frontend/src/components/ConfirmDialog.test.tsx` — `keyboard focus stays in the dialog`, `cancels on Escape even when the focused element stops the event propagating` |
| H8c | Any modal dialog is open and the operator keeps pressing Tab | Focus never leaves it: it cycles inside, a Tab from outside is pulled back in, a nested dialog takes the trap with it, and a dialog mid-save keeps the trap while refusing Escape. Every `aria-modal` surface uses the same hook | `auto`: `frontend/src/hooks/useModalKeyboard.test.tsx`; the wiring per dialog in `AddSSHHostDialog.test.tsx`, `PaneSettingsDialog.test.tsx`, `CommandPalette.test.tsx`, `CommandHistoryPanel.test.tsx`, `BoardDashboardPanel.test.tsx` — `keeps Tab inside the dialog` |
| H9 | Deleting the last remaining workspace | Refused with `409`, so the dashboard is never left with nothing to show | `auto`: `internal/api` — `TestDeleteWorkspace*`. Deliberately not driven in E2E: doing so means deleting every workspace on the shared server, which wrecks the fixture for every later spec |
| H10 | Switching to a previously hidden workspace | Its terminal renders rather than coming back blank | `auto`: `frontend/e2e/workspace-switch.spec.ts` |
| H11 | An inactive workspace needs attention | Its tab is flagged and a browser notification fires | `auto`: `frontend/e2e/workspace-switch.spec.ts` |
| H11c | The same agent prompt is seen again after a reload, or while storage is unavailable | A prompt already notified about is not notified again — the record survives a reload, and a browser that refuses storage falls back to memory rather than notifying on every redraw | `auto`: `frontend/src/utils/attentionNotificationState.test.ts` |
| H11b | Switching workspaces while panes are monitored for attention | The monitor sockets stay open — a switch rebuilds the callback and refetches the workspace list, and neither reconnects anything while the same panes are watched | `auto`: `frontend/src/hooks/useWorkspaceAttentionMonitor.test.ts` — `does not reconnect monitor sockets when only the attention callback identity changes`, `does not reconnect when the workspace list is refetched with the same pane IDs`, `reconnects when the pane ID set actually changes` |
| H12 | Workspace bar position and width | Every position renders, and the width is persisted | `auto`: `frontend/src/components/WorkspaceTabs.test.tsx`, `internal/api` — `TestPutWorkspaceTabPosition*`, `TestPutWorkspaceVerticalBarWidth*` |
| H12b | A URL too long for the pane is printed into it | It is one link across every row it occupies, whether the terminal wrapped it or the program printed its own newline at the pane edge; a fragment that runs to the edge with nothing continuing it is not clickable at all, so a URL cut inside its hostname cannot open a different host | `auto`: `frontend/src/hooks/useTerminalLinks.test.ts` — `url link provider: wrapped urls` |
| H12b2 | That URL sits beside an emoji, or the row ends with the back half of a CJK glyph | The link still covers the URL and nothing else: cells are mapped per UTF-16 code unit, so an astral character does not shift every later range, and a row whose last column holds a wide character's trailing half still counts as reaching the pane edge — so it joins, and the cut-off suppression still applies to it | `auto`: `frontend/src/hooks/useTerminalLinks.test.ts` — `url link provider: characters that are not one unit per cell` |
| H12b3 | That URL wraps across rows inside a TUI's drawn frame | Matching Unicode box-drawing decorations at the same left and right cell columns are excluded from the link, with or without inner padding and without shifting ranges around wide or astral characters; differing decorations and ordinary ASCII `|` output are not stripped | `auto`: `frontend/src/hooks/useTerminalLinks.test.ts` — `url link provider: urls wrapped inside a drawn border` |
| H12b4 | A `#123` reference is printed in a pane whose repository is known | It links to that repository's pull request, at the row and columns it actually occupies — the same 1-based mapping the URL provider uses, so a reference inside a URL is resolved in the URL's favour rather than landing on a different row entirely | `auto`: `frontend/src/hooks/useTerminalLinks.test.ts` — `pull request link provider` |
| H12c | Dragging a split divider | Each pointer move resizes by its own delta on the split's axis, the page cursor is taken over for the drag and given back on release, and nothing is resized after the button is released | `auto`: `frontend/src/components/SplitDivider.test.tsx` |
| H12d | The pane status bar | It labels every session type, shows the SSH connection and terminal size only when there is one, and the pane's own `show_status_bar` overrides the display default in both directions | `auto`: `frontend/src/components/PaneStatusBar.test.tsx` |
| H12e | A pane's own chrome | Clicking or focusing a pane makes it the active one, clears its attention flag and refreshes its git metadata; the attention outline wins over the active one when both apply; each header action — split, add beside, close, maximize and restore, settings, open in VSCode — acts on that pane and no other; and the pane re-fits its terminal whenever its element changes size, stopping once it unmounts | `auto`: `frontend/src/components/TerminalPane.test.tsx` |
| H13 | Terminal rendering and scrollback | The xterm viewport's scrollbar matches the terminal chrome | `auto`: `frontend/e2e/terminal-scrollbar.spec.ts` |
| H14 | Session types | `local`, `tmux`, `ssh` and `ssh_tmux` panes are validated and constructed from config. SSH-backed panes complete PTY/shell or tmux exec setup, round-trip bytes, resize, run auxiliary exec channels, parse responses, transition state on remote closure, and close without a reachable host; local tmux panes exercise the same PTY I/O/resize/close lifecycle without a tmux install | `auto`: `internal/config` — `TestValidatePane*`; `internal/session` — `TestSSHSessionLifecycleOverInProcessTransport`, `TestSSHSessionRemoteSideClosureChangesStateToExited`, `TestTmuxSSHSessionLifecycleOverInProcessTransport`, `TestTmuxSSHSessionRemoteSideClosureChangesStateToExited`, `TestSSHSessionExecMethodsUseRealChannelsAndParseResponses`, `TestTmuxSSHSessionExecMethodsUseRealChannelsAndParseResponses`, `TestTmuxLocalSessionLifecycleWithInjectedCommand`. Real external hosts and tmux servers remain `manual` — see [Not covered](#not-covered) |
| H14a | A hand-written single-pane workspace (`layout: {pane: ...}`, no children) | Loads and renders: the pane is migrated into the one child it means, so the response carries the `direction` + `children` shape the dashboard parses | `auto`: `internal/config` — `TestNormalizeLayoutNode_PaneOnlyRootBecomesItsSingleChild`, `TestNormalizeLayoutNode_AlwaysSerializesDirectionAndChildren`, `TestWorkspacesView_NormalizesEveryWorkspaceLayout`, `TestActiveLayout_NormalizesWhatItReturns` |
| H14b | A hand-written workspace with a root `pane` beside `children` | The root pane is left where the operator wrote it and survives a dashboard round-trip, rather than being stripped by the schema and deleted from `config.yaml` on the next split | `auto`: `internal/config` — `TestNormalizeLayoutNode_KeepsARootPaneThatSitsBesideChildren`; `frontend/src/schemas/index.test.ts` — `LayoutNodeSchema root pane round-trip` |
| H14c | A root pane that names no `type`, or an `ssh` connection that is not defined | Startup fails naming the pane, the same way the equivalent child pane always has, instead of loading into a workspace that displays nothing | `auto`: `internal/config` — `TestLoad_RelocatedRootPaneIsValidatedLikeAnyOther`, `TestLoad_WellFormedRootPaneRelocatesAndLoads` |
| H14d | A client `PUT`s a layout with no `direction` | The stored and echoed layout is normalized, so the response is a shape `LayoutNodeSchema` accepts rather than `"direction": ""` | `auto`: `internal/api` — `TestPutLayoutRoutes_EchoANormalizedNode`, `TestPutLayout_RelocatesAPaneOnlyRoot` |
| H14e | A client `PUT`s a root pane whose `cwd` is `~/…` | The relocated pane's path is expanded before it is echoed and persisted, rather than a literal `~/` reaching `config.yaml` | `auto`: `internal/api` — `TestPutLayoutRoutes_ExpandARelocatedRootPaneCwd` |
| H15 | WebSocket reconnect and replay | Output produced while disconnected is replayed once, in order, on reconnect, bracketed by `replay` control frames | `auto`: `internal/ws`, and over a real handshake through the production router in `internal/server/ws_integration_test.go` — `TestWSIntegration_TerminalRoute_ReplaysBufferedOutputOnReconnect`; plus the Alloy model in `docs/models/replay_state.als`, checked by `.github/workflows/model-check.yml` |
| H15b | A pane that has been producing output for hours is remounted | The replay is the newest 256KB of that pane's output — oldest bytes dropped first, order preserved — however much has scrolled past since | `auto`: `internal/session/replay_buffer_test.go` — `TestReplayBuffer_RetainsNewestBytes`, `TestReplayBuffer_MatchesTailOfEverythingWritten`, `TestReplayBuffer_GrowsWithoutLosingWhatItHolds`, `TestReplayBuffer_SnapshotIsIndependentOfLaterAppends`; and `internal/session/manager_test.go` — `TestManagedSession_Publish_ReplayBufferBound`, `TestManagedSession_Publish_DeliversTheChunkWithAFullReplayWindow` |
| H16 | A browser attaches to a pane's terminal | The handshake succeeds, keystrokes reach the pane, its output comes back as binary frames, a resize is applied, and the pane exiting arrives as a final status frame | `auto`: `internal/server/ws_integration_test.go` — `TestWSIntegration_TerminalRoute_StreamsBothDirections`, `TestWSIntegration_TerminalRoute_ReportsFinalStateWhenThePaneExits`, `TestWSIntegration_TerminalRoute_UnknownPane_404` |
| H17 | The dashboard parses what the server actually sends | Every Zod schema the frontend parses a response with accepts JSON captured from the real router, and drops nothing from it | `auto`: `internal/server/contract_fixture_test.go` captures into `testdata/api-contract/`, `frontend/src/schemas/contract.test.ts` validates it |
| H18 | A workspace or pane change cannot be written to `config.yaml` | The request fails with `500` and nothing changed: the workspace, pane and session are all still there, and the change is not silently persisted by the next successful write from another route | `auto`: `internal/api` — `TestConfigWriteFails_MutatingRoutesRollBackTheInMemoryConfig`, `TestDeleteWorkspace_ConfigWriteFails_KeepsTheWorkspaceAndItsSessions`, `TestDeleteSession_ConfigWriteFails_KeepsTheSessionUsable`; `internal/config` — `TestSnapshotRestore_UndoesRemoveWorkspace` and the rest of `snapshot_test.go` |
| H18b | The disk fills while `config.yaml` or the auth token file is being written | Neither file is left truncated: the previous `config.yaml` is still readable and unchanged, no partial token file is left where the next start would read one, and no temp file is left beside either | `auto`: `internal/config` — `TestWrite_FailureMidWrite_LeavesThePreviousConfigIntact`, `TestEnsureAuthTokenReportsAFailureMidWrite`; `internal/fileops` — `TestAtomicWriteReportsEachFailedStepAndNeverLeavesATempFile` |
| H18c | `config.yaml` is kept in a dotfiles repo and symlinked into `~/.config/panemux` | A save from the dashboard writes through the link: the symlink is still a symlink afterwards, the repo copy has the new contents, and no stray file is left beside the link | `auto`: `internal/config` — `TestWrite_SymlinkedConfig_WritesThroughTheLinkInsteadOfReplacingIt`, `TestEnsureAuthToken_SymlinkedTokenFile_WritesThroughTheLink` |
| H18d | The same link is in place before the repo has a `config.yaml` in it — the first save is meant to create one | The save creates the file the link points at, rather than replacing the link with a regular file holding the only copy. A relative link target resolves against the link's own directory | `auto`: `internal/config` — `TestWrite_SymlinkedConfigWithAMissingTarget_CreatesItThroughTheLink`, `TestResolveWriteTargetFollowsALinkWhoseTargetDoesNotExistYet` |
| H19 | Adding a workspace whose pane will not start, or that cannot be saved | The workspace is not left behind, and any session already created for it is closed rather than leaked | `auto`: `internal/api` — `TestPostWorkspace_CreateSessionFails_LeavesNoPhantomWorkspaceBehind`, `TestPostWorkspace_ConfigWriteFails_ClosesTheSessionsItHadCreated` |
| H19b | A Claude release changes how it encodes a project directory under `~/.claude/projects` | A local pane still finds its transcript through a bounded one-level fallback scan, and the mismatch is logged once per session instead of the worktree quietly disappearing from the pane header | `auto`: `internal/session` — `TestResolveClaudeTranscriptPath_FallsBackWhenClaudeEncodedTheDirDifferently`, `TestResolveClaudeTranscriptPath_LogsOneMismatchPerSessionRatherThanPerLookup`, `TestClaudeSessionCWDs_ReadsATranscriptFromAnUnexpectedProjectDir`, `TestClaudeProjectDirName_ObservedEncoding` |
| H19c | The same, on an `ssh` or `ssh_tmux` pane | The host is asked once where the transcript actually is, and its subagent transcripts are found under the same directory. A pane whose derived path works issues no extra command at all, a session with no transcript anywhere is probed once rather than every refresh, and a path the host reports that is not this session's own absolute transcript never reaches a command | `auto`: `internal/session` — `TestActiveRemoteWorkdir_DerivedTranscriptMissing_ProbesAndReadsTheRealOne`, `TestActiveRemoteWorkdir_DerivedTranscriptFound_DoesNotProbe`, `TestActiveRemoteWorkdir_ProbedDirIsReusedRatherThanProbedAgain`, `TestActiveRemoteWorkdir_ProbeFindsNothing_IsQuietAndBounded`, `TestActiveRemoteWorkdir_ProbeAnswerIsValidatedBeforeItReachesACommand`, `TestActiveRemoteWorkdir_ProbedDirAlsoResolvesSubagentTranscripts` |
| H19d | The transcript does not exist yet when the pane opens, or the probe itself fails | Neither becomes a permanent decision: a "nothing here" answer expires, so the transcript is found once it appears, and a probe that never ran is not recorded as an answer at all. A probe that finds the directory panemux already looked under is not reported as an encoding change | `auto`: `internal/session` — `TestActiveRemoteWorkdir_NothingFoundYet_IsRetriedOnceItGoesStale`, `TestActiveRemoteWorkdir_ProbeError_IsNotRememberedAsAnAnswer`, `TestActiveRemoteWorkdir_ProbeFindsTheDerivedDirectory_IsNotAnEncodingChange` |
| H20 | An SSH pane's peer stops answering mid-handshake — a bastion `ProxyCommand` that hangs while building its own tunnel is the exposed case | The handshake is abandoned after at most 30s and the restart fails with `500`, rather than blocking the pane forever. The bound holds on every transport, including the two whose `SetDeadline` cannot enforce one, and does not depend on the transport's own `Close` returning | `auto`: `internal/session` — `TestHandshakeWithTimeout_HangingHandshakeIsBoundedOnEveryTransport`, `TestHandshakeWithTimeout_ReturnsEvenWhenClosingTheTransportBlocks`, `TestDialSSHClientUntil_HangingHandshakeIsBounded` |
| H20b | The dial spent most of its retry budget before that happens, or the pane reaches its host through a ProxyJump chain | The handshake gets what is left of the shared budget rather than a fresh window of its own, so the whole restart stays inside the ceiling the retry budget documents however many hops it takes | `auto`: `internal/session` — `TestDialSSHClientUntil_HandshakeSharesTheDialBudget`, `TestHandshakeWithTimeout_NoBudgetLeft_FailsWithoutStartingTheHandshake` |
| H21 | Adding an SSH host without leaving the dashboard | The button beside the pane settings connection picker opens panemux's own dialog. The host it is given is appended to the user's ssh config on disk; a name that is missing, malformed or already taken is refused before anything is written. The dialog closes on success, and a write that fails keeps it open with the reason — including a failure carrying no message of its own — rather than losing what was typed. The refreshed list then offers the new alias to the pane it was added for | `auto`: the dialog and the dashboard wiring in `frontend/src/App.test.tsx`, `frontend/src/components/AddSSHHostDialog.test.tsx` and `frontend/src/hooks/usePaneSettings.test.ts`, all of which stop at the HTTP boundary; the write itself and its validation in `internal/api` — `TestPostSSHConfigHost_ValidHost_201` reads the file back, with `TestPostSSHConfigHost_MissingName_422`, `TestPostSSHConfigHost_InvalidNameChars_422` and `TestPostSSHConfigHost_DuplicateName_409` for the refusals; the route through the real router in `internal/server` — `TestServer_APIIntegration` |
| H21b | Pointing a pane at that alias | Saving the pane's settings restarts its session against the new connection, and a restart that cannot start the session leaves the old one usable rather than the pane dead | `auto` as far as the restart request: `frontend/src/hooks/usePaneSettings.test.ts` pins the restart policy field by field — a changed connection alone restarts, a changed title or board setting does not — and the save-then-restart order; `internal/api` — `TestRestartSession_Found_200`, `TestRestartSession_CreateFails_OldSessionStaysRegistered`, `TestRestartSession_CreateFails_500Body`. Reaching the host over that connection is the live SSH transport, which is `manual` — the same exclusion H14 records |

## I. Opening URLs from a pane

Added with [#177](https://github.com/tomo-chan/panemux/pull/177), which shipped
without rows — the omission this document's rule exists to prevent, so they are
written out here rather than quietly backfilled into another section. Two
mechanisms: a shell shim that intercepts `xdg-open`/`open` inside the pane, and
loopback port forwarding so an OAuth callback aimed at `localhost:<port>` on a
remote pane's host reaches the process waiting for it.

| # | Scenario | Expected | Verification |
|---|---|---|---|
| I1 | A program in the pane runs `xdg-open https://…` | panemux asks before opening anything; the URL is never opened automatically | `auto`: `frontend/e2e/pane-url-open.spec.ts` |
| I2 | The shim's own behavior | An `http`/`https` argument becomes an OSC sequence; anything else falls through to the real opener | `auto`: `internal/session` — `TestBrowserShimEmitsOSCForHTTPURLs`, `TestBrowserShimFallsThroughForNonHTTPArguments` |
| I3 | A `PATH` that still contains the shim directory | The shim refuses to exec back into itself | `auto`: `internal/session` — `TestBrowserShimDoesNotRecurseIntoItself` |
| I4 | No terminal, or no real opener installed | The shim falls through, or exits quietly — the pane's shell is never broken by it | `auto`: `internal/session` — `TestBrowserShimFallsThroughWhenNoTerminalIsAvailable`, `TestBrowserShimExitsQuietlyWhenNoRealOpenerExists` |
| I5 | A read-only home directory on the pane's host | Installing the shim fails and the pane still works | `auto`: `internal/session` — `TestNewLocalStartsEvenWhenTheShimCannotBeInstalled`, `TestRemoteBrowserShimSetupLeavesTheShellUsableWhenInstallFails` |
| I6 | `url_open.browser_shim: false` | No shim is installed and the pane's `PATH` is untouched | `auto`: `internal/session` — `TestNewLocalWithoutTheBrowserShim`, `TestSSHShellCommandWithoutTheBrowserShim`; root — `TestApplyBrowserShimSetting` |
| I7 | A crafted OSC sequence naming a `file:` or `javascript:` URL | Rejected by the scheme allowlist, on the frontend and independently in the backend | `auto`: `frontend/src/utils/paneUrlOpen.test.ts`; `internal/portforward` — `TestValidateOpenURL` |
| I8 | An SSH pane's OAuth callback URL | The callback port is republished on `127.0.0.1` on the panemux host, so the redirect reaches the remote CLI | `auto`: `internal/portforward` — `TestRegistryEnsureForwardsTrafficToTheDialer`, `TestCallbackPort` |
| I9 | A URL with no callback port, or a local pane | No forward is opened, and the reason says why | `auto`: `internal/api/openurl_test.go`; the route's own wiring in `internal/server` — `TestServer_APIIntegration` |
| I10 | A port already in use, or below 1024 | Refused rather than bound, with `409` for a port another pane holds | `auto`: `internal/portforward` — `TestRegistryEnsureRejectsUnforwardablePorts`, `TestRegistryEnsureRejectsPortHeldByAnotherSession`, `TestRegistryEnsureReportsPortAlreadyBoundOnTheHost` |
| I11 | The forward limits | At most 8 per pane and 32 in total | `auto`: `internal/portforward` — `TestRegistryEnsureEnforcesPerSessionAndTotalLimits` |
| I12 | An idle forward | Reaped after 30 minutes without traffic, but never while a connection is live | `auto`: `internal/portforward` — `TestRegistryReapsIdleForwardsAndKeepsUsedOnes`, `TestRegistryKeepsAForwardWithALiveConnectionPastTheTTL` |
| I13 | A pane is deleted, restarted, or the server shuts down | Every forward belonging to it is closed | `auto`: `internal/portforward` — `TestRegistryCloseSessionStopsOnlyThatSessionsForwards`, `TestRegistryCloseStopsEveryForward`; `internal/server` — `TestServer_ShutdownClosesPortForwards` |
| I14 | An OAuth flow completed end to end against a real remote host | The CLI in the pane receives its callback and completes login | `manual`: run a device-code login in an `ssh` pane, press `Open` when panemux asks, confirm the CLI reports success |

## J. The task dashboard

Added with issue [#252](https://github.com/tomo-chan/panemux/issues/252)'s stage 1: the agent
sessions on every host, by state, independently of panes. J15–J17 are
[#256](https://github.com/tomo-chan/panemux/issues/256)'s done and label records, J18–J21
[#257](https://github.com/tomo-chan/panemux/issues/257)'s starting and resuming, and J22–J25
[#258](https://github.com/tomo-chan/panemux/issues/258)'s summaries. The e2e rows run the real collection
script against the real `ps` and `tmux` of the machine, with a HOME holding what agents would have
written (`frontend/e2e/run-panemux-task-dashboard-e2e.sh`).

| # | Scenario | Expected | Verification |
|---|---|---|---|
| J1 | Switching between the workspaces and the task dashboard | `← Tasks` shows the dashboard over the workspaces, which stay mounted and inert; `Workspaces` returns. The back button counts tasks waiting for input. panemux opens on the workspaces | `auto`: `frontend/src/App.test.tsx`; `frontend/e2e/task-dashboard.spec.ts` |
| J1b | Switching with the keyboard | `Cmd/Ctrl+Shift+S` switches either way, also while a terminal pane has focus; `display.task_dashboard_shortcut` changes the letter, refuses anything but one letter and the palette's and board's `K` and `B`, and is not written into a config that never set it | `auto`: `frontend/src/App.test.tsx`; `frontend/src/utils/taskBoard.test.ts`; `frontend/e2e/task-dashboard.spec.ts`; `internal/config` — `TestValidate_TaskDashboardShortcut`, `TestSaveLayout_TaskDashboardShortcutIsWrittenOnlyWhenSet`; `internal/api` — `TestGetDisplay_ReportsTheEffectiveTaskDashboardShortcut` |
| J2 | Collection runs only while the dashboard is shown | Polls every 10s while shown and the page is visible; nothing is collected otherwise | `auto`: `frontend/src/hooks/useTasks.test.ts`; `frontend/src/App.test.tsx` |
| J3 | Running Claude Code sessions by state | `busy`, `waiting` and `idle` state files of a live claude process land in Working, Waiting for input and Idle, with the waiting reason and how long the state has lasted | `auto`: `internal/tasks` — `TestBuildTasks_LiveClaudeStatusMapsToState`, `TestBuildTasks_WaitingForIsOnlyReportedWhileWaiting`, `TestBuildTasks_StatusSinceFallsBackAndIsNeverInTheFuture`; `frontend/src/components/TaskDashboard.test.tsx`; `frontend/e2e/task-dashboard.spec.ts` |
| J3b | `/resume` inside a running Claude Code | The same card's process now runs the resumed session: it is listed as running there, and the session switched away from as stopped | `auto`: `internal/tasks` — `TestBuildTasks_ResumeSwitchesTheTaskInPlace`; the state-file behavior it relies on was observed on macOS (see the decision log) |
| J4 | A leftover state file | A file whose pid is gone, or now belongs to a process that is not claude — an editor or `tail` reading a file under `~/.claude` included — is not shown as running | `auto`: `internal/tasks` — `TestBuildTasks_StateFileNeedsALiveClaudeProcess`, `TestIsClaudeProcess` |
| J5 | An unreadable state file, and codex | Kept on the board as unknown while its pid is a claude process, and dropped once it is not; a running interactive codex process shows as running, and codex's non-interactive subcommands do not | `auto`: `internal/tasks` — `TestBuildTasks_UnreadableStateFileIsKeptAsUnknown`, `TestBuildTasks_UnreadableStateFileWithoutALiveProcess`, `TestBuildTasks_CodexProcessesAreRunningTasks`, `TestIsInteractiveCodex` |
| J5b | A running claude process with no state file | Shown as running with an unknown state, not as stopped, and its session is not listed a second time as stopped | `auto`: `internal/tasks` — `TestBuildTasks_ClaudeProcessWithoutAStateFileIsUnknownNotStopped`, `TestBuildTasks_UnreadableStateFileClaimsItsNewestLog`, `TestRunLocal_ReportsTheWorkingDirectoryOfAClaudeProcess` |
| J6 | Stopped sessions | Conversation logs of the last 7 days with no live process are listed as stopped, newest first, at most 50 per host, never twice and never alongside the same live session | `auto`: `internal/tasks` — `TestBuildTasks_TranscriptsWithoutALiveProcessAreStopped`, `TestBuildTasks_StoppedTasksAreCappedNewestFirst`, `TestBuildTasks_DuplicateTranscriptsAreListedOnce`, `TestRunLocal_CollectScriptRunsUnderShAndParses`; `frontend/e2e/task-dashboard.spec.ts` |
| J7 | Where a task runs | An agent under a tmux pane, however deep, is located in that tmux session; one outside tmux says so and offers no pane | `auto`: `internal/tasks` — `TestBuildTasks_LocationFollowsTheParentChainToATmuxPane`; `frontend/e2e/task-dashboard.spec.ts` |
| J8 | Opening a task that runs in tmux | Creates a `tmux` / `ssh_tmux` pane attached to its session in the active workspace, focuses and outlines it, and afterwards the card offers `Go to pane`; a task with a pane already attached switches to that pane's workspace | `auto`: `frontend/src/App.test.tsx`; `frontend/src/utils/taskBoard.test.ts`; `frontend/e2e/task-dashboard.spec.ts` (skipped where tmux is not installed) |
| J9 | One connection per SSH host | Reused across collections and never shared with a pane; a dropped one is redialed on the next collection; a failed dial waits 60s unless reconnected; a slow one reports `connecting`; a removed host is disconnected | `auto`: `internal/tasks` — `TestCollect_ReusesOneConnectionAndSendsTheScriptOnStdin`, `TestCollect_ATransportFailureDropsTheConnectionAndRedialsAtOnce`, `TestCollect_AFailedDialWaitsBeforeRetryingUnlessReconnected`, `TestCollect_ASlowDialReportsConnectingAndServesTheNextCollection`, `TestCollect_AHostRemovedFromTheConfigIsDisconnected`, `TestCollect_ATimedOutScriptOnAHealthyConnectionKeepsIt`, `TestCollect_ATimedOutScriptOnADeadConnectionDropsIt`; `internal/session` — `TestCommandConnRunReturnsStdoutOverARealExecChannel`, `TestCommandConnPing` |
| J10 | An unreachable host | Its chip shows the error and a `Reconnect` button; every other host's tasks are still shown | `auto`: `internal/tasks` — `TestCollect_HostsAreSortedAfterLocalAndFailuresStayPerHost`; `internal/api` — `TestGetTasks_ReportsEveryConfiguredHost`, `TestPostTaskHostReconnect`; `frontend/src/components/TaskDashboard.test.tsx` |
| J11 | Repository, branch and PR of a task | Looked up once per directory and cached, linked from the card and the detail panel; a stopped task shows its repository but not the directory's current branch or PR | `auto`: `internal/api` — `TestTaskGitInfo_LocalRepositoryWithPR`, `TestGetTasks_GitLookupsAreSharedPerDirectoryAndCached`, `TestGetTasks_StoppedTaskReportsItsRepositoryButNotBranchOrPR`, `TestTaskGitInfo_PRLookupStopsWithTheRequest`; `frontend/src/components/TaskDashboard.test.tsx` |
| J12 | Filtering and rows | Text filter over directory, branch, PR and session; rows by host, label or repository | `auto`: `frontend/src/utils/taskBoard.test.ts`; `frontend/src/components/TaskDashboard.test.tsx` |
| J12b | Another site's page requests the task routes | Refused with `403` before anything is collected, reconnected, recorded, started or resumed | `auto`: `internal/api` — `TestTaskRoutes_RefuseCrossSiteRequests`, `TestPostTask_Refusals`, `TestPostTaskResume_Refusals` |
| J12c | The palette or the board while the dashboard is shown | Their shortcuts do nothing, and one already open closes when the dashboard appears; pressing Open twice creates one pane | `auto`: `frontend/src/App.test.tsx` |
| J13 | The routes through the real router | `GET /api/tasks`, the reconnect route, `PUT /api/tasks/records`, `POST /api/tasks` and `POST /api/tasks/resume` answer through `server.New()` | `auto`: `internal/server` — `TestServer_APIIntegration` |
| J14 | A real SSH host running Claude Code | Its sessions appear under its name, and opening one attaches an `ssh_tmux` pane | `manual`: add the host under `ssh_connections`, start `claude` inside `tmux` there, open the dashboard, confirm the task's state, press `Open`, and confirm the pane shows the running agent |
| J15 | Marking a task done | `Mark done` asks first; a stopped task marked done moves to the Done column, which is hidden until `Done column` is checked; a task marked done that runs again stays in its state's column with a `Done` tag; `Mark not done` clears it; a task without a session ID offers neither | `auto`: `frontend/src/components/TaskDashboard.test.tsx`; `frontend/src/utils/taskBoard.test.ts`; `internal/api` — `TestGetTasks_CarriesEachTasksRecord`; `frontend/e2e/task-dashboard.spec.ts` |
| J16 | Labeling a task | Labels are added and removed in the detail panel and shown on the card; the board filters by a label and splits rows by label, a task with two labels in both rows and unlabeled tasks under "No label"; an invalid label is refused with its reason | `auto`: `frontend/src/components/TaskDashboard.test.tsx`; `frontend/src/utils/taskBoard.test.ts`; `internal/tasks` — `TestNormalizeLabels`; `internal/api` — `TestPutTaskRecord_Refusals`; `frontend/e2e/task-dashboard.spec.ts` |
| J17 | Done and labels survive a restart | Stored in `~/.config/panemux/tasks.json` per host, agent and session ID, kept while the session is off the list, removed from the file when cleared; a hand edit while panemux runs is read again rather than undone; a symlinked file is written through; a removed host's records can still be cleared; a file panemux cannot read is reported and never overwritten | `auto`: `internal/tasks` — `TestRecordStore_PutPersistsAcrossStores`, `TestRecordStore_KeysAreHostAgentAndSession`, `TestRecordStore_AnEmptyRecordIsRemoved`, `TestRecordStore_UnreadableFileIsReportedAndNotOverwritten`, `TestRecordStore_AFailedWriteChangesNothing`, `TestRecordStore_AFileEditedByHandIsReadAgain`, `TestRecordStore_AFileBrokenOrRemovedByHand`, `TestRecordStore_WritesThroughASymlink`; `internal/api` — `TestGetTasks_AnUnreadableRecordFileIsReportedNotFatal`, `TestPutTaskRecord_ARemovedHostsRecordCanStillBeCleared`; `frontend/e2e/task-dashboard.spec.ts` |
| J18 | Starting a new task | `New task` asks for a host, a directory, labels and a first instruction; claude starts in a detached tmux session `task-<8>` on the host with a session ID panemux minted, and no pane opens; the labels are recorded on that session; the dashboard says it is waiting and selects the task once it is listed; a form error, a refused label and a host refusal are each shown, and the form keeps what was typed | `auto`: `internal/tasks` — `TestLaunch_LocalRunsTheScriptOnThePanemuxHost`, `TestLaunch_RemoteRunsTheScriptOverTheHostConnection`, `TestLaunch_Errors`, `TestLaunchScript_HostRefusals`, `TestLaunchScript_FindsClaudeThroughTheLoginShellWhenPathLacksIt`; `internal/api` — `TestPostTask_LaunchesAndRecordsTheLabels`, `TestPostTask_LaunchedEvenWhenTheLabelsCannotBeSaved`, `TestPostTask_Refusals`; `frontend/src/components/NewTaskDialog.test.tsx`; `frontend/src/components/TaskDashboard.test.tsx`; `frontend/src/hooks/useTasks.test.ts`; `frontend/e2e/task-dashboard.spec.ts` |
| J19 | Resuming a stopped task | `Resume` on a stopped claude task runs `claude --resume=<id>` in the directory its log records, in a new tmux session `task-<8>` — or as a new window of a session of that name a recreated pane left, when no agent runs in it; only a session the host lists as stopped is resumed; two resumes of one task run one after the other, so the second is refused rather than starting a second claude; the task keeps its done and labels; each task's resume in flight disables only its own button; a refusal is shown | `auto`: `internal/tasks` — `TestResume_RunsClaudeForAStoppedTaskInItsRecordedDirectory`, `TestResume_Refusals`, `TestResume_AHostStillConnectingIsNotResumed`, `TestResume_ReusesTheTaskSessionOnlyWhenNoAgentRunsInIt`, `TestResume_OverlappingResumesOfOneTaskAreSerialized`, `TestResume_GivesUpWaitingForAnotherResumeWhenItsRequestEnds`, `TestLaunchScript_ASessionOfTheSameNameIsReusedOnlyForAPermittedResume`, `TestLaunchScript_RealTmuxResumeAddsAWindowToASessionLeftByAPane`, `TestLaunchScript_ResumeRunsClaudeWithTheSessionIDInTheEqualsForm`; `internal/api` — `TestPostTaskResume_ResumesAStoppedTask`, `TestPostTaskResume_Refusals`; `frontend/src/utils/taskBoard.test.ts`; `frontend/src/components/TaskDashboard.test.tsx`; `frontend/e2e/task-dashboard.spec.ts` |
| J20 | A hostile first instruction, directory or session ID | A directory holding `#` is where claude starts (never tmux's `-c`, which expands it); an instruction with a leading option, substitutions, quotes or a heredoc-looking line reaches claude as one argument after `--`, never a shell or tmux's arguments, and its temp file is removed; a session ID that is not a UUID, a directory with a shell character, and an oversized or empty instruction are refused before anything runs | `auto`: `internal/tasks` — `TestLaunchScript_NewTaskHandsThePromptToClaudeAsOneArgumentAfterTheEndOfOptions`, `TestLaunchScript_RealTmuxStartsClaudeInADirectoryHoldingAHash`, `TestBuildLaunchScript_RefusesInputBeforeAnythingRuns`, `TestParseLaunchOutput`; `internal/session` — `TestValidateRemotePath`; `frontend/e2e/task-dashboard.spec.ts` |
| J21 | Starting and resuming with a real Claude Code, locally and on an SSH host | The first instruction is sent as claude's first message, the session's state file and log carry the minted ID (so the labels show on the task), `/exit` ends the tmux session and the task shows as stopped, and `Resume` reopens the same conversation | `manual`: on the panemux host and on an `ssh_connections` host with a signed-in `claude`, start a task whose instruction begins with `--`, open it and confirm the instruction arrived as a message rather than as help output; confirm `~/.claude/projects/<dir>/<session ID>.jsonl` exists for the ID the dashboard shows and no `panemux-task.*` file remains under `$TMPDIR` or `/tmp`; `/exit`, confirm the task is Stopped, press `Resume`, and confirm the conversation continues in `task-<8>` |
| J22 | Summaries of a task's work | With `task_dashboard.summary.enabled`, a waiting, idle or unknown task is summarized by the poll and a stopped one when selected; the card shows the summary and `Next: … · n left`, the detail panel the summary and the remaining work; a summary is reused until the log's modification time or size changes, a busy task keeps its last one marked outdated, a failure is not retried until asked, at most two run at once; off by default, when nothing is summarized and the panel says so | `auto`: `internal/tasks` — `TestSummaries_RunningTasksThatAreNotBusyAreSummarized`, `TestSummaries_AreReusedUntilTheLogChanges`, `TestSummaries_ABusyTaskKeepsItsLastSummaryMarkedOutdated`, `TestSummaries_PendingWhileRunning`, `TestSummaries_AFailureIsNotRetriedUntilAskedOrTheLogChanges`, `TestSummaries_RunAtMostTwoAtOnce`, `TestSummaries_ReadARemoteLogOverTheHostsConnection`, `TestSummaries_AreForgottenWithTheirTask`, `TestRequestSummary`, `TestBuildTasks_ClaudeTasksCarryTheirLogVersion`; `internal/api` — `TestGetTasks_SummariesAreOffUnlessEnabled`, `TestGetTasks_SummarizesAnIdleTask`, `TestPostTaskSummary`, `TestPostTaskSummary_Refusals`, `TestPostTaskSummary_RefusesCrossSiteRequests`; `internal/config` — `TestTaskDashboardSummary_IsOffUnlessEnabled`; `frontend/src/components/TaskDashboard.test.tsx`; `frontend/src/hooks/useTasks.test.ts`; `frontend/src/utils/taskBoard.test.ts` |
| J23 | A done candidate | A current summary with nothing remaining shows `Done?` on the card and a line in the detail head; it is only shown, and a task marked done is not called a candidate | `auto`: `internal/tasks` — `TestSummaries_AnswerWithNothingRemainingIsADoneCandidate`; `frontend/src/components/TaskDashboard.test.tsx` |
| J24 | What claude is given, and how it runs | Only the text of user and assistant messages — the first user message and the newest 24 KiB — never tool calls, tool results, thinking, attachments or a subagent's messages; a log in which nothing is understood is `unreadable` and nothing is sent; claude runs without the operator's settings, MCP servers or slash commands, with every acting tool denied, the excerpt on stdin, and none of its own text passed on when it fails | `auto`: `internal/tasks` — `TestBuildExcerpt_KeepsOnlyConversationText`, `TestBuildExcerpt_NothingReadableIsNotSummarized`, `TestBuildExcerpt_HeadAndTail`, `TestBuildExcerpt_IsBounded`, `TestSummaries_AnUnreadableLogIsNotSummarized`, `TestSummaryArgs`, `TestClaudeSummarizer_RunsClaudeWithTheExcerptOnStdin`, `TestClaudeSummarizer_Failures`, `TestParseSummaryOutput_Errors`, `TestBuildTranscriptScript`, `TestRunLocal_TranscriptScriptReadsTheLog`, `TestRunLocal_TranscriptScriptSendsHeadAndTailOfALargeLog` |
| J25 | Summaries with a real Claude Code, locally and on an SSH host | A waiting task's card shows a summary in the conversation's language within a poll or two of it starting to wait, and selecting a stopped task summarizes it | `manual`: set `task_dashboard.summary.enabled: true`, sign in to `claude` on the panemux host, leave a claude task waiting for input on the panemux host and on an `ssh_connections` host, open the dashboard and confirm both cards show a summary and `Next: …`; select a stopped task and confirm its detail panel shows `Summarizing…` and then the summary |

## Not covered

Stated explicitly, because an absent row reads as an oversight and these are decisions:

- **Remote (`ssh` / `ssh_tmux`) board panes have no e2e scenario.** All three board fixtures use
  `type: local`. The remote paths are unit-tested (C7) but never walked end to end, because doing so
  needs a second host the suite cannot assume.
- **The command center's real `claude` binary is only exercised manually** (E7). The e2e fixture
  stubs the binary, deliberately: the shipped argv is pinned against the real CLI's documented
  behavior in `internal/commandcenter/runner_test.go`, and a stub cannot reproduce that parsing.
- **The agmsg contract tests stay opt-in locally** (C6, G2–G8). They need a real agmsg install and
  `sqlite3`, so running them inside `make check` would stop it being hermetic. CI does run them
  (G9/G10), which is where the drift signal comes from; a contributor without agmsg installed sees
  them skip.
- **Nothing verifies the contract against a *remote* agmsg install.** `RemoteAgmsgClient` builds and
  escapes its commands identically and is unit-tested, but Tier 2 only ever drives the local client,
  for the same reason C7's remote paths have no e2e row: a second host cannot be assumed.
- **Install scenarios A1/A2 are manual.** CI builds the binary on every PR, but nobody automatically
  downloads a release artifact and runs it.
- **Every row in section F is manual.** Documentation accuracy is not mechanically checkable here.
- **Real external session endpoints are manual** (H14). The SSH protocol lifecycle is automated
  against an in-process `x/crypto/ssh` server, and the local-tmux lifecycle uses an injected command
  behind a real PTY, so neither needs a reachable host or tmux install in `make check`. What remains
  manual is compatibility with an operator's actual SSH server, shell, tmux binary/server and OS PTY
  behavior — the environment adapters the `Makefile` records next to `COVERAGE_PKGS`.
- **The task dashboard against a real remote host is manual** (J14), for the same reason as H14:
  the SSH connection and the script are automated against an in-process server and the local host,
  but not against an operator's own SSH server with Claude Code running on it.
- **Summaries with a real Claude Code are manual** (J25). The argv was run once against claude 2.1.283
  while this was built and answered in the conversation's language, but `make check` cannot sign in
  to claude, so every automated row uses a stand-in for it.
- **Starting and resuming with a real Claude Code is manual** (J21). The launch script runs for real
  under `sh`, and the e2e suite runs it under a real tmux, but claude is a stand-in in both: the
  development environment's claude stopped at its first-run screen, so whether the first instruction
  becomes the first message and whether the state file carries the minted ID were not observed.
- **An end-to-end OAuth flow against a real remote host is manual** (I14). The forward itself, the
  scheme allowlist and the limits are all unit-tested, but a real device-code login needs a real
  provider and a second host.
- **Performance is measured, not verified.** `make bench` reports terminal throughput,
  replay-buffer cost and the relay's polling cost, and asserts no threshold — the `publish` rows
  move by up to 2.9× between runs of the same binary on the same machine, so a threshold chosen
  from them would fire on container noise. That is not a scenario row: there is no expected outcome
  to state yet. See [quality-gateway/measurements.md](quality-gateway/measurements.md). The one
  exception is where a performance finding turned out to be about work done rather than time taken:
  the replay buffer's steady-state cost is asserted as an allocation count in `internal/session`,
  which has none of a timing's spread. H15b is the row it sits under.
- **Accessibility is held at a ceiling, not at zero.** `frontend/e2e/a11y.spec.ts` scans the
  dashboard and a modal dialog with axe-core and fails when a violation count rises above the
  currently-recorded one (the ceiling and the comparator are in `frontend/e2e/a11y-ceiling.ts`,
  unit-tested by `frontend/e2e/a11y-ceiling.test.ts`); the recorded counts are violations this
  repository still has, not an approved state. It is not a scenario row either, for the same
  reason — the expected outcome is "no worse than today", which is a gate's statement rather than a
  use case's. See [quality-gateway.md](quality-gateway.md)'s "Accessibility" for the ceiling and
  for how to lower it after a fix.

## Checking this ledger

`make check-scenarios` (part of `make check`) resolves every path and Go test name an `auto` row
names, and fails when one does not exist. The rule above says a silently absent row is not a
legitimate answer; this closes the failure that rule never anticipated — **a row that names a test
which has since been renamed, moved or deleted**. Such a row reads as coverage and is worth nothing,
and it is the most likely kind of rot in a living ledger, because nobody grepping for a test name
expects to find it in a markdown table. It found one the day it was written: C7 named
`..._TransportError_DistinctFromNo`, and the test is
`TestBootstrapWatcher_RemotePresenceCheckTransportError_DistinctFromNo`.

CI additionally fails a pull request that touches `frontend/src`, `internal/api` or
`internal/config` without touching this file. Apply the `scenarios-exempt` label to a change that
genuinely alters no use case.

## Related documents

- Developer workflow and the TDD rules these scenarios sit on top of: [../DEVELOPMENT.md](../DEVELOPMENT.md)
- Agent Board design, including its own testing plan and agmsg compatibility contract: [agent-board.md](agent-board.md)
- Behavior and API specification: [behavior.md](behavior.md)
- Security requirements: [security.md](security.md)
