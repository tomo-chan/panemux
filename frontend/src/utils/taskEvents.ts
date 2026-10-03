import { TaskEventFrameSchema, type Task, type TaskEventFrame, type TaskEventHost, type TaskEventTask } from '../schemas'
import { hostLabel, type TaskPaneRef } from './taskBoard'

// The browser's side of the task event stream (issue #279,
// docs/behavior/task-events.md#receiver). Everything here is pure: the hook
// that owns the connection feeds frames through it, and the dashboard, the
// pane and workspace attention and the browser notifications are projections
// of what it returns.

/** What a tab holds of the stream: as of the last frame it applied. */
export interface TaskEventStore {
  epoch: string
  seq: number
  tasks: ReadonlyMap<string, TaskEventTask>
  hosts: ReadonlyMap<string, TaskEventHost>
}

export type TaskEventApplyResult =
  | { ok: true; store: TaskEventStore; frame: TaskEventFrame }
  | { ok: false; reason: string }

/**
 * Applies one frame, as received, to what the tab holds (null before the
 * connection's snapshot). Anything that is not the next frame of the same
 * stream — a gap or a step back in seq, another epoch, a type this tab does
 * not know, a frame failing its schema — is refused: the caller discards
 * what it holds and reconnects, since recovery is always a fresh snapshot.
 */
export function applyTaskEventFrame(store: TaskEventStore | null, raw: unknown): TaskEventApplyResult {
  const parsed = TaskEventFrameSchema.safeParse(raw)
  if (!parsed.success) return { ok: false, reason: 'frame failed validation' }
  const frame = parsed.data

  if (frame.type === 'snapshot') {
    return {
      ok: true,
      frame,
      store: {
        epoch: frame.epoch,
        seq: frame.seq,
        tasks: new Map(frame.tasks.map((task) => [task.id, task])),
        hosts: new Map(frame.hosts.map((host) => [host.name, host])),
      },
    }
  }
  if (!store) return { ok: false, reason: 'change before a snapshot' }
  if (frame.epoch !== store.epoch) return { ok: false, reason: 'epoch changed' }
  if (frame.seq !== store.seq + 1) return { ok: false, reason: `expected seq ${store.seq + 1}, got ${frame.seq}` }

  const next: TaskEventStore = { ...store, seq: frame.seq }
  if (frame.type === 'task') {
    const tasks = new Map(store.tasks)
    if (frame.op === 'removed') tasks.delete(frame.task.id)
    else tasks.set(frame.task.id, frame.task)
    next.tasks = tasks
  } else {
    const hosts = new Map(store.hosts)
    if (frame.op === 'removed') hosts.delete(frame.host.name)
    else hosts.set(frame.host.name, frame.host)
    next.hosts = hosts
  }
  return { ok: true, store: next, frame }
}

/**
 * The waits a frame starts: every waiting task of a snapshot, a task added
 * already waiting, a task changed into wait, and a waiting task given a new
 * wait_id. `before` is what the tab held before the frame.
 */
export function taskWaitStarts(before: TaskEventStore | null, frame: TaskEventFrame): TaskEventTask[] {
  if (frame.type === 'snapshot') return frame.tasks.filter((task) => task.state === 'wait')
  if (frame.type !== 'task' || frame.task.state !== 'wait') return []
  if (frame.op === 'added') return [frame.task]
  if (frame.op !== 'changed') return []
  const previous = before?.tasks.get(frame.task.id)
  if (frame.prev_state !== 'wait' || previous?.wait_id !== frame.task.wait_id) return [frame.task]
  return []
}

// The states that end a wait. unknown is not one: a state file being
// rewritten can read as unknown for one observation.
const WAIT_ENDING_STATES = new Set(['busy', 'idle', 'run'])

/**
 * The tasks whose wait gives their pane attention, by task id, each with the
 * latest view of the task (so a task that moved is matched where it is now).
 * A wait flags its task when it starts, unless this tab already cleared that
 * wait_id; the flag goes when the wait ends — a change to busy, idle or run,
 * or the task's removal — wherever it was answered. A change to unknown and
 * a failing host leave it.
 */
export function attentionAfterFrame(
  flags: ReadonlyMap<string, TaskEventTask>,
  before: TaskEventStore | null,
  frame: TaskEventFrame,
  after: TaskEventStore,
  cleared: Pick<ReadonlySet<string>, 'has'>,
): ReadonlyMap<string, TaskEventTask> {
  const next = new Map(flags)

  if (frame.type === 'snapshot') {
    for (const [id, flagged] of flags) {
      const now = after.tasks.get(id)
      if (now) {
        if (WAIT_ENDING_STATES.has(now.state)) next.delete(id)
        else if (now.state === 'unknown') next.set(id, { ...now, wait_id: flagged.wait_id })
        continue
      }
      // Gone from a host that has answered: the task ended. Gone from one
      // that has not answered yet, or failed: not known to have ended.
      if (after.hosts.get(flagged.host)?.status === 'ok' || !after.hosts.has(flagged.host)) next.delete(id)
    }
  } else if (frame.type === 'task') {
    const { task } = frame
    const flagged = flags.get(task.id)
    if (flagged) {
      if (frame.op === 'removed' || WAIT_ENDING_STATES.has(task.state)) next.delete(task.id)
      else if (task.state === 'unknown') next.set(task.id, { ...task, wait_id: flagged.wait_id })
      else next.set(task.id, task)
    }
  }

  for (const task of taskWaitStarts(before, frame)) {
    if (task.wait_id && cleared.has(task.wait_id)) {
      next.delete(task.id)
      continue
    }
    next.set(task.id, task)
  }
  return next
}

/** At most this many wait IDs are remembered per record. */
export const WAIT_ID_RECORD_LIMIT = 500

export interface WaitIdRecord {
  has: (waitId: string) => boolean
  add: (waitId: string) => void
  /** Drops every ID not in `waitIds`: called with a snapshot's waits. */
  retainOnly: (waitIds: ReadonlySet<string>) => void
}

/**
 * A list of wait IDs kept in this tab's session storage under `key`, which a
 * reload of the tab keeps and other tabs do not see. Without session storage
 * the list lives in the page's memory.
 */
export function createWaitIdRecord(key: string, storage: () => Storage = () => window.sessionStorage): WaitIdRecord {
  let ids = load()

  function load(): string[] {
    try {
      const value: unknown = JSON.parse(storage().getItem(key) ?? '[]')
      return Array.isArray(value) ? value.filter((id): id is string => typeof id === 'string') : []
    } catch {
      return []
    }
  }

  function save() {
    try {
      storage().setItem(key, JSON.stringify(ids))
    } catch {
      // The record stays in memory.
    }
  }

  return {
    has: (waitId) => ids.includes(waitId),
    add(waitId) {
      ids = [...ids.filter((id) => id !== waitId), waitId].slice(-WAIT_ID_RECORD_LIMIT)
      save()
    },
    retainOnly(waitIds) {
      ids = ids.filter((id) => waitIds.has(id))
      save()
    },
  }
}

/**
 * The dashboard's collected tasks with the state, waiting_for, wait and
 * status_since of every task the stream has, so a change shows as soon as it
 * is published. Everything else stays as collected.
 */
export function overlayTaskEvents(tasks: Task[], live: ReadonlyMap<string, TaskEventTask>): Task[] {
  if (live.size === 0) return tasks
  return tasks.map((task) => {
    const event = live.get(task.id)
    if (!event) return task
    return {
      ...task,
      state: event.state,
      waiting_for: event.waiting_for,
      // A wait panemux only observed (e1-…) has no signature.
      wait_signature: event.wait_id?.startsWith('w1-') ? event.wait_id : undefined,
      status_since: event.status_since ?? task.status_since,
    }
  })
}

/** What a tab shows at the moment a wait starts. */
export interface WaitScreen {
  /** The page is visible and has focus. */
  browserActive: boolean
  layer: 'tasks' | 'workspaces'
  activeWorkspaceId: string | null
  maximizedPaneId: string | null
  /** The tasks the dashboard lists right now, after its filters. */
  dashboardTaskIds: ReadonlySet<string>
}

/**
 * Whether the person can see a wait now, in which case it is not notified:
 * the browser is active and either the task's pane is on screen, or the task
 * dashboard is on screen and lists the task. A task no pane shows is visible
 * only on the dashboard.
 */
export function isWaitVisible(taskId: string, pane: TaskPaneRef | null, screen: WaitScreen): boolean {
  if (!screen.browserActive) return false
  if (screen.layer === 'tasks') return screen.dashboardTaskIds.has(taskId)
  if (!pane || pane.workspaceId !== screen.activeWorkspaceId) return false
  return !screen.maximizedPaneId || screen.maximizedPaneId === pane.paneId
}

/**
 * A wait's notification: the agent, the host and the task's directory name.
 * Never what the task waits for, conversation text or a prompt.
 */
export function taskWaitNotification(task: TaskEventTask): { title: string; body: string } {
  const directory = task.cwd?.split('/').filter(Boolean).pop()
  const where = `${task.agent} on ${hostLabel(task.host)}`
  return { title: 'Agent waiting', body: directory ? `${where}: ${directory}` : where }
}
