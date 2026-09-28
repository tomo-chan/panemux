import { describe, expect, it } from 'vitest'
import type { Task, Workspace } from '../schemas'
import {
  TASK_COLUMNS,
  columnForState,
  filterTasks,
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
    expect(TASK_COLUMNS.map((column) => column.id)).toEqual(['wait', 'busy', 'idle', 'other', 'stop'])
    const states = TASK_COLUMNS.flatMap((column) => column.states)
    expect([...states].sort()).toEqual(['busy', 'idle', 'run', 'stop', 'unknown', 'wait'])
  })

  it('maps codex and unreadable sessions to the same column', () => {
    expect(columnForState('run')).toBe('other')
    expect(columnForState('unknown')).toBe('other')
    expect(columnForState('wait')).toBe('wait')
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

  it('matches title, directory, branch, PR number and session id case-insensitively', () => {
    const ids = (query: string) => filterTasks(tasks, { query, host: null }).map((t) => t.id)
    expect(ids('')).toEqual(['a', 'b', 'c'])
    expect(ids('PANEMUX')).toEqual(['a'])
    expect(ids('pay-418')).toEqual(['b'])
    expect(ids('#253')).toEqual(['a'])
    expect(ids('253')).toEqual(['a'])
    expect(ids('ffee')).toEqual(['c'])
    expect(ids('  demo  ')).toEqual(['b'])
    expect(ids('nothing')).toEqual([])
  })

  it('matches a reference that only the PR title carried', () => {
    const withRef = [
      ...tasks,
      task({ id: 'd', git: { branch: 'fix/retry', autolinks: [{ text: 'OPS-77', url: 'https://jira.example.com/browse/OPS-77' }] } }),
    ]
    expect(filterTasks(withRef, { query: 'ops-77', host: null }).map((t) => t.id)).toEqual(['d'])
  })

  it('filters by host, where the empty name is the panemux host', () => {
    expect(filterTasks(tasks, { query: '', host: '' }).map((t) => t.id)).toEqual(['a'])
    expect(filterTasks(tasks, { query: '', host: 'dev-server' }).map((t) => t.id)).toEqual(['b', 'c'])
  })
})

describe('lanes', () => {
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
