import { useCallback, useEffect, useRef, useState } from 'react'
import { BoardMessage, BoardMessagesResponseSchema, BoardStatusEntry, BoardStatusResponseSchema } from '../schemas'

const BOARD_STATUS_POLL_INTERVAL_MS = 5000
const MAX_MESSAGES = 500

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
  const epochRef = useRef('')
  const inFlightRef = useRef(false)

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

    const fetchMessagesPage = async (since: number) => {
      const res = await fetch(`/api/board/messages?since=${since}`, { headers })
      if (!res.ok) {
        messagesError = errorMessageForStatus(res.status)
        return null
      }
      return BoardMessagesResponseSchema.parse(await res.json())
    }

    // appendPage advances the cursor and appends only rows the feed does not
    // already hold. Filtering on the cursor's own previous value (rather than
    // tracking every seq ever seen) bounds this to O(1) state and is exact:
    // seq is server-assigned and monotonic, so anything at or below the
    // cursor is by definition a row already delivered.
    const appendPage = (page: { messages: BoardMessage[] }) => {
      const previousSeq = lastSeqRef.current
      for (const message of page.messages) {
        if (message.seq > lastSeqRef.current) lastSeqRef.current = message.seq
      }
      const fresh = page.messages.filter((message) => message.seq > previousSeq && !message.is_status)
      if (fresh.length > 0) {
        setMessages((prev) => [...prev, ...fresh].slice(-MAX_MESSAGES))
      }
    }

    try {
      let page = await fetchMessagesPage(lastSeqRef.current)
      if (cancelledRef.current) return
      if (page) {
        // A changed epoch means the server-side cache was rebuilt (panemux
        // restarted): the cursor we just sent counts against a numbering that
        // no longer exists, so every future poll would come back empty and
        // the feed would freeze without ever reporting an error. Drop what we
        // hold and re-read from the start of the new cache.
        if (epochRef.current !== page.epoch) {
          const staleCursor = lastSeqRef.current
          epochRef.current = page.epoch
          lastSeqRef.current = 0
          if (staleCursor > 0) {
            setMessages([])
            page = await fetchMessagesPage(0)
            if (cancelledRef.current) return
          }
        }
        if (page) appendPage(page)
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
    // A poll that outlives the interval must not be joined by a second one:
    // both would send the same since cursor, both would get the same rows
    // back, and the feed would show every one of them twice. Skipping the
    // tick is the right response rather than queueing it — the next tick
    // re-reads the same cursor a few seconds later anyway.
    const pollUnlessBusy = async () => {
      if (inFlightRef.current) return
      inFlightRef.current = true
      try {
        await poll(cancelledRef)
      } finally {
        inFlightRef.current = false
      }
    }

    void pollUnlessBusy()
    const interval = setInterval(() => {
      void pollUnlessBusy()
    }, BOARD_STATUS_POLL_INTERVAL_MS)

    return () => {
      cancelledRef.current = true
      inFlightRef.current = false
      clearInterval(interval)
    }
  }, [enabled, token, isVisible, poll])

  return { statuses, messages, error }
}
