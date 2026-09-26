import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Task,
  TaskLaunched,
  TaskLaunchedSchema,
  TaskLaunchResponse,
  TaskLaunchResponseSchema,
  TaskRecordSchema,
  TasksResponse,
  TasksResponseSchema,
  TaskSummarySchema,
} from '../schemas'
import { applyTaskRecord } from '../utils/taskBoard'

// How often the dashboard re-collects while it is on screen. Collection runs
// a script on every host, so it happens only while the dashboard is shown and
// the page is visible — never in the background (issue #252).
export const TASKS_POLL_INTERVAL_MS = 10000

/** A new task: where it runs, and its first instruction and labels. */
export interface TaskLaunchInput {
  /** The ssh_connections key, or '' for the panemux host. */
  host: string
  cwd: string
  prompt: string
  labels: string[]
}

/** What starting or resuming a task came to: the task, or why not. */
export type TaskActionResult<T> = { ok: true; launched: T } | { ok: false; error: string }

export interface TasksState {
  data: TasksResponse | null
  error: string | null
  loading: boolean
  /** When the last successful response arrived (ms since epoch). */
  updatedAt: number | null
  refresh: () => Promise<void>
  reconnect: (host: string) => Promise<void>
  /**
   * Replaces what is recorded about a task — done and its labels — and
   * applies the server's answer at once. Resolves to why it failed, or null.
   */
  saveRecord: (task: Task, record: { done: boolean; labels: string[] }) => Promise<string | null>
  /**
   * Starts a claude task in a tmux session of its own on its host, without a
   * pane (issue #257), then collects again.
   */
  launch: (input: TaskLaunchInput) => Promise<TaskActionResult<TaskLaunchResponse>>
  /** Runs `claude --resume` for a stopped task, then collects again. */
  resume: (task: Task) => Promise<TaskActionResult<TaskLaunched>>
  /**
   * Asks the server to summarize a task (issue #258) and shows where its
   * summary stands at once; the summary itself arrives with a later poll.
   * Resolves to why it failed, or null.
   */
  requestSummary: (task: Task) => Promise<string | null>
}

// postTaskAction POSTs body to path and parses the answer with schema. A
// refusal comes back as the server's own reason.
async function postTaskAction<T>(
  path: string,
  body: unknown,
  schema: { safeParse: (value: unknown) => { success: true; data: T } | { success: false } },
): Promise<TaskActionResult<T>> {
  try {
    const res = await fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const reason = (await res.text()).trim()
      return { ok: false, error: reason || `HTTP ${res.status}` }
    }
    const parsed = schema.safeParse(await res.json())
    if (!parsed.success) return { ok: false, error: `Unexpected response from ${path}` }
    return { ok: true, launched: parsed.data }
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : `Request to ${path} failed` }
  }
}

export function useTasks(enabled: boolean): TasksState {
  const [data, setData] = useState<TasksResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [updatedAt, setUpdatedAt] = useState<number | null>(null)
  const [isVisible, setIsVisible] = useState(() => document.visibilityState === 'visible')
  const inFlight = useRef(false)
  // Counts record saves. A collection that was running when a save landed
  // read the records before it, so its answer is dropped rather than allowed
  // to put the old record back; the next poll brings the new one.
  const recordSaves = useRef(0)
  // Set when a task was started or resumed while a collection was running.
  // That collection began before the task existed, so another one runs as
  // soon as it finishes instead of the task waiting for the next poll.
  const collectAgain = useRef(false)

  const refresh = useCallback(async () => {
    if (inFlight.current) return
    inFlight.current = true
    setLoading(true)
    const savesAtStart = recordSaves.current
    try {
      const res = await fetch('/api/tasks')
      if (!res.ok) {
        setError(`HTTP ${res.status}`)
        return
      }
      const parsed = TasksResponseSchema.safeParse(await res.json())
      if (!parsed.success) {
        setError('Unexpected response from /api/tasks')
        return
      }
      if (savesAtStart !== recordSaves.current) return
      setData(parsed.data)
      setError(null)
      setUpdatedAt(Date.now())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load tasks')
    } finally {
      inFlight.current = false
      setLoading(false)
      if (collectAgain.current) {
        collectAgain.current = false
        void refresh()
      }
    }
  }, [])

  const refreshAfterAction = useCallback(async () => {
    if (inFlight.current) {
      collectAgain.current = true
      return
    }
    await refresh()
  }, [refresh])

  const reconnect = useCallback(async (host: string) => {
    try {
      const res = await fetch(`/api/tasks/hosts/${encodeURIComponent(host)}/reconnect`, { method: 'POST' })
      if (!res.ok) {
        setError(`Reconnect ${host} failed: HTTP ${res.status}`)
        return
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : `Reconnect ${host} failed`)
      return
    }
    await refresh()
  }, [refresh])

  const saveRecord = useCallback(async (task: Task, record: { done: boolean; labels: string[] }) => {
    if (!task.session_id) return 'This task has no session ID to record against'
    try {
      const res = await fetch('/api/tasks/records', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          host: task.host,
          agent: task.agent,
          session_id: task.session_id,
          done: record.done,
          labels: record.labels,
        }),
      })
      if (!res.ok) {
        const reason = (await res.text()).trim()
        return reason || `HTTP ${res.status}`
      }
      const parsed = TaskRecordSchema.safeParse(await res.json())
      if (!parsed.success) return 'Unexpected response from /api/tasks/records'
      recordSaves.current += 1
      setData((current) => (current ? { ...current, tasks: applyTaskRecord(current.tasks, parsed.data) } : current))
      return null
    } catch (err) {
      return err instanceof Error ? err.message : 'Failed to save the task record'
    }
  }, [])

  const launch = useCallback(async (input: TaskLaunchInput) => {
    const result = await postTaskAction(
      '/api/tasks',
      { host: input.host, agent: 'claude', cwd: input.cwd, prompt: input.prompt, labels: input.labels },
      TaskLaunchResponseSchema,
    )
    if (result.ok) await refreshAfterAction()
    return result
  }, [refreshAfterAction])

  const resume = useCallback(async (task: Task): Promise<TaskActionResult<TaskLaunched>> => {
    if (!task.session_id) return { ok: false, error: 'This task has no session ID to resume' }
    const result = await postTaskAction(
      '/api/tasks/resume',
      { host: task.host, session_id: task.session_id },
      TaskLaunchedSchema,
    )
    if (result.ok) await refreshAfterAction()
    return result
  }, [refreshAfterAction])

  const requestSummary = useCallback(async (task: Task) => {
    if (!task.session_id) return 'This task has no session ID to summarize'
    const result = await postTaskAction(
      '/api/tasks/summary',
      { host: task.host, session_id: task.session_id },
      TaskSummarySchema,
    )
    if (!result.ok) return result.error
    const summary = result.launched
    setData((current) =>
      current
        ? { ...current, tasks: current.tasks.map((t) => (t.id === task.id ? { ...t, summary } : t)) }
        : current,
    )
    return null
  }, [])

  useEffect(() => {
    const handleVisibilityChange = () => setIsVisible(document.visibilityState === 'visible')
    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
  }, [])

  useEffect(() => {
    if (!enabled || !isVisible) return
    void refresh()
    const interval = setInterval(() => {
      void refresh()
    }, TASKS_POLL_INTERVAL_MS)
    return () => clearInterval(interval)
  }, [enabled, isVisible, refresh])

  return { data, error, loading, updatedAt, refresh, reconnect, saveRecord, launch, resume, requestSummary }
}
