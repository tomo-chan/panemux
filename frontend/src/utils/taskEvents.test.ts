import { describe, expect, it } from 'vitest'
import type { TaskEventTask } from '../schemas'
import {
  applyTaskEventFrame,
  isWaitVisible,
  taskWaitNotification,
  attentionAfterFrame,
  createWaitIdRecord,
  overlayTaskEvents,
  taskWaitStarts,
  type TaskEventStore,
} from './taskEvents'
import type { Task } from '../schemas'

const EPOCH = 'e0'

function view(id: string, overrides: Partial<TaskEventTask> = {}): TaskEventTask {
  return {
    id,
    host: '',
    agent: 'claude',
    session_id: id,
    cwd: '/workspace/user/project',
    state: 'busy',
    location: { kind: 'tmux', tmux_session: `s-${id}`, attachable: true },
    ...overrides,
  }
}

function waiting(id: string, waitId: string, overrides: Partial<TaskEventTask> = {}): TaskEventTask {
  return view(id, { state: 'wait', waiting_for: 'input needed', wait_id: waitId, ...overrides })
}

function snapshot(seq: number, tasks: TaskEventTask[], hosts = [{ name: '', status: 'ok' }], epoch = EPOCH) {
  return { type: 'snapshot', epoch, seq, hosts, tasks }
}

function taskFrame(seq: number, op: 'added' | 'changed' | 'removed', task: TaskEventTask, prevState?: string, epoch = EPOCH) {
  return { type: 'task', epoch, seq, op, task, ...(prevState ? { prev_state: prevState } : {}) }
}

function storeOf(frames: unknown[]): TaskEventStore {
  let store: TaskEventStore | null = null
  for (const frame of frames) {
    const result = applyTaskEventFrame(store, frame)
    if (!result.ok) throw new Error(`frame rejected: ${result.reason}`)
    store = result.store
  }
  if (!store) throw new Error('no frames')
  return store
}

describe('applyTaskEventFrame', () => {
  it('starts from a snapshot and applies each change after it in order', () => {
    const store = storeOf([
      snapshot(10, [view('a')], [{ name: '', status: 'ok' }, { name: 'gpu', status: 'pending' }]),
      taskFrame(11, 'changed', waiting('a', 'w1-a'), 'busy'),
      taskFrame(12, 'added', view('b', { host: 'gpu' })),
      { type: 'host', epoch: EPOCH, seq: 13, op: 'changed', host: { name: 'gpu', status: 'error', error: 'timeout' } },
      taskFrame(14, 'removed', view('b', { host: 'gpu' }), 'busy'),
      { type: 'host', epoch: EPOCH, seq: 15, op: 'added', host: { name: 'box', status: 'pending' } },
      { type: 'host', epoch: EPOCH, seq: 16, op: 'removed', host: { name: 'box', status: 'pending' } },
    ])

    expect(store.seq).toBe(16)
    expect(store.epoch).toBe(EPOCH)
    expect([...store.tasks.keys()]).toEqual(['a'])
    expect(store.tasks.get('a')?.wait_id).toBe('w1-a')
    expect(store.hosts.get('gpu')).toEqual({ name: 'gpu', status: 'error', error: 'timeout' })
    expect(store.hosts.has('box')).toBe(false)
  })

  it('replaces everything it held when a snapshot arrives', () => {
    const store = storeOf([
      snapshot(1, [view('a'), view('b')]),
      snapshot(40, [view('c')], [], 'e1'),
    ])
    expect([...store.tasks.keys()]).toEqual(['c'])
    expect(store.hosts.size).toBe(0)
    expect(store).toMatchObject({ epoch: 'e1', seq: 40 })
  })

  it.each([
    ['a change before any snapshot', null, taskFrame(1, 'added', view('a'))],
    ['a gap in seq', 'store', taskFrame(12, 'added', view('b'))],
    ['seq going back', 'store', taskFrame(10, 'added', view('b'))],
    ['the same seq again', 'store', taskFrame(10, 'added', view('b'))],
    ['a different epoch', 'store', taskFrame(11, 'added', view('b'), undefined, 'other')],
    ['an unknown type', 'store', { type: 'heartbeat', epoch: EPOCH, seq: 11 }],
    ['a frame failing its schema', 'store', { type: 'task', epoch: EPOCH, seq: 11, op: 'added', task: { id: 'b' } }],
    ['a task state that is never published', 'store', taskFrame(11, 'added', view('b', { state: 'stop' as never }))],
    ['something that is not an object', 'store', 'nonsense'],
  ])('asks for a fresh snapshot on %s', (_name, start, frame) => {
    const base = start === 'store' ? storeOf([snapshot(10, [view('a')])]) : null
    const result = applyTaskEventFrame(base, frame)
    expect(result.ok).toBe(false)
  })
})

describe('taskWaitStarts', () => {
  const before = storeOf([snapshot(1, [view('a'), waiting('w', 'w1-old')])])

  it.each([
    ['every waiting task in a snapshot', snapshot(5, [waiting('a', 'w1-a'), view('b'), waiting('c', 'e1-x-3')]), ['a', 'c']],
    ['a task added already waiting', taskFrame(2, 'added', waiting('n', 'w1-n')), ['n']],
    ['a task changed into wait', taskFrame(2, 'changed', waiting('a', 'w1-a'), 'busy'), ['a']],
    ['a waiting task given a new wait_id', taskFrame(2, 'changed', waiting('w', 'w1-new'), 'wait'), ['w']],
    ['a waiting task whose other fields changed', taskFrame(2, 'changed', waiting('w', 'w1-old', { cwd: '/tmp/sample-project' }), 'wait'), []],
    ['a task added busy', taskFrame(2, 'added', view('n')), []],
    ['a task leaving wait', taskFrame(2, 'changed', view('w', { state: 'idle' }), 'wait'), []],
    ['a waiting task removed', taskFrame(2, 'removed', waiting('w', 'w1-old'), 'wait'), []],
    ['a host change', { type: 'host', epoch: EPOCH, seq: 2, op: 'changed', host: { name: '', status: 'error' } }, []],
  ])('finds %s', (_name, frame, ids) => {
    const result = applyTaskEventFrame(before, frame)
    if (!result.ok) throw new Error(result.reason)
    expect(taskWaitStarts(before, result.frame).map((task) => task.id)).toEqual(ids)
  })
})

describe('attentionAfterFrame', () => {
  const none = new Set<string>()

  function run(frames: unknown[], cleared: ReadonlySet<string> = none) {
    let store: TaskEventStore | null = null
    let flags: ReadonlyMap<string, TaskEventTask> = new Map()
    for (const raw of frames) {
      const result = applyTaskEventFrame(store, raw)
      if (!result.ok) throw new Error(result.reason)
      flags = attentionAfterFrame(flags, store, result.frame, result.store, cleared)
      store = result.store
    }
    return flags
  }

  it('flags a task when its wait starts', () => {
    const flags = run([snapshot(1, [view('a')]), taskFrame(2, 'changed', waiting('a', 'w1-a'), 'busy')])
    expect(flags.get('a')?.wait_id).toBe('w1-a')
  })

  it('flags a task already waiting in a snapshot, unless that wait was cleared in this tab', () => {
    expect([...run([snapshot(1, [waiting('a', 'w1-a'), waiting('b', 'w1-b')])], new Set(['w1-b'])).keys()]).toEqual(['a'])
  })

  it.each([
    ['busy', view('a')],
    ['idle', view('a', { state: 'idle' })],
    ['run', view('a', { state: 'run' })],
  ])('clears the flag when the wait ends in %s', (_name, task) => {
    const flags = run([snapshot(1, [waiting('a', 'w1-a')]), taskFrame(2, 'changed', task, 'wait')])
    expect(flags.size).toBe(0)
  })

  it('clears the flag when the task is removed', () => {
    const flags = run([snapshot(1, [waiting('a', 'w1-a')]), taskFrame(2, 'removed', waiting('a', 'w1-a'), 'wait')])
    expect(flags.size).toBe(0)
  })

  it('keeps the flag through unknown, following where the task now runs', () => {
    const moved = view('a', { state: 'unknown', location: { kind: 'tmux', tmux_session: 'moved', attachable: true } })
    const flags = run([snapshot(1, [waiting('a', 'w1-a')]), taskFrame(2, 'changed', moved, 'wait')])
    expect(flags.get('a')?.location.tmux_session).toBe('moved')
    expect(flags.get('a')?.wait_id).toBe('w1-a')
  })

  it('keeps the flag while the task\'s host fails', () => {
    const flags = run([
      snapshot(1, [waiting('a', 'w1-a', { host: 'gpu' })], [{ name: 'gpu', status: 'ok' }]),
      { type: 'host', epoch: EPOCH, seq: 2, op: 'changed', host: { name: 'gpu', status: 'error', error: 'x' } },
    ])
    expect(flags.has('a')).toBe(true)
  })

  it('moves the flag to the new wait when the wait_id changes', () => {
    const flags = run([snapshot(1, [waiting('a', 'w1-a')]), taskFrame(2, 'changed', waiting('a', 'w1-b'), 'wait')])
    expect(flags.get('a')?.wait_id).toBe('w1-b')
  })

  it('resyncs flags from a later snapshot', () => {
    const flags = run([
      snapshot(1, [waiting('ended', 'w1-1'), waiting('gone', 'w1-2'), waiting('unk', 'w1-3'), waiting('failing', 'w1-4', { host: 'gpu' }), waiting('same', 'w1-5')], [{ name: '', status: 'ok' }, { name: 'gpu', status: 'ok' }]),
      snapshot(30, [
        view('ended', { state: 'idle' }),
        view('unk', { state: 'unknown' }),
        waiting('same', 'w1-5'),
      ], [{ name: '', status: 'ok' }, { name: 'gpu', status: 'pending' }]),
    ])
    // Ended in idle, and gone from a host that answered: cleared. Unknown, and
    // gone from a host that has not answered yet: kept.
    expect([...flags.keys()].sort()).toEqual(['failing', 'same', 'unk'])
  })
})

describe('createWaitIdRecord', () => {
  function memoryStorage(): Storage {
    const values = new Map<string, string>()
    return {
      get length() { return values.size },
      clear: () => values.clear(),
      getItem: (key) => values.get(key) ?? null,
      key: (index) => [...values.keys()][index] ?? null,
      removeItem: (key) => { values.delete(key) },
      setItem: (key, value) => { values.set(key, value) },
    }
  }

  it('remembers IDs across instances backed by the same storage', () => {
    const storage = memoryStorage()
    createWaitIdRecord('k', () => storage).add('w1-a')
    expect(createWaitIdRecord('k', () => storage).has('w1-a')).toBe(true)
    expect(createWaitIdRecord('other', () => storage).has('w1-a')).toBe(false)
  })

  it('keeps only the IDs a snapshot still holds', () => {
    const storage = memoryStorage()
    const record = createWaitIdRecord('k', () => storage)
    record.add('w1-a')
    record.add('w1-b')
    record.retainOnly(new Set(['w1-b']))
    expect(record.has('w1-a')).toBe(false)
    expect(createWaitIdRecord('k', () => storage).has('w1-b')).toBe(true)
  })

  it('keeps at most the 500 newest', () => {
    const record = createWaitIdRecord('k', () => memoryStorage())
    for (let i = 0; i < 501; i += 1) record.add(`w1-${i}`)
    expect(record.has('w1-0')).toBe(false)
    expect(record.has('w1-1')).toBe(true)
    expect(record.has('w1-500')).toBe(true)
  })

  it('lives in memory when session storage throws', () => {
    const record = createWaitIdRecord('k', () => { throw new Error('denied') })
    record.add('w1-a')
    expect(record.has('w1-a')).toBe(true)
  })

  it('ignores a stored value that is not a list of IDs', () => {
    const storage = memoryStorage()
    storage.setItem('k', '{"not":"a list"}')
    expect(createWaitIdRecord('k', () => storage).has('not')).toBe(false)
    storage.setItem('k', '[1, "w1-a"]')
    expect(createWaitIdRecord('k', () => storage).has('w1-a')).toBe(true)
  })
})

describe('overlayTaskEvents', () => {
  const collected: Task = {
    id: 'a',
    host: '',
    agent: 'claude',
    state: 'busy',
    status_since: '2026-10-02T09:00:00Z',
    location: { kind: 'tmux', tmux_session: 's', attachable: true },
    labels: ['x'],
  }

  it('takes the state, waiting_for, wait and status_since from the stream', () => {
    const live = new Map([['a', waiting('a', 'w1-a', { status_since: '2026-10-02T10:00:00Z' })]])
    const [task] = overlayTaskEvents([collected], live)
    expect(task).toMatchObject({
      state: 'wait',
      waiting_for: 'input needed',
      wait_signature: 'w1-a',
      status_since: '2026-10-02T10:00:00Z',
      labels: ['x'],
    })
  })

  it('does not present an observed wait ID as a wait signature', () => {
    const live = new Map([['a', waiting('a', 'e1-e0-4')]])
    expect(overlayTaskEvents([collected], live)[0].wait_signature).toBeUndefined()
  })

  it('clears the wait fields when the stream says the wait ended', () => {
    const before: Task = { ...collected, state: 'wait', waiting_for: 'x', wait_signature: 'w1-a' }
    const [task] = overlayTaskEvents([before], new Map([['a', view('a', { state: 'idle' })]]))
    expect(task.state).toBe('idle')
    expect(task.waiting_for).toBeUndefined()
    expect(task.wait_signature).toBeUndefined()
  })

  it('leaves a task the stream does not have as collected', () => {
    const stopped: Task = { ...collected, id: 's', state: 'stop' }
    const tasks = [collected, stopped]
    expect(overlayTaskEvents(tasks, new Map())).toBe(tasks)
    const overlaid = overlayTaskEvents(tasks, new Map([['s', view('s')]]))
    expect(overlaid[0]).toBe(collected)
    // The stream has it running, which is newer than the collection's stop.
    expect(overlaid[1].state).toBe('busy')
  })
})

describe('isWaitVisible', () => {
  const pane = { paneId: 'p', paneTitle: 'P', workspaceId: 'w', workspaceTitle: 'W' }
  const screen = {
    browserActive: true,
    layer: 'workspaces' as const,
    activeWorkspaceId: 'w',
    maximizedPaneId: null,
    dashboardTaskIds: new Set<string>(),
  }

  it.each([
    ['its pane is on screen', pane, {}, true],
    ['its pane is the maximized one', pane, { maximizedPaneId: 'p' }, true],
    ['the browser is not active', pane, { browserActive: false }, false],
    ['its pane is in another workspace', pane, { activeWorkspaceId: 'other' }, false],
    ['another pane is maximized over it', pane, { maximizedPaneId: 'q' }, false],
    ['the dashboard covers its pane', pane, { layer: 'tasks' as const }, false],
    ['the dashboard lists it', pane, { layer: 'tasks' as const, dashboardTaskIds: new Set(['t']) }, true],
    ['it has no pane and the dashboard lists it', null, { layer: 'tasks' as const, dashboardTaskIds: new Set(['t']) }, true],
    ['it has no pane and the workspaces are shown', null, {}, false],
    ['the dashboard lists it but the browser is not active', null, { layer: 'tasks' as const, dashboardTaskIds: new Set(['t']), browserActive: false }, false],
    ['stale dashboard ids while the workspaces are shown', null, { dashboardTaskIds: new Set(['t']) }, false],
  ])('when %s: %s', (_name, paneRef, overrides, visible) => {
    expect(isWaitVisible('t', paneRef, { ...screen, ...overrides })).toBe(visible)
  })
})

describe('taskWaitNotification', () => {
  it('names the agent, the host and the directory, and not what the task waits for', () => {
    const note = taskWaitNotification(waiting('a', 'w1-a', { host: 'gpu', waiting_for: 'secret prompt text' }))
    expect(note.title).toBe('Agent waiting')
    expect(note.body).toBe('claude on gpu: project')
    expect(JSON.stringify(note)).not.toContain('secret')
  })

  it('calls the panemux host Local and leaves out a missing directory', () => {
    expect(taskWaitNotification(waiting('a', 'w1-a', { cwd: undefined })).body).toBe('claude on Local')
  })
})
