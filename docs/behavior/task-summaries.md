---
type: Specification
title: "Task summaries"
description: "Summary scheduling, conversation excerpts, persistence, and agent-specific behavior."
---

# Task summaries

Part of the [task dashboard](tasks.md).

## Summaries

Summaries are off by default. `task_dashboard.summary.enabled: true` opts into sending conversation
excerpts to the task agent's account on the panemux host. Claude uses `claude -p`, Codex `codex exec`;
a task never falls back to another agent. Eligibility requires a summary adapter, an accepted session
ID (UUID for Codex), and a collected log (newest 100 per host within 7 days); pid-only tasks have none.

### Reading and privacy

Logs are read on the task's host with a fixed script: the whole file up to 2.25 MiB, otherwise its
first 256 KiB and last 2 MiB. Claude logs are `~/.claude/projects/*/<session ID>.jsonl`; duplicates
use collection order, newest modification time then larger size for same-second writes.
Claude accepts JSON-object lines with `type` `user` or `assistant`, `message.content` a string
or `text` blocks, excluding `isSidechain` subagent lines. Only conversation text is sent, excluding tools, thinking, attachments,
sidechains and cut lines: first user message up to 4 KiB, newest messages up to 24 KiB total and
2 KiB each. Typed or quoted secrets are not masked. Remote excerpts go to the panemux host account,
never the remote account; only answers are saved, never logs/excerpts.
[Summary security](../security/command-execution.md#task-summaries) owns extraction limits,
CLI isolation and exact argument rules.

No supported conversation lines means `unreadable`: no raw-log fallback, no CLI request and no
further read until the log changes. The button stays disabled; a changed log marks the result
outdated and enables it, and selecting a stopped/unknown task requests it again.

### Scheduling and results

| Trigger/state | Summary action |
|---|---|
| Collection finds `wait` or `idle` with a new log version | Automatic request |
| `busy` or `unknown` during collection | Retain last summary, marked outdated; no automatic request |
| Stopped or `unknown` task selected without current summary | Request |
| Summarize / Summarize again | Request for any eligible task; retries failures |

A summary is keyed by (host, agent, session), current while log modification time, size and Codex
rollout name match. When a due summary rereads a changed log, an unchanged excerpt hash (including
agent and summarizer reader/runner version) makes it current without a CLI run. Tool-only log
changes can therefore reuse it. A different summarizer version is shown but never current.
Polling never retries a failure for the same log; the button does.
At most two summaries run across hosts; reads have 15 seconds and CLI runs 2 minutes.
Off-list tasks retain summaries, returning when listed again; entries expire 30 days after last
listing, with least-recently-listed evicted beyond 1000. Removing a host deletes its summaries.

Answers use the conversation's language: one or two sentences up to 1 KiB and immediate-first
remaining work, at most ten items of 300 bytes (longer items end in `…`). A ready, current answer
with no remaining work is a done candidate, never an automatic done record.
Claude selects the CLI's Haiku alias; an account disallowing it may answer with its default model.
The answer is kept and that model shown; the server logs only its ID once per model, malformed
IDs as `unknown`. See [cost measurements](task-summary-cost.md).
Failure exposes fixed messages for exit, timeout, non-JSON or missing summary, never CLI output.
A missing CLI reports `<agent> CLI was not found on the panemux host`, without a path.

### Saved summaries

Summaries are saved in `~/.config/panemux/task-summaries.json` on the panemux host (mode `0600`,
replaced atomically, a symlink written through), so a restart shows them again without reading a log
or asking an agent.

- **What is saved.** For each summary: its host, agent and session ID, the text, the remaining work
  and when it was made, when its task was last listed (saved at most once a day), the SHA-256 of the
  excerpt it was made from with the summarizer's version (its adapter's reader and runner), and the log's modification time, size and
  Codex rollout name it was last found current for. Never the log or the excerpt. A summary still
  running, a failure and an unreadable log are not saved, so a restart shows neither `pending` nor
  `error`: a task summarizing when panemux stopped shows the summary before it.
- **After a restart.** A saved summary whose log is unchanged is current; one whose log changed is
  outdated and is made again, or found current by its hash, under the usual rules.
- **When summaries are off** the file is neither read nor written, and is left as it is.
- **A summary of an agent without a summary adapter** — one whose adapter was removed — is not shown,
  is written back unchanged, and is dropped 30 days after its task was last listed, like any other.
  The file stays usable.
- **A file that cannot be used** — not JSON, a format version other than `1`, or an entry panemux would
  not have written, such as an empty summary, a malformed agent ID or a session ID the agent's
  adapter refuses — is renamed to `task-summaries.json.bad-<UTC time>`
  (with `-1`, `-2`, … added when an earlier file moved aside in the same second holds that name;
  once 100 names in one second are taken, the file is treated as one that cannot be moved aside),
  unchanged, the server log says so, and saving starts again from an empty file. Edits made to the file while panemux runs are
  overwritten by its next save.
- **A file that cannot be read or moved aside** is not written over. Summaries are made and shown from
  memory, the server log says why once, and `GET /api/tasks` reports it in `summaries_error`, which
  the dashboard shows above the task list. The file is tried again at the next poll.
- **A save that fails** keeps the summaries in memory, is logged and reported in `summaries_error`
  until a later save succeeds. A save of an older set that finishes after it is dropped, so the file
  never goes back to lacking the newer summaries while reporting itself healthy.

### Codex summaries

Codex tasks use `codex exec` on the panemux host with the operator's normal authentication,
model, user/global/managed settings, MCP and hooks, in an empty temporary working directory with
an ephemeral session. No profile, permission bypass or configuration suppression is added.
Inherited settings can affect input, cost and permitted actions: a summary-only prompt does not
make this a tool-free runtime. A run needing intervention or unsupported flags fails with a fixed
error, without CLI text or fallback to another model or agent. The
[Codex security contract](../security/command-execution.md#codex-summary-runner-and-rollout-reader)
owns exact argv, answer-file validation and deadline enforcement (including after CLI exit).

The [Codex rollout reader](../security/command-execution.md#codex-summary-runner-and-rollout-reader)
owns canonical message extraction, injected-wrapper removal, ID deduplication and pinned-log
validation; empty or fully stripped messages do not consume an ID, so a later message with that
ID can supply conversation text. It uses the same first/recent budgets as Claude. Collection chooses modification time,
numeric size, then descending filename; a changed/missing pinned log fails without substitution.
Only the standard `~/.codex/sessions` root is read: an open custom-root rollout can be collected,
but fails summary reading if absent there. Scheduling follows the shared rules above. Remote
excerpts go to Codex on the panemux host, never Claude or the remote account.
