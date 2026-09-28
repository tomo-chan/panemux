import { fireEvent, render, screen, within } from '@testing-library/react'
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
      git: {
        repo: 'payment', branch: 'PAY-418-retry', pr_number: 87, pr_url: 'https://github.com/example-org/payment/pull/87',
        issues: [
          { number: 252, url: 'https://github.com/example-org/payment/issues/252', repo: 'example-org/payment' },
          { number: 9, url: 'https://github.com/example-org/infra/issues/9', repo: 'example-org/infra' },
        ],
        autolinks: [
          { text: 'PAY-418', url: 'https://jira.example.com/browse/PAY-418' },
          { text: 'OPS-77', url: 'https://jira.example.com/browse/OPS-77' },
        ],
      },
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

  it('links the issues the pull request closes and the references of a task in a new tab', () => {
    renderDashboard()
    const busy = screen.getByTestId('task-card-busy-1')
    const expected: [string, string][] = [
      ['Issue #252', 'https://github.com/example-org/payment/issues/252'],
      ['Issue example-org/infra#9', 'https://github.com/example-org/infra/issues/9'],
      ['PAY-418', 'https://jira.example.com/browse/PAY-418'],
      ['OPS-77', 'https://jira.example.com/browse/OPS-77'],
    ]
    for (const [name, href] of expected) {
      const link = within(busy).getByRole('link', { name })
      expect(link).toHaveAttribute('href', href)
      expect(link).toHaveAttribute('target', '_blank')
      expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    }

    fireEvent.click(busy)
    const detail = screen.getByRole('complementary', { name: 'Task details' })
    expect(within(detail).getByRole('link', { name: '#252' })).toHaveAttribute(
      'href', 'https://github.com/example-org/payment/issues/252',
    )
    expect(within(detail).getByRole('link', { name: 'example-org/infra#9' })).toHaveAttribute('target', '_blank')
    expect(detail).toHaveTextContent('closed by the pull request')
    expect(detail).toHaveTextContent('References')
    expect(within(detail).getByRole('link', { name: 'PAY-418' })).toHaveAttribute(
      'href', 'https://jira.example.com/browse/PAY-418',
    )
    expect(within(detail).getByRole('link', { name: 'OPS-77' })).toHaveAttribute('target', '_blank')
    expect(detail).toHaveTextContent('from the branch name or pull request title')

    // A task with neither shows no row for them.
    expect(within(screen.getByTestId('task-card-wait-1')).queryByRole('link', { name: /^(Issue |PAY-|OPS-)/ }))
      .not.toBeInTheDocument()
    fireEvent.click(screen.getByTestId('task-card-stop-1'))
    expect(detail).not.toHaveTextContent('closed by the pull request')
    expect(detail).not.toHaveTextContent('from the branch name')
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

  it('says it is loading before the first response', () => {
    renderDashboard(tasksState({ data: null, loading: true, updatedAt: null }))
    expect(screen.getByText('Loading…')).toBeInTheDocument()
    expect(screen.getAllByText('None', { selector: '.td-empty' })).toHaveLength(5)
  })
})
