import { useCallback, useEffect, useRef, useState } from 'react'
import { TasksResponse, TasksResponseSchema } from '../schemas'

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
}

export function useTasks(enabled: boolean): TasksState {
  const [data, setData] = useState<TasksResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [updatedAt, setUpdatedAt] = useState<number | null>(null)
  const [isVisible, setIsVisible] = useState(() => document.visibilityState === 'visible')
  const inFlight = useRef(false)

  const refresh = useCallback(async () => {
    if (inFlight.current) return
    inFlight.current = true
    setLoading(true)
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

  return { data, error, loading, updatedAt, refresh, reconnect }
}
