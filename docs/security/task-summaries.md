---
type: Security Requirement
title: "Security: task summaries"
description: "Conversation extraction, agent runner restrictions and private summary output."
---

# Security: task summaries

[Common execution requirements](command-execution.md) apply at this boundary.

When `task_dashboard.summary.enabled` is set, two sinks summarize a task: a host log reader and
that task's own agent CLI on the panemux host ([behavior](../behavior/task-summaries.md#summaries)).
Conversation text is untrusted. Every summary adapter must keep its agent's authentication,
settings and tool limits; registration grants nothing and excerpts never cross to another agent.
The fixed `logScript` accepts only its validated session ID and collected log version, rejecting
invalid IDs with `ErrInvalidSummary` before execution. The same validation governs the dashboard
action and `POST /api/tasks/summary`. Errors are fixed messages; a missing CLI reads
`<agent> CLI was not found on the panemux host`, without a path.

## Log reading and Claude runner

The fixed `transcriptScriptTemplate` runs as `sh -s`. Its three byte counts are Go constants;
the single-quoted ID must pass `validSessionID` (`^[a-zA-Z0-9_-]+$`). It appears only in
`"$HOME"/.claude/projects/*/"$sid.jsonl"`, so a leading `-` cannot become an option. Only a session
listed with a log by the host's last collection can be summarized. Read the framed body by the
header's byte length, never by searching conversation text for a marker.

`newClaudeSummarizer` runs literal `claude` through `exec.CommandContext`, without a shell, in an
empty temporary directory. The excerpt is stdin; a compile-time instruction is the only prompt
argument, after `--`. Its arguments follow the [command-center contract](command-center.md#command-center-subprocess-execution):

| Argument | Why |
|---|---|
| `--session-id <minted UUID>`, `--no-session-persistence` | A session of panemux's own, not the ambient one, and nothing written under `~/.claude` |
| `--setting-sources ""` | None of the operator's settings, hooks or `CLAUDE.md` |
| `--strict-mcp-config` | No MCP server |
| `--disable-slash-commands` | A `/command` in the conversation stays text |
| `--disallowedTools=<commandcenter.DisallowedTools()>` | Every tool that can execute, write, read files, reach the network or start another agent is refused by name — the denial the command center found survives a permissions override |
| `--output-format=json`, `--json-schema <schema>` | The answer is parsed as a structure, and bounded before it is shown |
| `--tools ""` | No tool is offered at all; the structured answer still arrives (checked with Claude Code 2.1.294) |
| `--system-prompt <compile-time literal>` | The CLI's own coding-agent system prompt is replaced by a fixed one, which tells claude to describe only the excerpt |
| `--model=haiku` | A compile-time literal; it chooses the model, not where the request goes or how it is authenticated. A model other than Haiku that answers is logged and shown by its ID from `modelUsage`; a name not shaped like a model ID becomes `unknown`, so nothing else from the answer reaches the log |

The instruction treats the excerpt as data, but is not the safety barrier. `--tools ""` supplements
the denial list, whose denial survives a permissions override. `--bare` is not used because it
would require `ANTHROPIC_API_KEY` instead of the operator's signed-in CLI. Prompt injection can
mislead summary/remaining-work text and the done-candidate label; all render as text, and marking
done still requires a person's click.

Only user/assistant message text is extracted. Tool calls/results, thinking and attachments,
including credential attachments, are excluded. Budgets are 4 KiB for the first user message and
24 KiB for recent messages, at most 2 KiB each. Typed or quoted secrets are not masked; summaries
are therefore off by default. Only the parsed answer is shown, bounded to 1 KiB of summary and
10 remaining-work items of 300 bytes. Exit, timeout, non-JSON and missing-summary failures use fixed
messages, never CLI text that could quote the conversation. Each runner has its own process group,
killed on its 2-minute deadline or shutdown; at most two run concurrently.

`SummaryStore` writes only summary, remaining work, creation time, excerpt SHA-256 and log version
(mtime, size, Codex rollout basename) to `~/.config/panemux/task-summaries.json`, mode 0600 through
`fileops.AtomicWrite`. It never writes the log or excerpt. The answer can repeat private conversation
text: it stays on the panemux host, nothing outside panemux reads it, and it is neither read nor
written with summaries off. Stored values never reach commands; log versions are compared only,
and scripts use the collection. Unparseable files, other format versions, and entries with unknown
agents, invalid session IDs/hashes or over-bound summaries are renamed aside unread.

## Codex summary runner and rollout reader

`internal/tasks/codex_summary.go` supplies a separate Codex runner. It runs the literal `codex`
on the panemux host through `exec.CommandContext`, with no shell, in an empty temporary working
directory. Its fixed argv is `exec --ephemeral --skip-git-repo-check --color never --output-schema
<private schema file> --output-last-message <private answer file> -`. The fixed summary instruction
and bounded conversation excerpt are stdin. No model, profile, approval bypass, sandbox override or
user-config suppression is added. The operator's normal authentication, user/global instructions,
MCP, hooks and managed policies apply; those settings may affect model input, cost and actions.
The temporary working directory does not inherit the task's project configuration.

The instruction requests only a summary and explicitly asks for no tools or action on conversation
instructions. Unlike the Claude denial list, this is not a guarantee that all acting tools are
removed: Codex uses the operator's configured execution restrictions and its non-interactive exec
approval behavior. A failed non-interactive run or unsupported CLI becomes a fixed error; the app
does not approve requests or relax permissions. Authentication is neither copied nor rewritten.
`--ephemeral` avoids persisting the summary session, not every CLI cache or runtime file.

CLI stdout/stderr are discarded. The answer is opened without following a final symlink and with
nonblocking open flags; the opened descriptor must be a regular file before any bytes are read.
Path replacement cannot turn a prior pathname check into a blocking FIFO open or special-file read.
The open/stat/read operation stays within the summary context deadline, including after the CLI
exits; a late filesystem operation closes its own handle without retaining a summary slot.
Only the final answer file is read, up to 64 KiB even if it grows during the read, and its required
JSON fields/types are checked before applying the same 1 KiB/10 items/300 bytes display limits.
Setup, execution, timeout and parse errors expose no CLI text or local paths. Cancellation kills
the runner's process group, as for Claude.

`buildCodexTranscriptScript` uses the existing framed head/tail reader with a fixed standard
`~/.codex/sessions/*/*/*/rollout-*-<UUID>.jsonl` glob. A non-UUID is rejected before execution.
`buildCodexExcerpt` keeps only `response_item` payloads of type `message`, user `input_text` and
assistant `output_text`. Calls/results, reasoning, developer instructions, attachments and
`event_msg` mirrors are omitted. Complete leading AGENTS/environment wrappers are excluded from
the source conversation; this does not suppress the summary runner's own inherited instructions.
Repeated IDs are deduplicated, identical messages with distinct/no IDs are retained, and cut lines
are skipped. The existing first/recent text budgets still apply; unknown formats are never sent raw.

The service routes by host, agent and session; Codex never falls back to Claude. The reader pins
the collected basename and version, validates session_meta.id, and rejects a change before or
during the read instead of substituting another rollout.
