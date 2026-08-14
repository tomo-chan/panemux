import { useCallback, useEffect, useRef, useState } from 'react'
import { BoardMessage, BoardMessagesResponseSchema, BoardStatusEntry, BoardStatusResponseSchema } from '../schemas'

const BOARD_STATUS_POLL_INTERVAL_MS = 5000
const MAX_MESSAGES = 500

// SYSTEM_ID mirrors internal/board.SystemID (Go) / config's reservedSystemID
// — the reserved agmsg identity panemux's own relay and command center use
// as their status-report to. Duplicated here rather than imported since the
// frontend has no dependency on the Go package; see internal/board/board.go's
// own comment for why the two copies are kept in sync by convention.
const SYSTEM_ID = '_system'

// STATUS_KIND mirrors internal/board's own statusKind literal.
const STATUS_KIND = 'board_status'

interface UseBoardStatusOptions {
  enabled: boolean
  token: string
}

export interface UseBoardStatusResult {
  statuses: Record<string, BoardStatusEntry>
  messages: BoardMessage[]
  error: string | null
}

function errorMessageForStatus(status: number): string {
  if (status === 401 || status === 403) return 'Not authorized to view the agent board.'
  return `Failed to load board status (${status}).`
}

// isBoardStatusRow reports whether message is a pane's own status
// self-report rather than an ordinary cross-pane message. These are
// appended to agmsg history alongside real messages (see
// internal/board/relay.go), so the dashboard's message feed must filter
// them out itself — otherwise raw board_status JSON would show up as if it
// were a message a user or agent actually sent.
function isBoardStatusRow(message: BoardMessage): boolean {
  if (message.to !== SYSTEM_ID) return false
  try {
    const parsed: unknown = JSON.parse(message.body)
    return (
      typeof parsed === 'object' &&
      parsed !== null &&
      (parsed as Record<string, unknown>).kind === STATUS_KIND
    )
  } catch {
    return false
  }
}

// useBoardStatus polls the Agent Board's status snapshot and incremental
// message history for the dashboard panel. See docs/agent-board.md's
// Architecture section: GET /api/board/status always returns a full
// snapshot, while GET /api/board/messages?since=<seq> is incremental against
// panemux's own monotonic cursor (not an agmsg-native id).
export function useBoardStatus({ enabled, token }: UseBoardStatusOptions): UseBoardStatusResult {
  const [statuses, setStatuses] = useState<Record<string, BoardStatusEntry>>({})
  const [messages, setMessages] = useState<BoardMessage[]>([])
  const [error, setError] = useState<string | null>(null)
  const [isVisible, setIsVisible] = useState(() => document.visibilityState === 'visible')
  const lastSeqRef = useRef(0)

  useEffect(() => {
    const handleVisibilityChange = () => setIsVisible(document.visibilityState === 'visible')
    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
  }, [])

  const poll = useCallback(async (cancelledRef: { current: boolean }) => {
    const headers = { Authorization: `Bearer ${token}` }
    // The status and messages requests are independent, but both report
    // into one error slot — statusError takes priority over messagesError
    // so, e.g., an expired token surfaces even if the messages request
    // happened to still return something parseable. Neither branch calls
    // setError directly: doing so would let a later success on the other
    // request silently clobber an earlier failure's error (or vice versa).
    let statusError: string | null = null
    let messagesError: string | null = null

    try {
      const res = await fetch('/api/board/status', { headers })
      if (cancelledRef.current) return
      if (!res.ok) {
        statusError = errorMessageForStatus(res.status)
      } else {
        const data = BoardStatusResponseSchema.parse(await res.json())
        if (!cancelledRef.current) setStatuses(data.statuses)
      }
    } catch {
      statusError = 'Failed to load board status.'
    }
    if (cancelledRef.current) return

    try {
      const res = await fetch(`/api/board/messages?since=${lastSeqRef.current}`, { headers })
      if (cancelledRef.current) return
      if (!res.ok) {
        messagesError = errorMessageForStatus(res.status)
      } else {
        const data = BoardMessagesResponseSchema.parse(await res.json())
        if (!cancelledRef.current && data.messages.length > 0) {
          for (const message of data.messages) {
            if (message.seq > lastSeqRef.current) lastSeqRef.current = message.seq
          }
          const nonStatusRows = data.messages.filter((message) => !isBoardStatusRow(message))
          if (nonStatusRows.length > 0) {
            setMessages((prev) => [...prev, ...nonStatusRows].slice(-MAX_MESSAGES))
          }
        }
      }
    } catch {
      messagesError = 'Failed to load board status.'
    }
    if (cancelledRef.current) return

    setError(statusError ?? messagesError)
  }, [token])

  useEffect(() => {
    if (!enabled || !token || !isVisible) return

    const cancelledRef = { current: false }
    void poll(cancelledRef)
    const interval = setInterval(() => {
      void poll(cancelledRef)
    }, BOARD_STATUS_POLL_INTERVAL_MS)

    return () => {
      cancelledRef.current = true
      clearInterval(interval)
    }
  }, [enabled, token, isVisible, poll])

  return { statuses, messages, error }
}
