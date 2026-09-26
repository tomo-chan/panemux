import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { TaskDashboard } from './TaskDashboard'
import type { TasksState } from '../hooks/useTasks'
import type { Task, TasksResponse, Workspace } from '../schemas'

const NOW = Date.parse('2026-09-25T12:00:00Z')

function task(overrides: Partial<Task>): Task {
  return {
    id: 'local:claude:a',
    host: '',
    agent: 'claude',
    session_id: 'aaaaaaaa-0000',
    cwd: '/workspace/user/panemux',
    state: 'busy',
    status_since: '2026-09-25T11:57:00Z',
    location: { kind: 'tmux', tmux_session: 'task-a', attachable: true },
    ...overrides,
  }
}

const response: TasksResponse = {
  hosts: [
    { name: '', status: 'ok', collected_at: '2026-09-25T12:00:00Z' },
    { name: 'dev-server', status: 'ok', collected_at: '2026-09-25T12:00:00Z' },
    { name: 'gpu-box', status: 'error', error: 'connect to gpu-box: i/o timeout' },
    { name: 'slow-box', status: 'connecting' },
  ],
  tasks: [
    task({ id: 'wait-1', state: 'wait', waiting_for: 'input needed', cwd: '/workspace/user/panemux' }),
    task({
      id: 'busy-1', state: 'busy', host: 'dev-server', cwd: '/remote/home/demo/payment',
      location: { kind: 'tmux', tmux_session: 'infra', attachable: true },
      git: { repo: 'payment', branch: 'PAY-418-retry', pr_number: 87, pr_url: 'https://github.com/example-org/payment/pull/87' },
    }),
    task({ id: 'idle-1', state: 'idle', cwd: '/workspace/user/docs', location: { kind: 'outside', attachable: false } }),
    task({ id: 'run-1', state: 'run', agent: 'codex', session_id: undefined, cwd: '/workspace/user/api' }),
    task({ id: 'stop-1', state: 'stop', cwd: '/workspace/user/old', location: { kind: 'none', attachable: false } }),
  ],
}

const workspaces: Workspace[] = [
  {
    id: 'remote',
    title: 'Remote',
    layout: {
      direction: 'horizontal',
      children: [{ size: 100, pane: { id: 'p-infra', type: 'ssh_tmux', connection: 'dev-server', tmux_session: 'infra', title: 'infra' } }],
    },
  },
]

function tasksState(overrides: Partial<TasksState> = {}): TasksState {
  return {
    data: response,
    error: null,
    loading: false,
    updatedAt: NOW - 8000,
    refresh: vi.fn().mockResolvedValue(undefined),
    reconnect: vi.fn().mockResolvedValue(undefined),
    saveRecord: vi.fn().mockResolvedValue(null),
    launch: vi.fn().mockResolvedValue({ ok: false, error: 'not stubbed' }),
    resume: vi.fn().mockResolvedValue({ ok: false, error: 'not stubbed' }),
    ...overrides,
  }
}

function renderDashboard(state: TasksState = tasksState(), onOpenTask = vi.fn(), onShowWorkspaces = vi.fn()) {
  render(
    <TaskDashboard
      tasksState={state}
      workspaces={workspaces}
      onOpenTask={onOpenTask}
      onShowWorkspaces={onShowWorkspaces}
      now={() => NOW}
    />,
  )
  return { onOpenTask, onShowWorkspaces }
}

describe('TaskDashboard', () => {
  it('places each task in its state column with a count', () => {
    renderDashboard()
    const column = (name: string) => screen.getByRole('region', { name })
    expect(within(column('Waiting for input')).getByTestId('task-card-wait-1')).toBeInTheDocument()
    expect(within(column('Working')).getByTestId('task-card-busy-1')).toBeInTheDocument()
    expect(within(column('Idle')).getByTestId('task-card-idle-1')).toBeInTheDocument()
    expect(within(column('Running / unknown')).getByTestId('task-card-run-1')).toBeInTheDocument()
    expect(within(column('Stopped')).getByTestId('task-card-stop-1')).toBeInTheDocument()
    expect(within(column('Working')).getByRole('heading', { level: 2 })).toHaveTextContent('Working1')
  })

  it('shows why a task waits and how long it has', () => {
    renderDashboard()
    const card = screen.getByTestId('task-card-wait-1')
    expect(card).toHaveTextContent('input needed · open the pane to respond')
    expect(within(card).getByTitle('Waiting for input for 3m')).toHaveTextContent('3m')
  })

  it('shows every host, a reconnect for a failed one, and running counts', () => {
    const state = tasksState()
    renderDashboard(state)
    const hosts = screen.getByRole('list', { name: 'Hosts' })
    expect(hosts).toHaveTextContent('Local 3 running')
    expect(hosts).toHaveTextContent('dev-server 1 running')
    expect(hosts).toHaveTextContent('slow-box connecting…')
    expect(hosts).toHaveTextContent('gpu-box connect to gpu-box: i/o timeout')

    fireEvent.click(within(hosts).getByRole('button', { name: 'Reconnect' }))
    expect(state.reconnect).toHaveBeenCalledWith('gpu-box')
  })

  it('offers no reconnect for the panemux host itself', () => {
    renderDashboard(tasksState({ data: { hosts: [{ name: '', status: 'error', error: 'sh: not found' }], tasks: [] } }))
    expect(screen.queryByRole('button', { name: 'Reconnect' })).not.toBeInTheDocument()
  })

  it('says when it was last updated and refreshes on request', () => {
    const state = tasksState()
    const { onShowWorkspaces } = renderDashboard(state)
    expect(screen.getByText(/Updated just now · every 10s while shown/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(state.refresh).toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Workspaces' }))
    expect(onShowWorkspaces).toHaveBeenCalled()
  })

  it('reports a failed update without dropping the board', () => {
    renderDashboard(tasksState({ error: 'HTTP 500' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Failed to update tasks: HTTP 500')
    expect(screen.getByTestId('task-card-busy-1')).toBeInTheDocument()
  })

  it('opens a new pane, goes to an existing one, and offers nothing when it cannot', () => {
    const { onOpenTask } = renderDashboard()
    fireEvent.click(within(screen.getByTestId('task-card-wait-1')).getByRole('button', { name: 'Open: panemux' }))
    expect(onOpenTask).toHaveBeenCalledWith(expect.objectContaining({ id: 'wait-1' }), { kind: 'open' })

    const busy = screen.getByTestId('task-card-busy-1')
    expect(busy).toHaveTextContent('pane infra · Remote')
    fireEvent.click(within(busy).getByRole('button', { name: 'Go to pane: payment' }))
    expect(onOpenTask).toHaveBeenLastCalledWith(
      expect.objectContaining({ id: 'busy-1' }),
      { kind: 'goto', pane: { paneId: 'p-infra', paneTitle: 'infra', workspaceId: 'remote', workspaceTitle: 'Remote' } },
    )

    const idle = screen.getByTestId('task-card-idle-1')
    expect(idle).toHaveTextContent('outside tmux · cannot open in a pane')
    expect(within(idle).queryByRole('button', { name: /Open|Go to/ })).not.toBeInTheDocument()
  })

  it('links the branch and pull request of a task', () => {
    renderDashboard()
    const busy = screen.getByTestId('task-card-busy-1')
    expect(busy).toHaveTextContent('payment ⎇ PAY-418-retry')
    const link = within(busy).getByRole('link', { name: 'PR #87' })
    expect(link).toHaveAttribute('href', 'https://github.com/example-org/payment/pull/87')
    expect(link).toHaveAttribute('target', '_blank')
  })

  it('shows the selected task in the detail panel', () => {
    renderDashboard()
    const detail = screen.getByRole('complementary', { name: 'Task details' })
    expect(detail).toHaveTextContent('Select a task to see its details.')

    fireEvent.click(screen.getByRole('button', { name: 'panemux' }))
    expect(detail).toHaveTextContent('Waiting for input: input needed · for 3m')
    expect(within(detail).getByRole('heading', { level: 2 })).toHaveTextContent('panemux')
    expect(detail).toHaveTextContent('none yet (Open creates one)')
    expect(within(detail).getByRole('button', { name: 'Open: panemux' })).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('task-card-stop-1'))
    expect(detail).toHaveTextContent('No running process handles this session.')
    expect(detail).toHaveTextContent('Cannot open in a pane: not running.')

    fireEvent.click(screen.getByTestId('task-card-busy-1'))
    expect(within(detail).getByRole('link', { name: '#87' })).toBeInTheDocument()
    expect(detail).toHaveTextContent('Workspace')

    fireEvent.click(screen.getByRole('button', { name: 'Close task details' }))
    expect(detail).toHaveAttribute('data-open', 'false')
  })

  it('does not select a card when its link or open button is used', () => {
    renderDashboard()
    fireEvent.click(within(screen.getByTestId('task-card-busy-1')).getByRole('link', { name: 'PR #87' }))
    expect(screen.getByTestId('task-card-busy-1')).toHaveAttribute('data-selected', 'false')
  })

  it('filters by text and host, and splits rows by host or repository', () => {
    renderDashboard()
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filter tasks' }), { target: { value: 'pay-418' } })
    expect(screen.getAllByTestId(/^task-card-/)).toHaveLength(1)

    fireEvent.change(screen.getByRole('searchbox', { name: 'Filter tasks' }), { target: { value: '' } })
    fireEvent.change(screen.getByRole('combobox', { name: 'Show host' }), { target: { value: 'dev-server' } })
    expect(screen.getAllByTestId(/^task-card-/).map((el) => el.dataset.testid)).toEqual(['task-card-busy-1'])

    fireEvent.change(screen.getByRole('combobox', { name: 'Show host' }), { target: { value: '' } })
    expect(screen.getAllByTestId(/^task-card-/)).toHaveLength(4)

    fireEvent.change(screen.getByRole('combobox', { name: 'Split rows by' }), { target: { value: 'repo' } })
    expect(screen.getAllByRole('heading', { level: 3, name: 'Not in a Git repository' }).length).toBeGreaterThan(0)
  })

  it('explains the codex and unreadable states', () => {
    renderDashboard(tasksState({
      data: {
        hosts: [{ name: '', status: 'ok' }],
        tasks: [
          task({ id: 'codex', state: 'run', agent: 'codex', cwd: '/workspace/user/api' }),
          task({ id: 'unreadable', state: 'unknown', session_id: undefined, cwd: undefined, location: { kind: 'none', attachable: false } }),
        ],
      },
    }))
    const detail = screen.getByRole('complementary', { name: 'Task details' })
    fireEvent.click(screen.getByTestId('task-card-codex'))
    expect(detail).toHaveTextContent('codex reports no detailed state')
    fireEvent.click(screen.getByTestId('task-card-unreadable'))
    expect(detail).toHaveTextContent('could not be read or has an unexpected format')
  })

  // efficacy:exempt unchanged by this branch; the new describe block after it falls inside its line range
  it('says it is loading before the first response', () => {
    renderDashboard(tasksState({ data: null, loading: true, updatedAt: null }))
    expect(screen.getByText('Loading…')).toBeInTheDocument()
    expect(screen.getAllByText('None', { selector: '.td-empty' })).toHaveLength(5)
  })
})

describe('TaskDashboard done and labels', () => {
  const recorded: TasksResponse = {
    hosts: [{ name: '', status: 'ok' }, { name: 'dev-server', status: 'ok' }],
    tasks: [
      task({ id: 'busy-done', state: 'busy', cwd: '/workspace/user/alpha', done: true, labels: ['payment', 'sprint-42'] }),
      task({ id: 'stop-done', state: 'stop', cwd: '/workspace/user/beta', done: true,
        location: { kind: 'none', attachable: false } }),
      task({ id: 'stop-open', state: 'stop', host: 'dev-server', cwd: '/remote/home/demo/gamma', labels: ['sprint-42'],
        location: { kind: 'none', attachable: false } }),
      task({ id: 'codex', state: 'run', agent: 'codex', session_id: undefined, cwd: '/workspace/user/delta' }),
      task({ id: 'codex-with-session', state: 'idle', session_id: 'bbbbbbbb-0000', cwd: '/workspace/user/epsilon' }),
    ],
  }
  const detail = () => screen.getByRole('complementary', { name: 'Task details' })
  const cardIds = () => screen.queryAllByTestId(/^task-card-/).map((el) => el.dataset.testid?.replace('task-card-', ''))

  it('hides the Done column and the tasks in it until asked to show them', () => {
    renderDashboard(tasksState({ data: recorded }))
    expect(screen.queryByRole('region', { name: 'Done' })).not.toBeInTheDocument()
    expect(cardIds()).toEqual(['busy-done', 'codex-with-session', 'codex', 'stop-open'])

    fireEvent.click(screen.getByRole('checkbox', { name: 'Show Done column' }))
    const done = screen.getByRole('region', { name: 'Done' })
    expect(within(done).getByTestId('task-card-stop-done')).toBeInTheDocument()
    expect(within(done).getByRole('heading', { level: 2 })).toHaveTextContent('Done1')
    expect(within(screen.getByRole('region', { name: 'Stopped' })).queryByTestId('task-card-stop-done')).toBeNull()
  })

  it('keeps a running task marked done in its state column, marked as done', () => {
    renderDashboard(tasksState({ data: recorded }))
    const card = within(screen.getByRole('region', { name: 'Working' })).getByTestId('task-card-busy-done')
    expect(within(card).getByText('Done')).toBeInTheDocument()
  })

  it('shows labels on the card', () => {
    renderDashboard(tasksState({ data: recorded }))
    const labels = within(screen.getByTestId('task-card-busy-done')).getByRole('list', { name: 'Labels' })
    expect(within(labels).getAllByRole('listitem').map((el) => el.textContent)).toEqual(['payment', 'sprint-42'])
    expect(within(screen.getByTestId('task-card-codex')).queryByRole('list', { name: 'Labels' })).toBeNull()
  })

  it('filters by label and splits rows by label, a task with two labels in each', () => {
    renderDashboard(tasksState({ data: recorded }))
    const labelFilter = screen.getByRole('combobox', { name: 'Show label' })
    expect(within(labelFilter).getAllByRole('option').map((el) => el.textContent)).toEqual(['All', 'payment', 'sprint-42'])

    fireEvent.change(labelFilter, { target: { value: 'sprint-42' } })
    expect(cardIds()).toEqual(['busy-done', 'stop-open'])
    fireEvent.change(labelFilter, { target: { value: 'payment' } })
    expect(cardIds()).toEqual(['busy-done'])

    fireEvent.change(labelFilter, { target: { value: '\u0000all' } })
    fireEvent.change(screen.getByRole('combobox', { name: 'Split rows by' }), { target: { value: 'label' } })
    const working = screen.getByRole('region', { name: 'Working' })
    expect(within(working).getAllByRole('heading', { level: 3 }).map((el) => el.textContent)).toEqual(['payment', 'sprint-42'])
    expect(within(working).getAllByTestId('task-card-busy-done')).toHaveLength(2)
    expect(within(screen.getByRole('region', { name: 'Running / unknown' })).getByRole('heading', { level: 3 }))
      .toHaveTextContent('No label')
  })

  it('marks a task done only after it is confirmed', async () => {
    const saveRecord = vi.fn().mockResolvedValue(null)
    renderDashboard(tasksState({ data: recorded, saveRecord }))
    fireEvent.click(screen.getByTestId('task-card-stop-open'))

    fireEvent.click(within(detail()).getByRole('button', { name: 'Mark done' }))
    expect(saveRecord).not.toHaveBeenCalled()
    expect(detail()).toHaveTextContent(
      "Mark this task done? It moves to the Done column, which is hidden until 'Done column' is checked.",
    )
    fireEvent.click(within(detail()).getByRole('button', { name: 'Cancel' }))
    expect(detail()).not.toHaveTextContent('Mark this task done?')
    expect(saveRecord).not.toHaveBeenCalled()

    fireEvent.click(within(detail()).getByRole('button', { name: 'Mark done' }))
    await act(async () => {
      fireEvent.click(within(detail()).getByRole('button', { name: 'Confirm: mark done' }))
    })
    expect(saveRecord).toHaveBeenCalledWith(expect.objectContaining({ id: 'stop-open' }), { done: true, labels: ['sprint-42'] })
    expect(detail()).not.toHaveTextContent('Mark this task done?')
  })

  it('marks a done task not done without asking', async () => {
    const saveRecord = vi.fn().mockResolvedValue(null)
    renderDashboard(tasksState({ data: recorded, saveRecord }))
    fireEvent.click(screen.getByTestId('task-card-busy-done'))
    expect(within(detail()).queryByRole('button', { name: 'Mark done' })).toBeNull()

    await act(async () => {
      fireEvent.click(within(detail()).getByRole('button', { name: 'Mark not done' }))
    })
    expect(saveRecord).toHaveBeenCalledWith(expect.objectContaining({ id: 'busy-done' }),
      { done: false, labels: ['payment', 'sprint-42'] })
  })

  it('adds and removes labels in the detail panel', async () => {
    const saveRecord = vi.fn().mockResolvedValue(null)
    renderDashboard(tasksState({ data: recorded, saveRecord }))
    fireEvent.click(screen.getByTestId('task-card-busy-done'))

    const input = within(detail()).getByRole('textbox', { name: 'Add a label' })
    const add = within(detail()).getByRole('button', { name: 'Add' })
    expect(add).toBeDisabled()
    fireEvent.change(input, { target: { value: '   ' } })
    expect(add).toBeDisabled()

    fireEvent.change(input, { target: { value: ' infra ' } })
    await act(async () => {
      fireEvent.click(add)
    })
    expect(saveRecord).toHaveBeenLastCalledWith(expect.objectContaining({ id: 'busy-done' }),
      { done: true, labels: ['payment', 'sprint-42', 'infra'] })
    expect(input).toHaveValue('')

    await act(async () => {
      fireEvent.click(within(detail()).getByRole('button', { name: 'Remove label payment' }))
    })
    expect(saveRecord).toHaveBeenLastCalledWith(expect.objectContaining({ id: 'busy-done' }),
      { done: true, labels: ['sprint-42'] })
  })

  it('shows why a save failed and keeps what was typed', async () => {
    const saveRecord = vi.fn().mockResolvedValue('invalid task record: label is longer than 32 characters')
    renderDashboard(tasksState({ data: recorded, saveRecord }))
    fireEvent.click(screen.getByTestId('task-card-stop-open'))

    const input = within(detail()).getByRole('textbox', { name: 'Add a label' })
    fireEvent.change(input, { target: { value: 'x'.repeat(40) } })
    await act(async () => {
      fireEvent.submit(input)
    })
    expect(within(detail()).getByRole('alert')).toHaveTextContent('Could not save: invalid task record: label is longer')
    expect(input).toHaveValue('x'.repeat(40))
  })

  it('offers neither done nor labels for a task without a session id', () => {
    renderDashboard(tasksState({ data: recorded }))
    fireEvent.click(screen.getByTestId('task-card-codex'))
    expect(within(detail()).queryByRole('button', { name: 'Mark done' })).toBeNull()
    expect(within(detail()).queryByRole('textbox', { name: 'Add a label' })).toBeNull()
    expect(detail()).toHaveTextContent('Only a task with a session ID can be marked done or labeled.')
  })

  it('reports a record file the server could not read', () => {
    renderDashboard(tasksState({ data: { ...recorded, records_error: 'parsing task record file: bad' } }))
    expect(screen.getByRole('alert')).toHaveTextContent('Done and labels could not be loaded: parsing task record file: bad')
  })

  it('says where a task goes when it is marked done', () => {
    renderDashboard(tasksState({ data: recorded }))
    const confirmText = (id: string) => {
      fireEvent.click(screen.getByTestId(`task-card-${id}`))
      fireEvent.click(within(detail()).getByRole('button', { name: 'Mark done' }))
      const text = detail().querySelector('.td-confirm span')?.textContent
      fireEvent.click(within(detail()).getByRole('button', { name: 'Cancel' }))
      return text
    }

    expect(confirmText('codex-with-session')).toBe(
      'Mark this task done? It stays in its column while it runs, and moves to Done when it stops.',
    )
    fireEvent.click(screen.getByRole('checkbox', { name: 'Show Done column' }))
    expect(confirmText('stop-open')).toBe('Mark this task done? It moves to the Done column.')
  })

  it('keeps a save that finishes after another task is selected to the task it was for', async () => {
    let finish: (value: string | null) => void = () => {}
    const saveRecord = vi.fn().mockImplementation(() => new Promise<string | null>((resolve) => { finish = resolve }))
    renderDashboard(tasksState({ data: recorded, saveRecord }))

    fireEvent.click(screen.getByTestId('task-card-stop-open'))
    fireEvent.change(within(detail()).getByRole('textbox', { name: 'Add a label' }), { target: { value: 'lbl' } })
    fireEvent.click(within(detail()).getByRole('button', { name: 'Add' }))

    fireEvent.click(screen.getByTestId('task-card-codex-with-session'))
    expect(within(detail()).getByRole('button', { name: 'Mark done' })).toBeEnabled()
    fireEvent.change(within(detail()).getByRole('textbox', { name: 'Add a label' }), { target: { value: 'typing' } })

    await act(async () => {
      finish('disk full')
    })
    expect(within(detail()).queryByRole('alert')).toBeNull()
    expect(within(detail()).getByRole('textbox', { name: 'Add a label' })).toHaveValue('typing')
  })

  it('splits rows by a label named like an inherited object property', () => {
    renderDashboard(tasksState({
      data: { ...recorded, tasks: [task({ id: 'proto', state: 'busy', labels: ['__proto__', 'constructor'] })] },
    }))
    fireEvent.change(screen.getByRole('combobox', { name: 'Split rows by' }), { target: { value: 'label' } })
    const working = screen.getByRole('region', { name: 'Working' })
    expect(within(working).getAllByRole('heading', { level: 3 }).map((el) => el.textContent)).toEqual([
      '__proto__',
      'constructor',
    ])
  })
})

describe('TaskDashboard new tasks and resume', () => {
  const NEW_ID = 'ssh:dev-server:claude:0f0e0d0c-0b0a-4908-8706-050403020100'
  const STOPPED_SID = '5d7e3a90-1b2c-4d3e-8f40-51627384a5b6'
  const stopped = task({
    id: 'local:claude:' + STOPPED_SID, session_id: STOPPED_SID, state: 'stop', cwd: '/workspace/user/old',
    location: { kind: 'none', attachable: false },
  })
  const base: TasksResponse = { hosts: response.hosts, tasks: [stopped, ...response.tasks] }
  const launchedTask = task({
    id: NEW_ID, host: 'dev-server', session_id: '0f0e0d0c-0b0a-4908-8706-050403020100', state: 'busy',
    cwd: '/remote/home/demo/new', location: { kind: 'tmux', tmux_session: 'task-0f0e0d0c', attachable: true },
  })
  const detail = () => screen.getByRole('complementary', { name: 'Task details' })

  function renderWithRerender(state: TasksState) {
    const view = (s: TasksState) => (
      <TaskDashboard tasksState={s} workspaces={workspaces} onOpenTask={vi.fn()} onShowWorkspaces={vi.fn()} now={() => NOW} />
    )
    const { rerender } = render(view(state))
    return (next: TasksState) => rerender(view(next))
  }

  async function startTask(labels = '') {
    fireEvent.click(screen.getByRole('button', { name: 'New task' }))
    const dialog = screen.getByRole('dialog', { name: 'New task' })
    fireEvent.change(within(dialog).getByLabelText('Host'), { target: { value: 'dev-server' } })
    fireEvent.change(within(dialog).getByLabelText('Working directory'), { target: { value: '/remote/home/demo/new' } })
    fireEvent.change(within(dialog).getByLabelText('Labels'), { target: { value: labels } })
    fireEvent.change(within(dialog).getByLabelText('First instruction'), { target: { value: 'go' } })
    await act(async () => {
      fireEvent.click(within(dialog).getByRole('button', { name: 'Start' }))
    })
  }

  it('starts a task, says it is waiting for it, and selects it once it is listed', async () => {
    const launch = vi.fn().mockResolvedValue({
      ok: true, launched: { id: NEW_ID, session_id: launchedTask.session_id, tmux_session: 'task-0f0e0d0c' },
    })
    const state = tasksState({ data: base, launch })
    const rerender = renderWithRerender(state)

    await startTask('infra')

    expect(launch).toHaveBeenCalledWith({ host: 'dev-server', cwd: '/remote/home/demo/new', prompt: 'go', labels: ['infra'] })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(screen.getByRole('status').textContent).toBe(
      'Started tmux task-0f0e0d0c on dev-server. It is selected here once claude has started.',
    )
    expect(screen.queryByRole('heading', { level: 2, name: 'new' })).toBeNull()

    rerender({ ...state, data: { ...base, tasks: [...base.tasks, launchedTask] } })

    expect(screen.getByTestId(`task-card-${NEW_ID}`).dataset.selected).toBe('true')
    expect(within(detail()).getByText('tmux task-0f0e0d0c')).toBeTruthy()
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('stops waiting for a started task once another is selected', async () => {
    const launch = vi.fn().mockResolvedValue({
      ok: true, launched: { id: NEW_ID, session_id: launchedTask.session_id, tmux_session: 'task-0f0e0d0c' },
    })
    const state = tasksState({ data: base, launch })
    const rerender = renderWithRerender(state)
    await startTask()

    fireEvent.click(screen.getByTestId('task-card-wait-1'))
    rerender({ ...state, data: { ...base, tasks: [...base.tasks, launchedTask] } })

    expect(screen.getByTestId('task-card-wait-1').dataset.selected).toBe('true')
    expect(screen.getByTestId(`task-card-${NEW_ID}`).dataset.selected).toBe('false')
  })

  it('reports labels that could not be saved for a task that did start', async () => {
    const launch = vi.fn().mockResolvedValue({
      ok: true,
      launched: { id: NEW_ID, session_id: launchedTask.session_id, tmux_session: 'task-0f0e0d0c', records_error: 'parse tasks.json' },
    })
    renderDashboard(tasksState({ data: base, launch }))

    await startTask('infra')

    expect(screen.getByRole('alert').textContent).toBe('The task started, but its labels could not be saved: parse tasks.json')
  })

  it('offers Resume only on a stopped claude task with a resumable session id', () => {
    renderDashboard(tasksState({ data: base }))

    expect(within(screen.getByTestId(`task-card-${stopped.id}`)).getByRole('button', { name: 'Resume: old' })).toBeTruthy()
    // stop-1's session id is not a UUID, and the others are running.
    for (const id of ['stop-1', 'wait-1', 'run-1']) {
      expect(within(screen.getByTestId(`task-card-${id}`)).queryByRole('button', { name: /^Resume/ })).toBeNull()
    }
    fireEvent.click(screen.getByTestId('task-card-stop-1'))
    expect(within(detail()).queryByRole('button', { name: /^Resume/ })).toBeNull()
  })

  it('resumes from the card and the detail panel, and selects the task', async () => {
    let finish: (value: unknown) => void = () => {}
    const resume = vi.fn().mockImplementation(() => new Promise((resolve) => { finish = resolve }))
    renderDashboard(tasksState({ data: base, resume }))

    fireEvent.click(within(screen.getByTestId(`task-card-${stopped.id}`)).getByRole('button', { name: /^Resume/ }))

    expect(resume).toHaveBeenCalledWith(stopped)
    expect(within(screen.getByTestId(`task-card-${stopped.id}`)).getByRole('button', { name: /^Resume/ })).toBeDisabled()
    await act(async () => {
      finish({ ok: true, launched: { id: stopped.id, session_id: STOPPED_SID, tmux_session: 'task-5d7e3a90' } })
    })
    expect(screen.getByTestId(`task-card-${stopped.id}`).dataset.selected).toBe('true')

    resume.mockResolvedValue({ ok: true, launched: { id: stopped.id, session_id: STOPPED_SID, tmux_session: 'task-5d7e3a90' } })
    await act(async () => {
      fireEvent.click(within(detail()).getByRole('button', { name: /^Resume/ }))
    })
    expect(resume).toHaveBeenCalledTimes(2)
  })

  it('shows why a resume was refused', async () => {
    const resume = vi.fn().mockResolvedValue({ ok: false, error: "a tmux session with the task's name already exists on the host" })
    renderDashboard(tasksState({ data: base, resume }))

    await act(async () => {
      fireEvent.click(within(screen.getByTestId(`task-card-${stopped.id}`)).getByRole('button', { name: /^Resume/ }))
    })

    expect(screen.getByRole('alert').textContent).toBe(
      "Could not resume old: a tmux session with the task's name already exists on the host",
    )
    expect(within(screen.getByTestId(`task-card-${stopped.id}`)).getByRole('button', { name: /^Resume/ })).toBeEnabled()
  })

  it('closes the New task dialog on Cancel without starting anything', () => {
    const launch = vi.fn()
    renderDashboard(tasksState({ data: base, launch }))
    fireEvent.click(screen.getByRole('button', { name: 'New task' }))
    fireEvent.click(within(screen.getByRole('dialog', { name: 'New task' })).getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(launch).not.toHaveBeenCalled()
  })
})
