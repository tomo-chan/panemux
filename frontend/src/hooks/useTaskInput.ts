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
  // The task the open popup is for. The server answers every attach to one
  // task with the same board attach, so a late answer for this task is the
  // attach the open popup's own request receives, and must not be ended.
  const openTaskIdRef = useRef<string | null>(null)
  const detachRef = useRef(detach)
  detachRef.current = detach

  const request = useCallback(
    async (task: Task, token: number) => {
      const result = await attach(task)
      if (tokenRef.current !== token) {
        if (
          result.ok &&
          result.launched.session_id !== sessionIdRef.current &&
          openTaskIdRef.current !== task.id
        ) {
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
      openTaskIdRef.current = task.id
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

  const end = useCallback(() => {
    tokenRef.current++
    openTaskIdRef.current = null
    const sessionId = sessionIdRef.current
    sessionIdRef.current = null
    if (sessionId) void detachRef.current(sessionId)
  }, [])

  const close = useCallback(() => {
    end()
    const closed = session
    setSession(null)
    return closed
  }, [end, session])

  const setTerminal = useCallback((terminal: TaskTerminalStatus) => {
    setSession((current) => (current && current.terminal !== terminal ? { ...current, terminal } : current))
  }, [])

  // The dashboard going away ends the attach too: unmounting (the layer
  // shortcut), and the page itself going (a reload, a closed tab), which runs
  // no cleanup — pagehide does, and detach's keepalive outlives the page.
  useEffect(() => {
    window.addEventListener('pagehide', end)
    return () => {
      window.removeEventListener('pagehide', end)
      end()
    }
  }, [end])

  return { session, open, retry, close, setTerminal }
}
