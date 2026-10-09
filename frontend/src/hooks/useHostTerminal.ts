import { useCallback, useEffect, useRef, useState } from 'react'
import type { HostTerminalType, TasksState } from './useTasks'
import type { TaskAttachPhase } from '../components/TaskInputPopup'
import type { TaskTerminalStatus } from '../components/TaskTerminal'

export interface HostTerminalSession {
  host: string
  type: HostTerminalType
  /** The ssh_tmux session asked for, from the connection menu. */
  tmuxSession: string | undefined
  /** The chip the menu was opened from, for returning focus to it. */
  originElement: HTMLElement | null
  attach: TaskAttachPhase
  attempt: number
  terminal: TaskTerminalStatus
}

/**
 * The task dashboard's host Type in pane popup (issue #314). One popup at a
 * time. Every request opens a new host terminal on the server, so an answer
 * that arrives after the popup closed, or after a newer request, is ended at
 * once rather than shown. Closing, or the dashboard going away, ends the
 * terminal: an ssh one logs out, an ssh_tmux one ends only its tmux client and
 * leaves the remote session running. A retry opens a new terminal on the same
 * tmux session, so an ssh_tmux popup comes back to the session it had.
 */
export function useHostTerminal(openTerminal: TasksState['hostTerminal'], closeTerminal: TasksState['closeHostTerminal']) {
  const [session, setSession] = useState<HostTerminalSession | null>(null)
  const tokenRef = useRef(0)
  const sessionIdRef = useRef<string | null>(null)
  const closeRef = useRef(closeTerminal)
  closeRef.current = closeTerminal

  const end = useCallback(() => {
    tokenRef.current++
    const sessionId = sessionIdRef.current
    sessionIdRef.current = null
    if (sessionId) void closeRef.current(sessionId)
  }, [])

  const request = useCallback(
    async (host: string, type: HostTerminalType, tmuxSession: string | undefined) => {
      const token = ++tokenRef.current
      const result = await openTerminal(host, type, tmuxSession)
      if (tokenRef.current !== token) {
        if (result.ok) void closeRef.current(result.launched.session_id)
        return
      }
      if (result.ok) sessionIdRef.current = result.launched.session_id
      setSession((current) =>
        current
          ? {
              ...current,
              terminal: 'connecting',
              attach: result.ok
                ? { phase: 'ready', sessionId: result.launched.session_id, tmuxSession: result.launched.tmux_session }
                : { phase: 'failed', error: result.error },
            }
          : current,
      )
    },
    [openTerminal],
  )

  const open = useCallback(
    (host: string, type: HostTerminalType, tmuxSession: string | undefined, originElement: HTMLElement | null) => {
      if (sessionIdRef.current || session) return
      setSession({
        host,
        type,
        tmuxSession,
        originElement,
        attach: { phase: 'connecting' },
        attempt: 0,
        terminal: 'connecting',
      })
      return request(host, type, tmuxSession)
    },
    [request, session],
  )

  const reopen = useCallback(
    (current: HostTerminalSession) => {
      end()
      setSession({ ...current, attach: { phase: 'connecting' }, attempt: current.attempt + 1, terminal: 'connecting' })
      return request(current.host, current.type, current.tmuxSession)
    },
    [end, request],
  )

  const retry = useCallback(() => {
    if (session) return reopen(session)
  }, [reopen, session])

  const close = useCallback(() => {
    end()
    const closed = session
    setSession(null)
    return closed
  }, [end, session])

  const setTerminal = useCallback((terminal: TaskTerminalStatus) => {
    setSession((current) => (current && current.terminal !== terminal ? { ...current, terminal } : current))
  }, [])

  // As useTaskInput: unmounting and pagehide end the terminal, and a page
  // restored from the back/forward cache asks for a new one.
  useEffect(() => {
    window.addEventListener('pagehide', end)
    return () => {
      window.removeEventListener('pagehide', end)
      end()
    }
  }, [end])

  const sessionRef = useRef(session)
  sessionRef.current = session
  useEffect(() => {
    const handlePageShow = (event: PageTransitionEvent) => {
      if (event.persisted && sessionRef.current) void reopen(sessionRef.current)
    }
    window.addEventListener('pageshow', handlePageShow)
    return () => window.removeEventListener('pageshow', handlePageShow)
  }, [reopen])

  return { session, open, retry, close, setTerminal }
}
