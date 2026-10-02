import { describe, expect, it } from 'vitest'
import { createTaskAttentionTracker, shouldNotifyTaskWait, taskWaitNotificationBody, TASK_ATTENTION_STORAGE_KEY, TERMINAL_WAIT_TOLERANCE_MS } from './taskAttention'
import type { TaskPaneRef } from './taskBoard'
import type { Task, TaskHost } from '../schemas'

function memoryStorage(): Storage {
  const data = new Map<string, string>()
  return {
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => {
      data.set(key, value)
    },
    removeItem: (key) => {
      data.delete(key)
    },
    clear: () => data.clear(),
    key: (index) => [...data.keys()][index] ?? null,
    get length() {
      return data.size
    },
  }
}

// Host and browser clocks agree in these tests unless a case says otherwise:
// collected_at is the browser time the snapshot arrived.
const T0 = Date.parse('2026-10-01T10:00:00Z')

function iso(ms: number): string {
  return new Date(ms).toISOString()
}

function waitTask(id: string, overrides: Partial<Task> = {}): Task {
  return {
    id,
    host: '',
    agent: 'claude',
    state: 'wait',
    session_id: `session-${id}`,
    cwd: `/workspace/user/${id}`,
    wait_signature: `w1-${id}-1`,
    status_since: iso(T0),
    location: { kind: 'tmux', tmux_session: id, attachable: true },
    ...overrides,
  } as Task
}

function okHost(name: string, collectedAt: number): TaskHost {
  return { name, status: 'ok', collected_at: iso(collectedAt) }
}

function snapshot(tasks: Task[], at: number, hosts: TaskHost[] = [okHost('', at)]) {
  return { hosts, tasks }
}

function freshTracker() {
  const storage = memoryStorage()
  return createTaskAttentionTracker(() => storage)
}

const paneMain: TaskPaneRef = { paneId: 'main', paneTitle: 'main', workspaceId: 'dev', workspaceTitle: 'Dev' }

function paneFor(map: Record<string, TaskPaneRef>) {
  return (task: Task) => map[task.id] ?? null
}

describe('createTaskAttentionTracker', () => {
  it('reports a new signed wait once, as a notification candidate', () => {
    const tracker = freshTracker()
    const first = tracker.applySnapshot(snapshot([waitTask('a')], T0 + 1000), T0 + 1000, paneFor({ a: paneMain }))
    expect(first).toEqual([{ task: expect.objectContaining({ id: 'a' }), pane: paneMain, notify: true }])

    const again = tracker.applySnapshot(snapshot([waitTask('a')], T0 + 16000), T0 + 16000, paneFor({ a: paneMain }))
    expect(again).toEqual([])
  })

  it('does not notify a signature already notified, after a reload, but raises attention again', () => {
    const storage = memoryStorage()
    createTaskAttentionTracker(() => storage).applySnapshot(snapshot([waitTask('a')], T0 + 1000), T0 + 1000, paneFor({}))

    const reloaded = createTaskAttentionTracker(() => storage)
    const events = reloaded.applySnapshot(snapshot([waitTask('a')], T0 + 2000), T0 + 2000, paneFor({ a: paneMain }))
    expect(events).toEqual([{ task: expect.objectContaining({ id: 'a' }), pane: paneMain, notify: false }])
  })

  it('reports a new wait after the previous one ended', () => {
    const tracker = freshTracker()
    tracker.applySnapshot(snapshot([waitTask('a')], T0 + 1000), T0 + 1000, paneFor({}))
    expect(tracker.applySnapshot(snapshot([waitTask('a', { state: 'busy', wait_signature: undefined })], T0 + 2000), T0 + 2000, paneFor({}))).toEqual([])

    const next = tracker.applySnapshot(
      snapshot([waitTask('a', { wait_signature: 'w1-a-2', status_since: iso(T0 + 2500) })], T0 + 3000),
      T0 + 3000,
      paneFor({}),
    )
    expect(next).toEqual([{ task: expect.objectContaining({ id: 'a' }), pane: null, notify: true }])
  })

  it('reports a second wait that follows the first without a non-wait snapshot between them', () => {
    const tracker = freshTracker()
    tracker.applySnapshot(snapshot([waitTask('a')], T0 + 1000), T0 + 1000, paneFor({}))
    const next = tracker.applySnapshot(snapshot([waitTask('a', { wait_signature: 'w1-a-2' })], T0 + 2000), T0 + 2000, paneFor({}))
    expect(next.map((event) => event.notify)).toEqual([true])
  })

  it('raises attention for an unsigned wait without making it a notification candidate, once', () => {
    const tracker = freshTracker()
    const task = waitTask('a', { wait_signature: undefined })
    expect(tracker.applySnapshot(snapshot([task], T0 + 1000), T0 + 1000, paneFor({ a: paneMain }))).toEqual([
      { task: expect.objectContaining({ id: 'a' }), pane: paneMain, notify: false },
    ])
    expect(tracker.applySnapshot(snapshot([task], T0 + 2000), T0 + 2000, paneFor({ a: paneMain }))).toEqual([])
  })

  it('reports every waiting task of a snapshot, each on its own pane', () => {
    const tracker = freshTracker()
    const side: TaskPaneRef = { ...paneMain, paneId: 'side' }
    const events = tracker.applySnapshot(
      snapshot([waitTask('a'), waitTask('b'), waitTask('c', { state: 'busy', wait_signature: undefined })], T0 + 1000),
      T0 + 1000,
      paneFor({ a: paneMain, b: side }),
    )
    expect(events.map((event) => [event.task.id, event.pane?.paneId ?? null, event.notify])).toEqual([
      ['a', 'main', true],
      ['b', 'side', true],
    ])
  })

  it('keeps a failed host’s waits as they were, so its recovery does not report them again', () => {
    const tracker = freshTracker()
    const remote = waitTask('r', { host: 'dev-server' })
    tracker.applySnapshot(
      snapshot([remote], T0 + 1000, [okHost('', T0 + 1000), okHost('dev-server', T0 + 1000)]),
      T0 + 1000,
      paneFor({}),
    )

    const failed = tracker.applySnapshot(
      snapshot([waitTask('l')], T0 + 2000, [okHost('', T0 + 2000), { name: 'dev-server', status: 'error', error: 'timeout' }]),
      T0 + 2000,
      paneFor({}),
    )
    // The healthy host still reports its new wait while the other one fails.
    expect(failed.map((event) => event.task.id)).toEqual(['l'])

    const recovered = tracker.applySnapshot(
      snapshot([waitTask('l'), remote], T0 + 3000, [okHost('', T0 + 3000), okHost('dev-server', T0 + 3000)]),
      T0 + 3000,
      paneFor({}),
    )
    expect(recovered).toEqual([])
  })

  it('ignores a snapshot older than the last one applied', () => {
    const tracker = freshTracker()
    tracker.applySnapshot(snapshot([waitTask('a', { state: 'busy', wait_signature: undefined })], T0 + 5000), T0 + 5000, paneFor({}))
    expect(tracker.applySnapshot(snapshot([waitTask('a')], T0 + 4000), T0 + 4000, paneFor({}))).toEqual([])
  })

  describe('with terminal output detection on the same pane', () => {
    it('does not notify a task wait that began before a terminal prompt already reported it', () => {
      const tracker = freshTracker()
      expect(tracker.noteTerminalPrompt('main', T0 + 200)).toBe(false)
      const events = tracker.applySnapshot(snapshot([waitTask('a')], T0 + 5000), T0 + 5000, paneFor({ a: paneMain }))
      expect(events.map((event) => event.notify)).toEqual([false])
    })

    it('treats a terminal prompt within the tolerance before the recorded wait start as the same wait', () => {
      const tracker = freshTracker()
      tracker.noteTerminalPrompt('main', T0 - TERMINAL_WAIT_TOLERANCE_MS)
      const events = tracker.applySnapshot(snapshot([waitTask('a')], T0 + 5000), T0 + 5000, paneFor({ a: paneMain }))
      expect(events.map((event) => event.notify)).toEqual([false])
    })

    it('notifies a task wait that began after an earlier terminal prompt', () => {
      const tracker = freshTracker()
      tracker.noteTerminalPrompt('main', T0 - TERMINAL_WAIT_TOLERANCE_MS - 1)
      const events = tracker.applySnapshot(snapshot([waitTask('a')], T0 + 5000), T0 + 5000, paneFor({ a: paneMain }))
      expect(events.map((event) => event.notify)).toEqual([true])
    })

    it('notifies when the terminal prompt was on another pane', () => {
      const tracker = freshTracker()
      tracker.noteTerminalPrompt('side', T0 + 200)
      const events = tracker.applySnapshot(snapshot([waitTask('a')], T0 + 5000), T0 + 5000, paneFor({ a: paneMain }))
      expect(events.map((event) => event.notify)).toEqual([true])
    })

    it('converts the wait start through the host clock before comparing', () => {
      const tracker = freshTracker()
      // The server clock runs 60 s behind the browser's: the wait the server
      // dates T0 began at browser time T0 + 60 s, after the prompt at T0 + 30 s.
      tracker.noteTerminalPrompt('main', T0 + 30000)
      const received = T0 + 65000
      const events = tracker.applySnapshot(snapshot([waitTask('a')], received, [okHost('', received - 60000)]), received, paneFor({ a: paneMain }))
      expect(events.map((event) => event.notify)).toEqual([true])
    })

    it('remembers the terminal prompt across a reload', () => {
      const storage = memoryStorage()
      createTaskAttentionTracker(() => storage).noteTerminalPrompt('main', T0 + 200)
      const events = createTaskAttentionTracker(() => storage).applySnapshot(
        snapshot([waitTask('a')], T0 + 5000),
        T0 + 5000,
        paneFor({ a: paneMain }),
      )
      expect(events.map((event) => event.notify)).toEqual([false])
    })

    it('reports a terminal prompt on a pane whose signed task wait was already reported as the same wait', () => {
      const tracker = freshTracker()
      tracker.applySnapshot(snapshot([waitTask('a')], T0 + 1000), T0 + 1000, paneFor({ a: paneMain }))
      expect(tracker.noteTerminalPrompt('main', T0 + 2000)).toBe(true)
      expect(tracker.noteTerminalPrompt('side', T0 + 2000)).toBe(false)
    })

    it('keeps a pane’s reported wait while its host fails to answer', () => {
      const tracker = freshTracker()
      const remote = waitTask('r', { host: 'dev-server' })
      const hosts = (status: 'ok' | 'error', at: number) => [okHost('', at), status === 'ok' ? okHost('dev-server', at) : { name: 'dev-server', status }]
      tracker.applySnapshot(snapshot([remote], T0 + 1000, hosts('ok', T0 + 1000)), T0 + 1000, paneFor({ r: paneMain }))
      tracker.applySnapshot(snapshot([], T0 + 2000, hosts('error', T0 + 2000)), T0 + 2000, paneFor({ r: paneMain }))
      expect(tracker.noteTerminalPrompt('main', T0 + 3000)).toBe(true)

      tracker.applySnapshot(snapshot([], T0 + 4000, hosts('ok', T0 + 4000)), T0 + 4000, paneFor({ r: paneMain }))
      expect(tracker.noteTerminalPrompt('main', T0 + 5000)).toBe(false)
    })

    it('does not count an unsigned or ended task wait as the terminal prompt’s wait', () => {
      const tracker = freshTracker()
      tracker.applySnapshot(snapshot([waitTask('a', { wait_signature: undefined })], T0 + 1000), T0 + 1000, paneFor({ a: paneMain }))
      expect(tracker.noteTerminalPrompt('main', T0 + 2000)).toBe(false)

      tracker.applySnapshot(snapshot([waitTask('b')], T0 + 3000), T0 + 3000, paneFor({ b: paneMain }))
      tracker.applySnapshot(snapshot([waitTask('b', { state: 'busy', wait_signature: undefined })], T0 + 4000), T0 + 4000, paneFor({ b: paneMain }))
      expect(tracker.noteTerminalPrompt('main', T0 + 5000)).toBe(false)
    })
  })

  it('keeps the stored signatures bounded, dropping the oldest', () => {
    const storage = memoryStorage()
    const tracker = createTaskAttentionTracker(() => storage, { maxEntries: 2 })
    tracker.applySnapshot(snapshot([waitTask('a')], T0 + 1000), T0 + 1000, paneFor({}))
    tracker.applySnapshot(snapshot([waitTask('b')], T0 + 2000), T0 + 2000, paneFor({}))
    tracker.applySnapshot(snapshot([waitTask('c')], T0 + 3000), T0 + 3000, paneFor({}))

    const stored = JSON.parse(storage.getItem(TASK_ATTENTION_STORAGE_KEY) ?? '{}') as { signatures: Record<string, number> }
    expect(Object.keys(stored.signatures).sort()).toEqual(['w1-b-1', 'w1-c-1'])
  })

  it('falls back to memory when storage is unavailable or corrupt', () => {
    const tracker = createTaskAttentionTracker(() => null)
    tracker.applySnapshot(snapshot([waitTask('a')], T0 + 1000), T0 + 1000, paneFor({}))
    expect(tracker.applySnapshot(snapshot([waitTask('a', { id: 'a2' })], T0 + 2000), T0 + 2000, paneFor({})).map((e) => e.notify)).toEqual([false])

    const corrupt = memoryStorage()
    corrupt.setItem(TASK_ATTENTION_STORAGE_KEY, '{not json')
    const events = createTaskAttentionTracker(() => corrupt).applySnapshot(snapshot([waitTask('a')], T0 + 1000), T0 + 1000, paneFor({}))
    expect(events.map((event) => event.notify)).toEqual([true])
  })
})

describe('shouldNotifyTaskWait', () => {
  const base = { dashboardShown: false, activeWorkspaceId: 'dev', maximizedPaneId: null, browserIsActive: true }
  it.each([
    ['inactive browser, pane visible', { ...base, browserIsActive: false, pane: paneMain }, true],
    ['inactive browser, dashboard shown', { ...base, browserIsActive: false, dashboardShown: true, pane: null }, true],
    ['active browser, pane visible in the active workspace', { ...base, pane: paneMain }, false],
    ['active browser, pane in another workspace', { ...base, activeWorkspaceId: 'ops', pane: paneMain }, true],
    ['active browser, pane hidden by another maximized pane', { ...base, maximizedPaneId: 'side', pane: paneMain }, true],
    ['active browser, the pane itself maximized', { ...base, maximizedPaneId: 'main', pane: paneMain }, false],
    ['active browser, no pane', { ...base, pane: null }, true],
    ['active browser, dashboard shown, no pane', { ...base, dashboardShown: true, pane: null }, false],
    ['active browser, dashboard shown over the pane', { ...base, dashboardShown: true, pane: paneMain }, false],
  ])('%s', (_name, input, expected) => {
    expect(shouldNotifyTaskWait(input)).toBe(expected)
  })
})

describe('taskWaitNotificationBody', () => {
  it('names only the agent, the host and the task, never why it waits', () => {
    const body = taskWaitNotificationBody(waitTask('a', { host: 'dev-server', agent: 'codex', waiting_for: 'secret prompt text' }))
    expect(body).toBe('codex on dev-server: a')
    expect(taskWaitNotificationBody(waitTask('a', { cwd: undefined, session_id: 'abcdef1234' }))).toBe('claude on Local: Session abcdef12')
  })
})
