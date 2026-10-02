import type { Task, TaskHost } from '../schemas'
import { hostLabel, taskTitle, type TaskPaneRef } from './taskBoard'

// Where the task-side half of agent attention (issue #279) is remembered
// across reloads: which wait signatures have already been handled, and when a
// pane last showed a fresh terminal confirmation prompt.
export const TASK_ATTENTION_STORAGE_KEY = 'panemux:task-attention'

// A terminal prompt this long before a wait's recorded start still counts as
// that wait: the agent renders its prompt and records its state at nearly the
// same moment, in either order.
export const TERMINAL_WAIT_TOLERANCE_MS = 5000

const DEFAULT_MAX_ENTRIES = 200

/** How often the page collects task waits outside the dashboard. */
export const TASK_ATTENTION_COLLECTION_INTERVAL_MS = 15000

/** A wait this page had not seen yet. */
export interface TaskWaitEvent {
  task: Task
  pane: TaskPaneRef | null
  /**
   * Whether this wait may show a browser notification: it has a signature,
   * no earlier snapshot, page load or terminal prompt has already handled it.
   * Whether the wait is visible right now is for the caller to decide.
   */
  notify: boolean
}

export interface TaskAttentionSnapshot {
  hosts: TaskHost[]
  tasks: Task[]
}

export interface TaskAttentionTracker {
  /**
   * Applies one collection of running tasks, received at receivedAt (browser
   * ms), and returns the waits new to this page. A snapshot received before
   * the last one applied is ignored, and a host that failed keeps what was
   * known about it.
   */
  applySnapshot: (
    snapshot: TaskAttentionSnapshot,
    receivedAt: number,
    findPane: (task: Task) => TaskPaneRef | null,
  ) => TaskWaitEvent[]
  /**
   * Records a fresh terminal confirmation prompt on a pane at `at` (browser
   * ms). Returns true when that pane's task is in a signed wait already
   * handled, which makes the prompt that same wait.
   */
  noteTerminalPrompt: (paneId: string, at: number) => boolean
}

interface StoredState {
  signatures: Record<string, number>
  terminal: Record<string, number>
}

interface KnownWait {
  host: string
  /** The wait signature, or '' for a wait that has none. */
  signature: string
  /** The pane last reported for it, or null while none showed it. */
  paneId: string | null
}

interface PaneWait {
  signature: string
  /** When a snapshot last showed this wait from a host that answered. */
  seenAt: number
  /** Kept only because its host failed to answer since. */
  held: boolean
}

export function createTaskAttentionTracker(
  getStorage: () => Storage | null,
  {
    maxEntries = DEFAULT_MAX_ENTRIES,
    holdMs = TASK_ATTENTION_COLLECTION_INTERVAL_MS,
  }: { maxEntries?: number; holdMs?: number } = {},
): TaskAttentionTracker {
  let memoryState: StoredState = { signatures: {}, terminal: {} }
  // The waits this page has reported, by task ID.
  const knownWaits = new Map<string, KnownWait>()
  // The signed wait each pane's task is in, as of the last snapshot, and when
  // its host last answered with it. A failed host's wait is held for holdMs
  // past that answer and no longer.
  let paneWaits = new Map<string, PaneWait>()
  let lastAppliedAt = -Infinity

  const read = (): StoredState => {
    const storage = getStorage()
    if (!storage) return memoryState
    try {
      const raw = storage.getItem(TASK_ATTENTION_STORAGE_KEY)
      if (!raw) return { signatures: {}, terminal: {} }
      const parsed = JSON.parse(raw) as Partial<StoredState> | null
      return {
        signatures: numberRecord(parsed?.signatures),
        terminal: numberRecord(parsed?.terminal),
      }
    } catch {
      return memoryState
    }
  }

  const write = (state: StoredState) => {
    const bounded = {
      signatures: newest(state.signatures, maxEntries),
      terminal: newest(state.terminal, maxEntries),
    }
    memoryState = bounded
    const storage = getStorage()
    if (!storage) return
    try {
      storage.setItem(TASK_ATTENTION_STORAGE_KEY, JSON.stringify(bounded))
    } catch {
      // memoryState already holds it.
    }
  }

  return {
    applySnapshot(snapshot, receivedAt, findPane) {
      if (receivedAt < lastAppliedAt) return []
      lastAppliedAt = receivedAt

      const hosts = new Map(snapshot.hosts.map((host) => [host.name, host]))
      const okHost = (name: string) => hosts.get(name)?.status === 'ok'
      const waiting = new Map<string, Task>()
      for (const task of snapshot.tasks) {
        if (task.state === 'wait' && okHost(task.host)) waiting.set(task.id, task)
      }

      // A wait ends when its host answered without it. A host that failed
      // says nothing about its tasks.
      for (const [taskId, known] of knownWaits) {
        if (!okHost(known.host)) continue
        const task = waiting.get(taskId)
        if (!task || (task.wait_signature ?? '') !== known.signature) knownWaits.delete(taskId)
      }

      const state = read()
      const events: TaskWaitEvent[] = []
      const nextPaneWaits = new Map<string, PaneWait>()
      for (const task of waiting.values()) {
        const pane = findPane(task)
        const signature = task.wait_signature ?? ''
        if (pane && signature) nextPaneWaits.set(pane.paneId, { signature, seenAt: receivedAt, held: false })
        const known = knownWaits.get(task.id)
        if (known) {
          // A wait first seen without a pane gets one once its pane is found:
          // attention, not a second notification.
          if (pane && pane.paneId !== known.paneId) events.push({ task, pane, notify: false })
          if (pane) known.paneId = pane.paneId
          continue
        }
        knownWaits.set(task.id, { host: task.host, signature, paneId: pane?.paneId ?? null })

        let notify = false
        if (signature && state.signatures[signature] === undefined) {
          const promptAt = pane ? state.terminal[pane.paneId] : undefined
          const waitStart = waitStartInBrowserClock(task, hosts.get(task.host), receivedAt)
          notify = !(promptAt !== undefined && waitStart !== null && promptAt >= waitStart - TERMINAL_WAIT_TOLERANCE_MS)
        }
        if (signature) state.signatures[signature] = receivedAt
        events.push({ task, pane, notify })
      }
      // A pane whose task sits on a failed host keeps its wait.
      for (const [paneId, wait] of paneWaits) {
        if (nextPaneWaits.has(paneId) || receivedAt - wait.seenAt > holdMs) continue
        const stillKnown = [...knownWaits.values()].some((known) => known.signature === wait.signature && !okHost(known.host))
        if (stillKnown) nextPaneWaits.set(paneId, { ...wait, held: true })
      }
      paneWaits = nextPaneWaits

      write(state)
      return events
    },

    noteTerminalPrompt(paneId, at) {
      const state = read()
      state.terminal[paneId] = at
      write(state)
      const wait = paneWaits.get(paneId)
      if (!wait || (wait.held && at - wait.seenAt > holdMs)) return false
      return state.signatures[wait.signature] !== undefined
    },
  }
}

/**
 * Whether a task's wait should show a browser notification: the same rule as
 * a terminal prompt's (docs/behavior/notifications.md), where the task
 * dashboard, when shown, is what makes a wait visible.
 */
export function shouldNotifyTaskWait({
  pane,
  dashboardShown,
  activeWorkspaceId,
  maximizedPaneId,
  browserIsActive,
}: {
  pane: TaskPaneRef | null
  dashboardShown: boolean
  activeWorkspaceId: string | null
  maximizedPaneId: string | null
  browserIsActive: boolean
}): boolean {
  if (!browserIsActive) return true
  if (dashboardShown) return false
  if (!pane || pane.workspaceId !== activeWorkspaceId) return true
  return maximizedPaneId !== null && maximizedPaneId !== pane.paneId
}

/**
 * A task wait's notification text. It identifies the task and nothing more:
 * what the agent asks, its log and its prompt stay out of the notification.
 */
export function taskWaitNotificationBody(task: Task): string {
  return `${task.agent} on ${hostLabel(task.host)}: ${taskTitle(task)}`
}

// waitStartInBrowserClock converts a wait's status_since, which the server
// dates on its own clock, to the browser's: collected_at is the server's time
// of the same collection the browser received at receivedAt.
function waitStartInBrowserClock(task: Task, host: TaskHost | undefined, receivedAt: number): number | null {
  const since = task.status_since ? Date.parse(task.status_since) : NaN
  if (Number.isNaN(since)) return null
  const collectedAt = host?.collected_at ? Date.parse(host.collected_at) : NaN
  if (Number.isNaN(collectedAt)) return since
  return since + (receivedAt - collectedAt)
}

function numberRecord(value: unknown): Record<string, number> {
  if (!value || typeof value !== 'object') return {}
  return Object.fromEntries(
    Object.entries(value).filter((entry): entry is [string, number] => typeof entry[1] === 'number' && Number.isFinite(entry[1])),
  )
}

function newest(record: Record<string, number>, max: number): Record<string, number> {
  const entries = Object.entries(record)
  if (entries.length <= max) return record
  return Object.fromEntries(entries.sort((a, b) => b[1] - a[1]).slice(0, max))
}

export const taskAttentionTracker = createTaskAttentionTracker(() => {
  try {
    return window.localStorage
  } catch {
    return null
  }
})
