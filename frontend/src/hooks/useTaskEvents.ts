import { useEffect, useRef, useState } from 'react'
import type { TaskEventFrame } from '../schemas'
import { applyTaskEventFrame, type TaskEventStore } from '../utils/taskEvents'

export type TaskEventConnectionStatus = 'connecting' | 'live' | 'offline'

/** One applied frame, with what the tab held before it and after it. */
export interface TaskEventChange {
  before: TaskEventStore | null
  frame: TaskEventFrame
  after: TaskEventStore
}

export interface TaskEventsState {
  /** Null until the connection's snapshot, and after the stream is lost. */
  store: TaskEventStore | null
  status: TaskEventConnectionStatus
}

const FIRST_RETRY_MS = 2_000
const LAST_RETRY_MS = 30_000

/**
 * The tab's one connection to /ws/tasks/events (issue #279,
 * docs/behavior/task-events.md#store-and-connection). Every frame goes through
 * applyTaskEventFrame; one it refuses discards what the tab holds and the
 * stream is opened again for a fresh snapshot. A lost connection is retried
 * from 2 to 30 seconds apart with no limit, and the stream stays open while
 * the page is hidden, since that is when notifications matter.
 *
 * onChange is read when a frame arrives, so a new callback on every render
 * does not reopen the stream.
 */
export function useTaskEvents(onChange: (change: TaskEventChange) => void): TaskEventsState {
  const [state, setState] = useState<TaskEventsState>({ store: null, status: 'connecting' })
  const onChangeRef = useRef(onChange)

  useEffect(() => {
    onChangeRef.current = onChange
  }, [onChange])

  useEffect(() => {
    let socket: WebSocket | null = null
    let retryTimer: number | null = null
    let retryMs = FIRST_RETRY_MS
    let store: TaskEventStore | null = null
    let stopped = false

    const scheduleRetry = () => {
      if (stopped || retryTimer !== null) return
      retryTimer = window.setTimeout(() => {
        retryTimer = null
        connect()
      }, retryMs)
      retryMs = Math.min(retryMs * 2, LAST_RETRY_MS)
    }

    const lose = (lost: WebSocket) => {
      if (lost !== socket) return
      socket = null
      lost.onmessage = null
      lost.onclose = null
      lost.onerror = null
      lost.close()
      store = null
      setState({ store: null, status: 'offline' })
      scheduleRetry()
    }

    const connect = () => {
      const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
      const opened = new WebSocket(`${protocol}//${location.host}/ws/tasks/events`)
      socket = opened
      setState((current) => (current.status === 'connecting' ? current : { ...current, status: 'connecting' }))

      opened.onmessage = (event) => {
        let raw: unknown
        try {
          raw = typeof event.data === 'string' ? JSON.parse(event.data) : null
        } catch {
          raw = null
        }
        const before = store
        const result = applyTaskEventFrame(before, raw)
        if (!result.ok) {
          lose(opened)
          return
        }
        store = result.store
        if (result.frame.type === 'snapshot') retryMs = FIRST_RETRY_MS
        setState({ store: result.store, status: 'live' })
        onChangeRef.current({ before, frame: result.frame, after: result.store })
      }
      opened.onclose = () => lose(opened)
      opened.onerror = () => lose(opened)
    }

    connect()

    return () => {
      stopped = true
      if (retryTimer !== null) window.clearTimeout(retryTimer)
      const open = socket
      socket = null
      if (open) {
        open.onmessage = null
        open.onclose = null
        open.onerror = null
        open.close()
      }
    }
  }, [])

  return state
}
