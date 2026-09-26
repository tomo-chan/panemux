import { useCallback, useEffect, useRef, useState } from 'react'
import { Task, TaskRecordSchema, TasksResponse, TasksResponseSchema } from '../schemas'
import { applyTaskRecord } from '../utils/taskBoard'

// How often the dashboard re-collects while it is on screen. Collection runs
// a script on every host, so it happens only while the dashboard is shown and
// the page is visible — never in the background (issue #252).
export const TASKS_POLL_INTERVAL_MS = 10000

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
    }
  }, [])

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

  return { data, error, loading, updatedAt, refresh, reconnect, saveRecord }
}
