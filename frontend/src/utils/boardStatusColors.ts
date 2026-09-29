// boardStatusColors maps a pane's Agent Board self-reported state to the
// existing status-dot color vocabulary (see WorkspaceTabs.tsx's own
// statusDotStyle/pillStyle) and formats the status snapshot's updated_at
// timestamp for BoardDashboardPanel. Kept as pure functions in utils/ (not
// coverage-gated, unlike hooks/ and schemas/, but still worth testing
// directly per the existing agentAttention.ts precedent).

const STATE_COLORS: Record<string, string> = {
  working: '#7bd88f',
  idle: '#7aa2f7',
  // Deliberately reuses the existing attention-gold pill color
  // (WorkspaceTabs.tsx's pillStyle('#5a4311', '#f4bf4f', true)) rather than
  // introducing a new color: "waiting" is the same kind of "needs a look"
  // signal as the terminal-attention pill.
  waiting: '#f4bf4f',
}

const UNKNOWN_STATE_COLOR = '#4b5565'

// colorForBoardState maps a pane's self-reported state string to a status
// dot color. The agent-reported vocabulary is free text, not an enum (see
// bootstrap.go's own PTY instruction, which only *suggests*
// working/idle/waiting) — any other string, or an empty/missing one, must
// render as a neutral color rather than throw or look broken.
export function colorForBoardState(state: string | undefined): string {
  if (!state) return UNKNOWN_STATE_COLOR
  return STATE_COLORS[state] ?? UNKNOWN_STATE_COLOR
}

const STALE_THRESHOLD_MS = 5 * 60 * 1000

// isStaleUpdatedAt reports whether a status entry's updated_at timestamp is
// older than the 5 minute staleness threshold. An unparsable timestamp is
// treated as not stale, rather than as always-stale or throwing — the
// dashboard should not visually penalize a row just because its timestamp
// couldn't be parsed.
export function isStaleUpdatedAt(updatedAt: string, now: Date = new Date()): boolean {
  const parsed = new Date(updatedAt).getTime()
  if (Number.isNaN(parsed)) return false
  return now.getTime() - parsed > STALE_THRESHOLD_MS
}

// formatRelativeTime renders an RFC3339 timestamp as a short "Ns/Nm/Nh/Nd
// ago" string. Falls back to the raw input string when it can't be parsed,
// matching CommandHistoryPanel's own formatTimestamp fallback convention.
export function formatRelativeTime(value: string, now: Date = new Date()): string {
  const parsed = new Date(value).getTime()
  if (Number.isNaN(parsed)) return value

  const diffMs = Math.max(0, now.getTime() - parsed)
  const diffSec = Math.floor(diffMs / 1000)
  if (diffSec < 60) return `${diffSec}s ago`

  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`

  const diffHour = Math.floor(diffMin / 60)
  if (diffHour < 24) return `${diffHour}h ago`

  const diffDay = Math.floor(diffHour / 24)
  return `${diffDay}d ago`
}
