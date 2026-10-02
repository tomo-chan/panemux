import { useCallback, useEffect, useRef, useState } from 'react'
import type { Task } from '../schemas'
import type { TasksState } from './useTasks'
import type { TaskAttachPhase } from '../components/TaskInputPopup'
import type { TaskTerminalStatus } from '../components/TaskTerminal'
import { columnForTask } from '../utils/taskBoard'
import type { TaskColumnId } from '../utils/taskBoard'

/** Which Type in pane button opened the popup, for returning focus to it. */
export type TaskInputOrigin = 'card' | 'detail'

export interface TaskInputSession {
  /** The task as it was when the popup opened. The popup stays on its ID whatever a poll does. */
  task: Task
  column: TaskColumnId
  origin: TaskInputOrigin
  originElement: HTMLElement | null
  attach: TaskAttachPhase
  attempt: number
  terminal: TaskTerminalStatus
}

/**
 * The Type in pane popup's attach (issue #284). One popup at a time, bound to
 * the task it was opened for. Each request carries a token: an answer that
 * arrives after the popup closed, or after a newer request, is ended at once
 * rather than shown, so a late answer never puts another task's terminal in
 * front of the person typing. Closing, or the dashboard going away, ends the
 * attach — only that tmux client; the agent and its session keep running.
 */
export function useTaskInput(attach: TasksState['attach'], detach: TasksState['detach']) {
  const [session, setSession] = useState<TaskInputSession | null>(null)
  const tokenRef = useRef(0)
  const sessionIdRef = useRef<string | null>(null)
  const detachRef = useRef(detach)
  detachRef.current = detach

  const request = useCallback(
    async (task: Task, token: number) => {
      const result = await attach(task)
      if (tokenRef.current !== token) {
        if (result.ok && result.launched.session_id !== sessionIdRef.current) {
          void detachRef.current(result.launched.session_id)
        }
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
    [attach],
  )

  const open = useCallback(
    (task: Task, origin: TaskInputOrigin, originElement: HTMLElement | null) => {
      if (sessionIdRef.current || session) return
      const token = ++tokenRef.current
      setSession({
        task,
        column: columnForTask(task),
        origin,
        originElement,
        attach: { phase: 'connecting' },
        attempt: 0,
        terminal: 'connecting',
      })
      void request(task, token)
    },
    [request, session],
  )

  const retry = useCallback(() => {
    if (!session) return
    const token = ++tokenRef.current
    setSession({ ...session, attach: { phase: 'connecting' }, attempt: session.attempt + 1, terminal: 'connecting' })
    void request(session.task, token)
  }, [request, session])

  const close = useCallback(() => {
    tokenRef.current++
    const sessionId = sessionIdRef.current
    sessionIdRef.current = null
    if (sessionId) void detachRef.current(sessionId)
    const closed = session
    setSession(null)
    return closed
  }, [session])

  const setTerminal = useCallback((terminal: TaskTerminalStatus) => {
    setSession((current) => (current && current.terminal !== terminal ? { ...current, terminal } : current))
  }, [])

  // The dashboard going away (the layer shortcut, a reload) ends the attach too.
  useEffect(
    () => () => {
      tokenRef.current++
      const sessionId = sessionIdRef.current
      sessionIdRef.current = null
      if (sessionId) void detachRef.current(sessionId)
    },
    [],
  )

  return { session, open, retry, close, setTerminal }
}
