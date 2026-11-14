---
type: Specification
title: "Task metadata and records"
description: "Repository and issue metadata, autolinks, and persistent done and label records."
---

# Task metadata and records

Part of the [task dashboard](tasks.md).

## Repository, branch, pull request, issues and references

Git metadata comes from `git` on the task's host and `gh pr view` on the panemux host, naming
remote repositories from origin URLs. It is cached per (host, directory) for 30 seconds; failed
hosts, non-repositories and uninspectable directories have no `git`. Abandoned requests stop gh
and never cache their stopped lookups. Only running-task directories query PRs; a cached git-only
result for stopped tasks is refreshed with a PR lookup if a running task appears there.
Metadata describes the directory's current checkout. Stopped tasks report only `repo` and
`repo_url`, never the branch, PR, issues or references of a later checkout.

- **Issues** are the ones the pull request closes: the same `gh pr view` call reads
  `closingIssuesReferences` along with the pull request's URL, number and title, so no further
  command runs. No pull request means no issues. An issue whose URL is not `http`/`https` or whose
  number is not positive is left out. Issue titles are not shown. The field needs `gh` 2.72.0 or
  later. An older `gh` refuses the whole call (`Unknown JSON field`), so the pull request is looked
  up again with only its URL and number, as a pane header does: the PR link stays, and the task has
  no issues and no references from the PR title.
- **References** are what `task_dashboard.autolinks` finds in the branch name and then the pull
  request title. Each entry works like a GitHub repository's autolink reference: its `key_prefix`
  followed by an identifier links to its `url_template` with every `<num>` replaced by the
  identifier. With `key_prefix: JIRA-` and `url_template: https://jira.example.com/JIRA-<num>`,
  `JIRA-123` links to `https://jira.example.com/JIRA-123`.
  - The identifier is digits, or with `is_alphanumeric: true` the letters `A`–`Z` in either case,
    digits and `-`, taking as many as follow the prefix. An alphanumeric identifier therefore runs
    on through a branch name's words: `TICKET-12-retry` gives `12-retry`.
  - The prefix matches exactly as written, case included, and not when an ASCII letter or digit
    comes directly before it. A numeric identifier does not match when a letter or digit follows
    it. `JIRA-418-retry-backoff` gives `JIRA-418`; `xJIRA-418`, `JIRA-418a` and `jira-418` give
    nothing.
  - Every reference is listed once, in the order found, with the text that matched (`JIRA-123`).
  - Only the configured prefixes match, so text that merely looks like a ticket key (`UTF-8`,
    `CVE-2024-45337`) is not linked unless its prefix is configured.
  - Without `task_dashboard.autolinks` there are no references. No issue tracker is ever contacted.

## Done and labels

A person records two things about a task on the dashboard: that it is **done**, and its **labels**.
Nothing about a process says whether a task's work is finished, so done is only ever what a person
set.

- **Only a task with a session ID can carry a record.** The record belongs to the host, the agent
  and the session ID together, so the same session ID on two hosts is two tasks. A codex process
  that has not started its session yet and a claude process no state file names are known only by a
  pid, which the host reuses once the process exits; they cannot be marked done or labeled. A codex
  session, running or stopped, can.
- **The records live in `~/.config/panemux/tasks.json` on the panemux host**, for every host's
  tasks, written through a temp file and a rename with mode `0600`. A symlink at that path is
  written through — its target gets the new contents and the link stays a link — as `config.yaml`
  is, including one whose target does not exist yet. The file holds only tasks that carry
  something: a task marked not done with no labels is removed from it.
- **Records persist until cleared**, even when tasks leave the list or their host is removed.
  They reappear with the session; off-list records can be cleared in the file while panemux runs.
  `PUT /api/tasks/records` can clear removed-host records but cannot add them.
- Only registry agents declaring `collect` (Claude or Codex) receive records. Records for unknown,
  non-collecting or alias IDs are validated (ID shape, session, labels and uniqueness), preserved
  unchanged including unknown fields, and written back; aliases are never canonicalized. Such
  records and their labels are not served until the agent is supported.
- **A task marked done is in the Done column only while it is stopped.** One that runs again (a
  `/resume`, `claude --resume`, or the dashboard's `Resume`) shows the state it is in, still marked done, and returns to Done
  when it stops. The record is not cleared automatically; `Mark not done` clears it.
- **Labels** are trimmed, repeats dropped, and kept in the order they were added. A label is at most
  32 characters, and has no control characters, no invisible format characters (zero-width spaces,
  direction overrides — Unicode category Cf) and no line or paragraph separators; a task carries at
  most 20. Emoji joined with a zero-width joiner (a family emoji, say) are refused with them, since
  the joiner is itself a format character. Case matters: `Docs` and `docs` are two labels.
- **Labels used before are every label in the file**, the records of tasks off the list included,
  each once, in case-insensitive alphabetical order by code point (`Docs` before `docs` when only
  case differs). The server and the dashboard order them the same way, characters beyond the Basic
  Multilingual Plane included.
  They are what `GET /api/tasks` offers as `known_labels` and the dashboard offers as suggestions
  when a task is labeled; there is no separate list of them. A label goes from them when the last
  record holding it does — cleared on the dashboard or edited out of the file. They are not offered
  while the file cannot be read, and the label filter does not use them: it lists the labels on the
  board.
- The file is served from memory while its modification time and size are what panemux last read
  or wrote, and read again when they change, so an edit made by hand while panemux runs is seen by
  the next request and is not undone by the next save. An edit that keeps both the same (within the
  filesystem's timestamp resolution, at the same size) is not noticed. A file that cannot be read —
  not JSON, another format version, an invalid entry (an agent that is not an ID shape among them) — is reported by `GET /api/tasks` as
  `records_error`, the tasks are listed without records, and every write fails until the file is
  fixed, so a file panemux does not understand is never replaced. A deleted file is no records.
