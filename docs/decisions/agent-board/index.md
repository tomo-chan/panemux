# Agent Board decisions

## Decisions

* [Agent Board uses agmsg only (2026-08-08, PR #162)](agmsg-only.md) - Use agmsg alone instead of maintaining a second panemux-owned messaging database protocol.
* [Contract incidents favor behavior over environment and wording (2026-08-23 onward, PR #176)](behavioral-contract-assertions.md) - Test message delivery and lock behavior rather than process ancestry or diagnostic wording.
* [Browser clients obtain the board token from a dedicated bootstrap endpoint (2026-08-14, PR #170)](board-token-bootstrap.md) - Bootstrap browser tokens through a dedicated local endpoint outside the authenticated board subtree.
* [Command-center subprocess arguments are structurally separated (2026-08-14, PR #170)](command-argument-separation.md) - Separate CLI options from prompt operands after real-CLI probes exposed flag parsing defects.
* [Command center uses panemux's API, not agmsg scripts (2026-08, PR #162)](command-center-api.md) - Expose narrow authenticated board operations instead of making the model compose shell-safe script calls.
* [Command center pins its own execution context (2026-08-22, PR #171)](command-center-context.md) - Isolate command-center sessions, settings, directories and permissions from the ambient interactive CLI.
* [Remote message bodies use an encoded wrapper argument (2026-08, PR #162)](encoded-message-wrapper.md) - Encode remote message bodies across the shell boundary because agmsg only accepts positional body arguments.
* [Global agmsg automatic updates (2026-10-31, #435 B3)](global-agmsg-updates.md) - Update only older installed releases on global opt-in events without periodic monitoring or automatic retry.
* [Global board participation and installer split (2026-10-29, #435 / #438)](global-board-participation.md) - Replace pane switches with instance-wide opt-in while preserving the existing same-user trust boundary.
* [Host tool management and the Codex hook adapter (2026-10-26, #438 / #440)](host-tool-port.md) - Move checks and installation to host management while preserving Codex settings and API behavior.
* [Dashboard capability is independent from command-center capability (2026-08-22, PR #171)](independent-board-capability.md) - Represent dashboard enablement separately so it can run independently of the command center.
* [Manual installer diagnostics and non-release versions (2026-10-31, #435 review)](installer-diagnostics.md) - Preserve failure output and treat describe, dirty and prerelease versions as unknown rather than installable releases.
* [Split installer work into three PRs (2026-10-30, #435 / #438)](installer-pr-sequence.md) - Split execution infrastructure, manual installation and automatic updates into sequential reviewable slices.
* [The own-send ledger is the model-checking pilot (2026-09, PR #243, issue #168)](ledger-model-pilot.md) - Pilot model checking on the smallest ledger state machine after a multiset-versus-set incident.
* [Live pane eligibility after configuration edits (2026-10-29, #435 / PR #447 review)](live-pane-eligibility.md) - Refresh routing authority after configuration edits instead of pinning pane membership to startup state.
* [Isolate the board message transport (2026-10-26, #438 / #441)](message-transport-port.md) - Keep relay orchestration independent of agmsg scripts, parsing and provider-specific participation steps.
* [Manual pinned agmsg installation (2026-10-30, #435 B2)](pinned-agmsg-install.md) - Verify pinned source for offline reuse and warn on differing releases, including newer patches.
* [Compatibility is checked against a real agmsg release (2026-08-23, PR #176)](real-agmsg-contract.md) - Check captured CLI assumptions against a real pinned installation and daily latest-release canary.
* [2026-11-07: retire Agent Board PTY onboarding (#503, #495 S1)](retire-pty-onboarding.md) - Remove generated participation text from terminal input and delegate identity and reception setup to agmsg.
* [Same-project panes claim an agmsg actas lock (2026-08-22, PR #171)](same-project-identity-claim.md) - Historically narrow same-project watchers with identity claims so panes do not subscribe to each other.
* [The own-send ledger authenticates sentinel-attributed rows (2026-08, PR #162)](sentinel-send-ledger.md) - Authenticate system-attributed rows against recent panemux sends instead of trusting free-text sender names.
* [One host identity and shared execution (2026-10-25, #438 / #439)](shared-host-execution.md) - Share execution by configured host identity while isolating cancellation and rejecting removed memberships.
* [Keep source data separate from diagnostics (2026-10-31, #435 review round 2)](source-output-separation.md) - Separate source stdout from diagnostics so Git warnings cannot corrupt revisions or archive bytes.
* [Automatic-update event delivery (2026-10-31, #435 B3 review)](update-event-delivery.md) - Coalesce confirmation events during active checks and schedule new hosts without adding a periodic watcher.
* [Scope automatic-update success to its host run (2026-10-31, #435 B3 review)](update-result-lifetime.md) - Scope installer outcomes to their worker run and host identity to prevent stale success claims.
* [Usage remains outside the Agent Board status contract (2026-08)](usage-contract-boundary.md) - Keep provider account usage outside cooperative status while leaving direct transcript usage reading separate.
* [Version coverage normalizes install provenance (2026-08, PR #176)](version-provenance.md) - Historically normalize provenance and tolerate later patches before stricter pin checks replaced that warning policy.
