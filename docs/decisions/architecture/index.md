# Architecture decisions

## Decisions

* [Domain config and file context use separate types (2026-09-20, issue #66, PR #240)](domain-config-context.md) - Separate serializable configuration from load context to prevent duplicated serialization fields dropping sections.
* [Filesystem globals use named seams (2026-09-10 through 2026-09-13, PRs #224, #231, #234)](filesystem-seams.md) - Provide named home, cache and persistent-write seams instead of unrelated process-global test overrides.
* [Loopback callbacks use a panemux-host forward (2026-08-23, PR #177)](loopback-callback-forward.md) - Forward remote loopback login callbacks on demand without exposing a general proxy or requiring manual SSH tunnels.
