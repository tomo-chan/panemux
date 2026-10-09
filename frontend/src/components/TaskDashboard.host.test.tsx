import { useEffect } from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { TaskDashboard } from './TaskDashboard'
import type { TasksState } from '../hooks/useTasks'
import type { SSHConnectionsState } from '../hooks/useSSHConnections'
import type { TasksResponse } from '../schemas'
import type { TaskTerminalStatus } from './TaskTerminal'

// Issue #314: a host chip's Type in pane popup. The terminal is a stub whose
// status the test drives, as in TaskDashboard.input.test.tsx.
const { terminals } = vi.hoisted(() => ({
  terminals: { mounts: [] as string[], onStatus: null as ((status: TaskTerminalStatus) => void) | null },
}))

vi.mock('./TaskTerminal', () => ({
  TaskTerminal: ({ sessionId, onStatus }: { sessionId: string; onStatus: (status: TaskTerminalStatus) => void }) => {
    terminals.onStatus = onStatus
    useEffect(() => {
      terminals.mounts.push(sessionId)
    }, [sessionId])
    return <div data-testid="task-terminal" data-session={sessionId} />
  },
}))

const NOW = Date.parse('2026-09-25T12:00:00Z')

const response: TasksResponse = {
  hosts: [
    { name: '', status: 'ok' },
    { name: 'dev-server', status: 'ok' },
  ],
  tasks: [],
}

function hostsState(): SSHConnectionsState {
  return {
    connections: [],
    sshConfigNames: [],
    loading: false,
    error: null,
    load: vi.fn().mockResolvedValue(undefined),
    create: vi.fn().mockResolvedValue(null),
    update: vi.fn().mockResolvedValue(null),
    remove: vi.fn().mockResolvedValue(null),
  }
}

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
    requestSummary: vi.fn().mockResolvedValue(null),
    attach: vi.fn().mockResolvedValue({ ok: false, error: 'not stubbed' }),
    detach: vi.fn().mockResolvedValue(undefined),
    hostSessionName: vi.fn().mockResolvedValue({ ok: true, launched: { tmux_session: 'dev-server-0123abcd' } }),
    hostTerminal: vi.fn().mockResolvedValue({ ok: true, launched: { session_id: 'board-0000000000000001', tmux_session: '' } }),
    closeHostTerminal: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

function renderDashboard(state: TasksState) {
  render(
    <TaskDashboard
      tasksState={state}
      hostsState={hostsState()}
      workspaces={[]}
      onOpenTask={vi.fn()}
      onOpenHost={vi.fn()}
      onShowWorkspaces={vi.fn()}
      now={() => NOW}
    />,
  )
  return screen.getByRole('button', { name: 'Open a terminal on dev-server' })
}

async function typeIn(chip: HTMLElement) {
  fireEvent.click(chip)
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Type in pane: dev-server' }))
  })
}

describe('TaskDashboard host Type in pane (issue #314)', () => {
  it('opens an ssh terminal in a popup, and closing it ends the terminal and returns to the chip', async () => {
    const state = tasksState()
    const chip = renderDashboard(state)

    await typeIn(chip)

    expect(state.hostTerminal).toHaveBeenCalledWith('dev-server', 'ssh', undefined)
    const popup = screen.getByRole('dialog', { name: 'Terminal on dev-server' })
    expect(popup).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByTestId('task-terminal')).toHaveAttribute('data-session', 'board-0000000000000001')
    expect(popup).toHaveTextContent('Closing logs out of dev-server')
    act(() => terminals.onStatus?.('connected'))
    expect(screen.getByTestId('task-input-status')).toHaveTextContent('Connected')

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog', { name: 'Terminal on dev-server' })).toBeNull()
    expect(state.closeHostTerminal).toHaveBeenCalledWith('board-0000000000000001')
    expect(chip).toHaveFocus()
  })

  it('opens an ssh_tmux terminal on the session the menu showed, and says it keeps running', async () => {
    const state = tasksState({
      hostTerminal: vi.fn().mockResolvedValue({
        ok: true,
        launched: { session_id: 'board-0000000000000002', tmux_session: 'dev-server-0123abcd' },
      }),
    })
    const chip = renderDashboard(state)

    fireEvent.click(chip)
    await act(async () => {
      fireEvent.click(screen.getByRole('radio', { name: /^ssh_tmux\b/ }))
    })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Type in pane: dev-server' }))
    })

    expect(state.hostTerminal).toHaveBeenCalledWith('dev-server', 'ssh_tmux', 'dev-server-0123abcd')
    const popup = screen.getByRole('dialog', { name: 'Terminal on dev-server' })
    expect(popup).toHaveTextContent('tmux: dev-server-0123abcd')
    expect(popup).toHaveTextContent('Closing leaves the tmux session running')
  })

  it('says why it could not open, and retries with a new terminal', async () => {
    const hostTerminal = vi.fn()
      .mockResolvedValueOnce({ ok: false, error: 'open a terminal on "dev-server": dial tcp: i/o timeout' })
      .mockResolvedValueOnce({ ok: true, launched: { session_id: 'board-0000000000000003', tmux_session: '' } })
    const chip = renderDashboard(tasksState({ hostTerminal }))

    await typeIn(chip)
    expect(screen.getByRole('alert')).toHaveTextContent('Could not connect: open a terminal on "dev-server": dial tcp: i/o timeout.')

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    })
    expect(hostTerminal).toHaveBeenCalledTimes(2)
    expect(screen.getByTestId('task-terminal')).toHaveAttribute('data-session', 'board-0000000000000003')
  })

  it('offers a new terminal once the shell has ended', async () => {
    const hostTerminal = vi.fn()
      .mockResolvedValueOnce({ ok: true, launched: { session_id: 'board-0000000000000004', tmux_session: '' } })
      .mockResolvedValueOnce({ ok: true, launched: { session_id: 'board-0000000000000005', tmux_session: '' } })
    const state = tasksState({ hostTerminal })
    const chip = renderDashboard(state)

    await typeIn(chip)
    act(() => terminals.onStatus?.('ended'))
    expect(screen.getByRole('alert')).toHaveTextContent('The connection ended.')

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Reconnect' }))
    })
    expect(state.closeHostTerminal).toHaveBeenCalledWith('board-0000000000000004')
    expect(screen.getByTestId('task-terminal')).toHaveAttribute('data-session', 'board-0000000000000005')
  })

  it('closes on Cmd/Ctrl+Shift+Escape from anywhere', async () => {
    const state = tasksState()
    const chip = renderDashboard(state)

    await typeIn(chip)
    fireEvent.keyDown(window, { key: 'Escape', shiftKey: true, ctrlKey: true })

    expect(screen.queryByRole('dialog', { name: 'Terminal on dev-server' })).toBeNull()
    expect(state.closeHostTerminal).toHaveBeenCalledWith('board-0000000000000001')
  })

  // The dashboard re-rendered as a poll that saw dev-server in status.
  function withDevServer(state: TasksState, status: 'ok' | 'connecting' | 'error'): TasksState {
    return {
      ...state,
      data: { ...response, hosts: [{ name: '', status: 'ok' }, { name: 'dev-server', status, error: status === 'error' ? 'refused' : undefined }] },
    }
  }

  function dashboard(state: TasksState) {
    return (
      <TaskDashboard
        tasksState={state}
        hostsState={hostsState()}
        workspaces={[]}
        onOpenTask={vi.fn()}
        onOpenHost={vi.fn()}
        onShowWorkspaces={vi.fn()}
        now={() => NOW}
      />
    )
  }

  it('drops the connection menu when its host stops being reachable, and does not reopen it later', () => {
    const state = tasksState()
    const { rerender } = render(dashboard(state))
    fireEvent.click(screen.getByRole('button', { name: 'Open a terminal on dev-server' }))
    expect(screen.getByRole('button', { name: 'Type in pane: dev-server' })).toHaveFocus()

    rerender(dashboard(withDevServer(state, 'error')))
    expect(screen.queryByRole('button', { name: 'Type in pane: dev-server' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Reconnect' })).toHaveFocus()

    rerender(dashboard(withDevServer(state, 'ok')))
    expect(screen.queryByRole('button', { name: 'Type in pane: dev-server' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Open a terminal on dev-server' })).toHaveAttribute('aria-expanded', 'false')
  })

  it('moves focus to the dashboard when the menu goes with a host that is connecting', () => {
    const state = tasksState()
    const { rerender, container } = render(dashboard(state))
    fireEvent.click(screen.getByRole('button', { name: 'Open a terminal on dev-server' }))

    rerender(dashboard(withDevServer(state, 'connecting')))
    expect(screen.queryByRole('button', { name: 'Type in pane: dev-server' })).toBeNull()
    expect(container.querySelector('.td-root')).toHaveFocus()
  })

  it('returns focus to the dashboard when the chip went away while the terminal was open', async () => {
    const state = tasksState()
    const { rerender, container } = render(dashboard(state))
    fireEvent.click(screen.getByRole('button', { name: 'Open a terminal on dev-server' }))
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Type in pane: dev-server' }))
    })

    rerender(dashboard(withDevServer(state, 'connecting')))
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog', { name: 'Terminal on dev-server' })).toBeNull()
    expect(container.querySelector('.td-root')).toHaveFocus()
  })

  it('returns focus to the host chip as it is now when the old one was replaced', async () => {
    const state = tasksState()
    const { rerender } = render(dashboard(state))
    fireEvent.click(screen.getByRole('button', { name: 'Open a terminal on dev-server' }))
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Type in pane: dev-server' }))
    })

    rerender(dashboard(withDevServer(state, 'error')))
    rerender(dashboard(withDevServer(state, 'ok')))
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.getByRole('button', { name: 'Open a terminal on dev-server' })).toHaveFocus()
  })
})
