# Agent Registry decisions

## Decisions

* [Agent availability: a separate probe and an anti-corruption layer for formats (2026-10-16, issue #373)](availability-probe.md) - Probe CLI availability separately in the background and degrade unknown formats instead of guessing state.
* [Board participation steps belong to agent adapters](board-participation-adapters.md) - Historically place per-agent identity steps behind checked adapters before participation moved outside panemux.
* [The dashboard on the agent capabilities (2026-10-23, issue #376)](capability-driven-dashboard.md) - Drive dashboard actions from server capabilities while preserving pane operations for unknown agents.
* [Collection is composed from per-agent collect adapters (2026-10-16, issue #373)](collect-adapters.md) - Compose constant per-agent collection fragments and reject missing sections instead of silently losing sessions.
* [Launch and resume through per-agent adapters (2026-10-18, issue #374)](launch-resume-adapters.md) - Separate launch and resume capabilities while sharing fixed-script prompt and directory handling across agents.
* [Preserve records for agents this build does not list](open-world-records.md) - Preserve unknown-agent records as original JSON without displaying them or blocking known-agent writes.
* [Pane workdirs per session, on the Task rule (2026-10-18, issue #375)](session-workdirs.md) - Share task process rules and parsers with pane lookup without coupling observations or failure handling.
* [Capabilities in the task snapshot (2026-10-23, issue #376)](snapshot-capabilities.md) - Return registry, availability and actions together instead of reconciling a separate agent endpoint.
* [One registry declares agents; capabilities are ports in their consumers (2026-10-12, issue #372)](static-agent-registry.md) - Use explicit static declarations and consumer-owned ports instead of configuration or implicit linking order.
* [Summary adapters (2026-10-24, issue #347)](summary-adapters.md) - Separate transcript sources from summary runners while preserving cache versions and unknown-agent answers.
* [A third agent behind a build tag (2026-10-24, issue #377)](tagged-fixture-agent.md) - Verify extensibility with a tagged third agent and isolated processes instead of production collection switches.
