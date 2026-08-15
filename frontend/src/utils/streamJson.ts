// Readable rendering of `claude -p --output-format=stream-json --verbose`
// output, for the command center palette and history panel.
//
// The raw stream is mostly bookkeeping: per-token `stream_event` deltas,
// `system` init/status frames, and `user` frames carrying tool results. An
// earlier revision labelled every line by its `type`, which turned one query
// into two dozen "[stream_event]" markers with the actual answer nowhere in
// sight. Only two frame kinds carry something a person wants to read — the
// assistant's own message content, and the terminal `result` — so those are
// what this extracts and everything else is dropped.

export interface StreamSummaryLine {
  kind: 'text' | 'tool'
  text: string
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : null
}

function nonEmptyString(value: unknown): string | null {
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  return trimmed === '' ? null : trimmed
}

// summarizeStreamLines reduces raw stream-json lines to the readable parts, in
// arrival order. The `result` frame repeats the final assistant message
// verbatim, so it is used only when the turn produced no assistant text of its
// own — otherwise the answer would render twice.
export function summarizeStreamLines(lines: readonly unknown[]): StreamSummaryLine[] {
  const summary: StreamSummaryLine[] = []
  let sawText = false
  let resultFallback: string | null = null

  for (const line of lines) {
    const frame = asRecord(line)
    if (!frame) continue

    if (frame.type === 'result') {
      resultFallback = nonEmptyString(frame.result) ?? resultFallback
      continue
    }

    if (frame.type !== 'assistant') continue

    const message = asRecord(frame.message)
    const content = message?.content
    if (!Array.isArray(content)) continue

    for (const rawBlock of content) {
      const block = asRecord(rawBlock)
      if (!block) continue

      if (block.type === 'text') {
        const text = nonEmptyString(block.text)
        if (text === null) continue
        summary.push({ kind: 'text', text })
        sawText = true
        continue
      }

      if (block.type === 'tool_use') {
        const name = nonEmptyString(block.name)
        if (name === null) continue
        summary.push({ kind: 'tool', text: name })
      }
    }
  }

  if (!sawText && resultFallback !== null) {
    summary.push({ kind: 'text', text: resultFallback })
  }

  return summary
}
