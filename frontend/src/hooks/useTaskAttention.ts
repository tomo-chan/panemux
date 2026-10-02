import { useEffect, useRef, useState } from 'react'
import { TasksAttentionResponseSchema, type TasksResponse, type Workspace } from '../schemas'
import { findTaskPane } from '../utils/taskBoard'
import { taskAttentionTracker, type TaskAttentionSnapshot, type TaskAttentionTracker, type TaskWaitEvent } from '../utils/taskAttention'

// How often the running tasks are collected for attention while the task
// dashboard is not collecting them itself (issue #279). Longer than the
// dashboard's own 10 s: this runs for as long as the page is open.
export const TASK_ATTENTION_POLL_INTERVAL_MS = 15000

interface UseTaskAttentionOptions {
  /** Whether the task dashboard layer is shown; it collects while the page is visible. */
  dashboardShown: boolean
  dashboardData: TasksResponse | null
  /** When dashboardData arrived (ms since epoch). */
  dashboardUpdatedAt: number | null
  workspaces: Workspace[]
  /** Called with the waits new to this page, from either source. */
  onTaskWaits: (events: TaskWaitEvent[]) => void
  /** Injectable for tests. */
  tracker?: TaskAttentionTracker
}

/**
 * Feeds every collection of running tasks to the attention tracker: the
 * dashboard's own while it collects (shown on a visible page), and
 * GET /api/tasks/attention every TASK_ATTENTION_POLL_INTERVAL_MS otherwise,
 * so a wait is noticed for as long as the page is open.
 */
export function useTaskAttention({
  dashboardShown,
  dashboardData,
  dashboardUpdatedAt,
  workspaces,
  onTaskWaits,
  tracker = taskAttentionTracker,
}: UseTaskAttentionOptions) {
  const [isVisible, setIsVisible] = useState(() => document.visibilityState === 'visible')
  const workspacesRef = useRef(workspaces)
  workspacesRef.current = workspaces
  const onTaskWaitsRef = useRef(onTaskWaits)
  onTaskWaitsRef.current = onTaskWaits

  const applyRef = useRef((snapshot: TaskAttentionSnapshot, receivedAt: number) => {
    const events = tracker.applySnapshot(snapshot, receivedAt, (task) => findTaskPane(task, workspacesRef.current))
    if (events.length > 0) onTaskWaitsRef.current(events)
  })

  useEffect(() => {
    const handleVisibilityChange = () => setIsVisible(document.visibilityState === 'visible')
    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
  }, [])

  const dashboardCollects = dashboardShown && isVisible

  useEffect(() => {
    if (dashboardCollects) return
    // Aborted when the dashboard takes over or the page goes, so an answer
    // that arrives after that is never applied over a newer one.
    const controller = new AbortController()
    let inFlight = false

    const collect = async () => {
      if (inFlight) return
      inFlight = true
      try {
        const res = await fetch('/api/tasks/attention', { signal: controller.signal })
        if (!res.ok) return
        const parsed = TasksAttentionResponseSchema.safeParse(await res.json())
        if (!parsed.success || controller.signal.aborted) return
        applyRef.current(parsed.data, Date.now())
      } catch {
        // A failed collection reports nothing; the next one tries again.
      } finally {
        inFlight = false
      }
    }

    void collect()
    const interval = setInterval(() => void collect(), TASK_ATTENTION_POLL_INTERVAL_MS)
    return () => {
      clearInterval(interval)
      controller.abort()
    }
  }, [dashboardCollects])

  useEffect(() => {
    if (!dashboardShown || !dashboardData || dashboardUpdatedAt === null) return
    applyRef.current(dashboardData, dashboardUpdatedAt)
  }, [dashboardShown, dashboardData, dashboardUpdatedAt])
}
