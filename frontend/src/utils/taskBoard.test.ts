import { describe, expect, it } from 'vitest'
import type { Task, Workspace } from '../schemas'
import {
  TASK_COLUMNS,
  allLabels,
  applyTaskRecord,
  canRecord,
  columnForState,
  columnForTask,
  filterTasks,
  labelColor,
  findTaskPane,
  formatElapsed,
  formatShortcut,
  isShortcut,
  groupIntoLanes,
  hostLabel,
  isLiveState,
  laneKeys,
  paneConfigForTask,
  runningCount,
  taskOpenAction,
  taskTitle,
  visibleColumns,
  waitingCount,
} from './taskBoard'

function task(overrides: Partial<Task> = {}): Task {
  return {
    id: 'local:claude:7c21e0a4',
    host: '',
    agent: 'claude',
    session_id: '7c21e0a4-1111-2222-3333-444455556666',
    cwd: '/workspace/user/panemux',
    state: 'busy',
    location: { kind: 'tmux', tmux_session: 'task-7c21', attachable: true },
    ...overrides,
  }
}

function workspace(id: string, panes: Array<Record<string, unknown>>): Workspace {
  return {
    id,
    title: id.toUpperCase(),
    layout: {
      direction: 'horizontal',
      children: [
        { size: 50, pane: panes[0] as Workspace['layout']['children'][number]['pane'] },
        {
          size: 50,
          direction: 'vertical',
          children: panes.slice(1).map((pane) => ({ size: 50, pane: pane as never })),
        },
      ],
    },
  }
}

describe('TASK_COLUMNS', () => {
  it('puts every state in exactly one column, in the dashboard order', () => {
    expect(TASK_COLUMNS.map((column) => column.id)).toEqual(['wait', 'busy', 'idle', 'other', 'stop', 'done'])
    const states = TASK_COLUMNS.flatMap((column) => column.states)
    expect([...states].sort()).toEqual(['busy', 'idle', 'run', 'stop', 'unknown', 'wait'])
  })

  // efficacy:exempt unchanged by this branch; the new describe block after it falls inside its line range
  it('maps codex and unreadable sessions to the same column', () => {
    expect(columnForState('run')).toBe('other')
    expect(columnForState('unknown')).toBe('other')
    expect(columnForState('wait')).toBe('wait')
  })
})

describe('columnForTask', () => {
  it('puts a task marked done in the Done column only while it is stopped', () => {
    expect(columnForTask(task({ state: 'stop', done: true }))).toBe('done')
    expect(columnForTask(task({ state: 'stop' }))).toBe('stop')
    expect(columnForTask(task({ state: 'stop', done: false }))).toBe('stop')
  })

  it('keeps a running task marked done in the column of the state it reports', () => {
    expect(columnForTask(task({ state: 'wait', done: true }))).toBe('wait')
    expect(columnForTask(task({ state: 'busy', done: true }))).toBe('busy')
    expect(columnForTask(task({ state: 'idle', done: true }))).toBe('idle')
    expect(columnForTask(task({ state: 'unknown', done: true }))).toBe('other')
  })
})

describe('visibleColumns', () => {
  it('shows the Done column only when asked to', () => {
    expect(visibleColumns(false).map((column) => column.id)).toEqual(['wait', 'busy', 'idle', 'other', 'stop'])
    expect(visibleColumns(true).map((column) => column.id)).toEqual(['wait', 'busy', 'idle', 'other', 'stop', 'done'])
  })
})

describe('canRecord', () => {
  it('allows done and labels only on a task with a session id', () => {
    expect(canRecord(task())).toBe(true)
    expect(canRecord(task({ agent: 'codex', session_id: undefined, id: 'local:codex:pid-8' }))).toBe(false)
    expect(canRecord(task({ session_id: '' }))).toBe(false)
  })
})

describe('allLabels', () => {
  it('lists every label once, sorted', () => {
    expect(allLabels([
      task({ labels: ['zeta', 'alpha'] }),
      task({ labels: ['alpha', 'Beta'] }),
      task({ labels: undefined }),
      task({ labels: [] }),
    ])).toEqual(['alpha', 'Beta', 'zeta'])
    expect(allLabels([])).toEqual([])
  })
})

describe('labelColor', () => {
  it('gives a label the same color every time', () => {
    expect(labelColor('payment')).toBe(labelColor('payment'))
    expect(labelColor('payment')).toMatch(/^#[0-9a-f]{6}$/)
  })

  it('spreads different labels over the palette', () => {
    const colors = new Set(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'docs', 'infra'].map(labelColor))
    expect(colors.size).toBeGreaterThan(3)
  })
})

describe('applyTaskRecord', () => {
  it('sets done and labels on the task the record names, and only on it', () => {
    const tasks = [
      task({ id: 'l', host: '', session_id: 's1' }),
      task({ id: 'r', host: 'dev-server', session_id: 's1' }),
      task({ id: 'c', host: '', agent: 'codex', session_id: 's1' }),
    ]
    const next = applyTaskRecord(tasks, { host: '', agent: 'claude', session_id: 's1', done: true, labels: ['x'] })
    expect(next.map((t) => [t.id, t.done, t.labels])).toEqual([
      ['l', true, ['x']],
      ['r', undefined, undefined],
      ['c', undefined, undefined],
    ])
    expect(tasks[0].done).toBeUndefined()
  })

  it('clears done and labels with an empty record', () => {
    const tasks = [task({ id: 'l', session_id: 's1', done: true, labels: ['x'] })]
    const next = applyTaskRecord(tasks, { host: '', agent: 'claude', session_id: 's1', done: false, labels: [] })
    expect(next[0].done).toBe(false)
    expect(next[0].labels).toEqual([])
  })
})

describe('hostLabel', () => {
  it('names the panemux host Local and keeps ssh connection names', () => {
    expect(hostLabel('')).toBe('Local')
    expect(hostLabel('dev-server')).toBe('dev-server')
  })
})

describe('taskTitle', () => {
  it('uses the last directory of the working directory', () => {
    expect(taskTitle(task({ cwd: '/workspace/user/panemux/' }))).toBe('panemux')
    expect(taskTitle(task({ cwd: '/' }))).toBe('/')
  })

  it('falls back to the session id, then to what kind of task it is', () => {
    expect(taskTitle(task({ cwd: undefined }))).toBe('Session 7c21e0a4')
    expect(taskTitle(task({ cwd: undefined, session_id: undefined, state: 'unknown' }))).toBe(
      'Unreadable session state',
    )
    expect(taskTitle(task({ cwd: undefined, session_id: undefined, agent: 'codex', state: 'run' }))).toBe(
      'codex session',
    )
  })
})

describe('filterTasks', () => {
  const tasks = [
    task({ id: 'a', cwd: '/workspace/user/panemux', git: { branch: 'feature/task-dashboard', pr_number: 253 } }),
    task({ id: 'b', host: 'dev-server', cwd: '/remote/home/demo/payment', git: { branch: 'PAY-418-retry' } }),
    task({ id: 'c', host: 'dev-server', cwd: undefined, session_id: 'ffee0011' }),
  ]

  // efficacy:exempt only the filter argument gained label: null; it pins stage 1 behavior this branch leaves as it was
  it('matches title, directory, branch, PR number and session id case-insensitively', () => {
    const ids = (query: string) => filterTasks(tasks, { query, host: null, label: null }).map((t) => t.id)
    expect(ids('')).toEqual(['a', 'b', 'c'])
    expect(ids('PANEMUX')).toEqual(['a'])
    expect(ids('pay-418')).toEqual(['b'])
    expect(ids('#253')).toEqual(['a'])
    expect(ids('253')).toEqual(['a'])
    expect(ids('ffee')).toEqual(['c'])
    expect(ids('  demo  ')).toEqual(['b'])
    expect(ids('nothing')).toEqual([])
  })

  // efficacy:exempt only the filter argument gained label: null; it pins stage 1 behavior this branch leaves as it was
  it('filters by host, where the empty name is the panemux host', () => {
    expect(filterTasks(tasks, { query: '', host: '', label: null }).map((t) => t.id)).toEqual(['a'])
    expect(filterTasks(tasks, { query: '', host: 'dev-server', label: null }).map((t) => t.id)).toEqual(['b', 'c'])
  })

  it('filters by label, together with the host and the query', () => {
    const labeled = [
      task({ id: 'a', labels: ['payment', 'sprint-42'] }),
      task({ id: 'b', host: 'dev-server', labels: ['sprint-42'] }),
      task({ id: 'c', labels: undefined }),
    ]
    const ids = (filter: { query?: string; host?: string | null; label: string | null }) =>
      filterTasks(labeled, { query: '', host: null, ...filter }).map((t) => t.id)
    expect(ids({ label: null })).toEqual(['a', 'b', 'c'])
    expect(ids({ label: 'sprint-42' })).toEqual(['a', 'b'])
    expect(ids({ label: 'payment' })).toEqual(['a'])
    expect(ids({ label: 'Payment' })).toEqual([])
    expect(ids({ label: 'sprint-42', host: 'dev-server' })).toEqual(['b'])
    expect(ids({ label: 'sprint-42', query: 'nothing' })).toEqual([])
  })
})

describe('lanes', () => {
  it('keys a task by each of its labels, or No label', () => {
    expect(laneKeys(task({ labels: ['payment', 'sprint-42'] }), 'label')).toEqual(['payment', 'sprint-42'])
    expect(laneKeys(task({ labels: [] }), 'label')).toEqual(['No label'])
    expect(laneKeys(task({ labels: undefined }), 'label')).toEqual(['No label'])
  })

  it('shows a task with two labels in both label lanes, and No label last', () => {
    const lanes = groupIntoLanes(
      [
        task({ id: '1', labels: ['sprint-42', 'payment'] }),
        task({ id: '2' }),
        task({ id: '3', labels: ['infra', 'sprint-42'] }),
      ],
      'label',
    )
    expect(lanes.map((lane) => [lane.key, lane.tasks.map((t) => t.id)])).toEqual([
      ['infra', ['3']],
      ['payment', ['1']],
      ['sprint-42', ['1', '3']],
      ['No label', ['2']],
    ])
  })

  it('keys a task by host or repository', () => {
    expect(laneKeys(task({ host: '' }), 'host')).toEqual(['Local'])
    expect(laneKeys(task({ git: { repo: 'panemux' } }), 'repo')).toEqual(['panemux'])
    expect(laneKeys(task({ git: undefined }), 'repo')).toEqual(['Not in a Git repository'])
    expect(laneKeys(task(), 'none')).toEqual([''])
  })

  it('sorts lanes by name and keeps the catch-all lane last', () => {
    const lanes = groupIntoLanes(
      [
        task({ id: '1', git: { repo: 'zeta' } }),
        task({ id: '2' }),
        task({ id: '3', git: { repo: 'alpha' } }),
        task({ id: '4', git: { repo: 'zeta' } }),
      ],
      'repo',
    )
    expect(lanes.map((lane) => [lane.key, lane.tasks.map((t) => t.id)])).toEqual([
      ['alpha', ['3']],
      ['zeta', ['1', '4']],
      ['Not in a Git repository', ['2']],
    ])
  })

  it('is a single unnamed lane when not split', () => {
    const lanes = groupIntoLanes([task({ id: '1' }), task({ id: '2' })], 'none')
    expect(lanes).toEqual([{ key: '', tasks: [task({ id: '1' }), task({ id: '2' })] }])
  })
})

describe('findTaskPane', () => {
  const workspaces = [
    workspace('main', [
      { id: 'p-shell', type: 'local' },
      { id: 'p-docs', type: 'tmux', tmux_session: 'docs', title: 'docs' },
    ]),
    workspace('remote', [
      { id: 'p-infra', type: 'ssh_tmux', connection: 'dev-server', tmux_session: 'infra' },
      { id: 'p-other', type: 'ssh_tmux', connection: 'gpu-box', tmux_session: 'docs' },
    ]),
  ]

  it('finds a local tmux pane attached to the same session', () => {
    const found = findTaskPane(task({ location: { kind: 'tmux', tmux_session: 'docs', attachable: true } }), workspaces)
    expect(found).toEqual({ paneId: 'p-docs', paneTitle: 'docs', workspaceId: 'main', workspaceTitle: 'MAIN' })
  })

  it('finds an ssh_tmux pane only on the same connection', () => {
    const onDev = task({ host: 'dev-server', location: { kind: 'tmux', tmux_session: 'infra', attachable: true } })
    expect(findTaskPane(onDev, workspaces)?.paneId).toBe('p-infra')
    expect(findTaskPane({ ...onDev, host: 'gpu-box' }, workspaces)).toBeNull()
    const docsOnGpu = task({ host: 'gpu-box', location: { kind: 'tmux', tmux_session: 'docs', attachable: true } })
    expect(findTaskPane(docsOnGpu, workspaces)?.paneId).toBe('p-other')
  })

  it('never matches a task that is not in tmux', () => {
    expect(findTaskPane(task({ location: { kind: 'outside', attachable: false } }), workspaces)).toBeNull()
    expect(findTaskPane(task({ location: { kind: 'none', attachable: false } }), workspaces)).toBeNull()
    expect(findTaskPane(task({ location: { kind: 'tmux', attachable: false } }), workspaces)).toBeNull()
  })

  it('uses the pane id as the title when the pane has none', () => {
    const found = findTaskPane(
      task({ host: 'dev-server', location: { kind: 'tmux', tmux_session: 'infra', attachable: true } }),
      workspaces,
    )
    expect(found?.paneTitle).toBe('p-infra')
  })
})

describe('taskOpenAction', () => {
  it('goes to an existing pane, opens a new one, or says why it cannot', () => {
    const pane = { paneId: 'p', paneTitle: 'p', workspaceId: 'w', workspaceTitle: 'W' }
    expect(taskOpenAction(task(), pane)).toEqual({ kind: 'goto', pane })
    expect(taskOpenAction(task(), null)).toEqual({ kind: 'open' })
    expect(taskOpenAction(task({ location: { kind: 'tmux', tmux_session: 'my work', attachable: false } }), null))
      .toEqual({ kind: 'unavailable', reason: 'tmux session name cannot be attached from a pane' })
    expect(taskOpenAction(task({ location: { kind: 'outside', attachable: false } }), null))
      .toEqual({ kind: 'unavailable', reason: 'running outside tmux' })
    expect(taskOpenAction(task({ state: 'stop', location: { kind: 'none', attachable: false } }), null))
      .toEqual({ kind: 'unavailable', reason: 'not running' })
  })
})

describe('paneConfigForTask', () => {
  it('builds a tmux pane for the panemux host and an ssh_tmux pane for a connection', () => {
    expect(paneConfigForTask(task(), 'pane-1')).toEqual({
      id: 'pane-1', type: 'tmux', tmux_session: 'task-7c21', title: 'task-7c21',
    })
    expect(paneConfigForTask(task({ host: 'dev-server' }), 'pane-2')).toEqual({
      id: 'pane-2', type: 'ssh_tmux', connection: 'dev-server', tmux_session: 'task-7c21', title: 'task-7c21',
    })
  })

  it('refuses a task that has no attachable tmux session', () => {
    expect(paneConfigForTask(task({ location: { kind: 'outside', attachable: false } }), 'x')).toBeNull()
    expect(paneConfigForTask(task({ location: { kind: 'tmux', tmux_session: 'a b', attachable: false } }), 'x'))
      .toBeNull()
  })
})

describe('counts', () => {
  const tasks = [
    task({ id: '1', state: 'wait' }),
    task({ id: '2', state: 'busy' }),
    task({ id: '3', state: 'run', host: 'dev-server' }),
    task({ id: '4', state: 'stop' }),
    task({ id: '5', state: 'unknown' }),
    task({ id: '6', state: 'wait', host: 'dev-server' }),
  ]

  it('counts running agents per host', () => {
    expect(runningCount(tasks, '')).toBe(2)
    expect(runningCount(tasks, 'dev-server')).toBe(2)
    expect(runningCount(tasks, 'gpu-box')).toBe(0)
  })

  it('counts every task waiting for input', () => {
    expect(waitingCount(tasks)).toBe(2)
  })

  it('treats only process-backed states as live', () => {
    expect(['busy', 'wait', 'idle', 'run', 'unknown', 'stop'].filter((s) => isLiveState(s as Task['state'])))
      .toEqual(['busy', 'wait', 'idle', 'run'])
  })
})

describe('formatElapsed', () => {
  const now = Date.parse('2026-09-25T12:00:00Z')
  it.each([
    ['2026-09-25T11:59:40Z', 'just now'],
    ['2026-09-25T11:59:00Z', '1m'],
    ['2026-09-25T11:57:00Z', '3m'],
    ['2026-09-25T10:00:00Z', '2h'],
    ['2026-09-23T12:00:00Z', '2d'],
    ['2026-09-25T12:05:00Z', 'just now'],
  ])('%s is %s', (since, expected) => {
    expect(formatElapsed(since, now)).toBe(expected)
  })

  it('is empty without a time or with one that does not parse', () => {
    expect(formatElapsed(undefined, now)).toBe('')
    expect(formatElapsed('yesterday', now)).toBe('')
  })
})

describe('shortcuts', () => {
  const key = (init: KeyboardEventInit) => new KeyboardEvent('keydown', init)

  it('matches Cmd or Ctrl with Shift and the letter, in either case', () => {
    expect(isShortcut(key({ key: 'S', ctrlKey: true, shiftKey: true }), 'S')).toBe(true)
    expect(isShortcut(key({ key: 's', metaKey: true, shiftKey: true }), 'S')).toBe(true)
    expect(isShortcut(key({ key: 'J', ctrlKey: true, shiftKey: true }), 'j')).toBe(true)
  })

  it('does not match without both modifiers or with another letter', () => {
    expect(isShortcut(key({ key: 'S', shiftKey: true }), 'S')).toBe(false)
    expect(isShortcut(key({ key: 's', ctrlKey: true }), 'S')).toBe(false)
    expect(isShortcut(key({ key: 'K', ctrlKey: true, shiftKey: true }), 'S')).toBe(false)
  })

  it('labels the shortcut for the platform', () => {
    expect(formatShortcut('S', true)).toBe('⌘⇧S')
    expect(formatShortcut('s', false)).toBe('Ctrl+Shift+S')
  })
})
