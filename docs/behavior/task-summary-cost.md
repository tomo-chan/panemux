# Task summaries: what they cost

> Measurements for [task summaries](tasks.md#summaries). These numbers are evidence, dated
> 2026-10-10, not a specification: they come from one synthetic conversation and the CLI and models
> of that day. Why the summarizer runs as it does is in
> [DECISIONLOG.md](../DECISIONLOG.md#summaries-cost-less-a-fixed-system-prompt-no-tools-and-haiku-2026-10-10-issue-353).

## How it was measured

- **The conversation is synthetic.** `internal/tasks/summary_cost_fixture_test.go` holds it: a
  request, written in Japanese, to add an export to a sample application, with a correction (CSV
  becomes TSV, tax-included becomes tax-excluded), work finished along the way and large tool results
  between the messages. No real conversation, path or host is in it.
- **The update sequence** (`TestSummaryCost_TheMeasuredSequence`): the task is idle and summarized;
  a poll with the log unchanged; twice, a tool result appended while the task is busy and the task
  idle again with nothing else new; conversation text appended; panemux restarted; the summary asked
  for. The test counts the agent calls each step makes and, with `PANEMUX_SUMMARY_COST_DIR` set,
  writes the excerpts it sent there.
- **The cost of one call** was measured by sending those excerpts to `claude` with the summarizer's
  argv, by hand, and reading `usage` and `total_cost_usd` from its JSON answer (Claude Code 2.1.294;
  its default model that day was Opus 5.5, its `haiku` alias Haiku 5.5). `total_cost_usd` is the
  CLI's figure at list price. The large excerpt is the first one's recent messages repeated up to the
  24 KiB budget, to see how a long conversation scales.

## Agent calls in the sequence

The excerpts sent are the same before and after: 3,077 bytes for the first summary, 3,623 after the
conversation text is appended.

| Step | Before #352 (`a1c4bf23`) | Saved and reused by input hash (#352, `5838a8c0`) |
|---|---|---|
| First summary | 1 | 1 |
| Poll, log unchanged | 0 | 0 |
| Tool result appended while busy (×2) | 0 | 0 |
| Idle again after tool results only (×2) | 2 | 0 |
| Conversation text appended | 1 | 1 |
| Restart | 1 | 0 |
| Summary asked for | 0 | 0 |
| **Calls** | **5** | **2** |

Issue #353 changes what one call costs, not how many are made: the sequence still makes two.

## One call

`in` is all input tokens: uncached, cache-written and cache-read. A call made while the previous
call's prompt cache was warm reads part of it back.

| Summarizer | Excerpt | In (cache write / read) | Out | US$ |
|---|---|---|---|---|
| CLI system prompt, default model (before #353) | first | 6,381 (6,379 / 0) | 261 | 0.0563 |
| — | after the append, cache warm | 6,532 (3,821 / 2,709) | 190 | 0.0349 |
| — | 24 KiB | 13,534 (10,823 / 2,709) | 264 | 0.0924 |
| Fixed system prompt, `--tools ""`, default model | first | 2,489 (2,487 / 0) | 255 | 0.0250 |
| — | after the append, cache warm | 2,649 (2,118 / 529) | 197 | 0.0210 |
| — the same, given only the previous summary and the new messages | after the append | 1,880 (1,349 / 529) | 166 | 0.0142 |
| Fixed system prompt, `--tools ""`, `haiku` (#353, `9b87852b`) | first | 2,555 (2,553 / 0) | 631 | 0.0008 |
| — | after the append, cache warm | 2,716 (2,122 / 592) | 456 | 0.0007 |
| — | 24 KiB | 9,703 (9,109 / 592) | 622 | 0.0021 |

The CLI's own system prompt and tool definitions were about 3,900 of the 6,400 input tokens of a
short excerpt. The rows for the default model with the fixed system prompt, and for an update made
from the previous summary, are the candidates measured on the way and not adopted as they stand.

## The sequence

| | Calls | US$ |
|---|---|---|
| Before #352 | 5 | about 0.196 (one cold call and four warm ones) |
| #352 | 2 | 0.091 |
| #353 | 2 | 0.0015 |

## Summary quality

Read by hand against what the conversation says. All the summaries were in Japanese, and all
reported TSV, BOM and tax-excluded amounts — the correction — rather than the first request.

| Summarizer | First summary: remaining | After the append: remaining |
|---|---|---|
| Before #353 | The screen test; README | Commit |
| #353, with the first fixed system prompt | The screen test; tests after TSV; README | Check whether the directory is a git repository; commit; report the commit — the first item is about the empty directory claude runs in, not the conversation |
| #353, as shipped (the system prompt says to describe only the excerpt) | The screen test; README | Commit |

On the 24 KiB excerpt the shipped summarizer added "run the tests after the TSV change" to the
screen test and README, which the conversation does not say were run after it. Haiku's summaries are
somewhat longer than the default model's, within the same bounds.
