import React, { useEffect } from 'react'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { TaskDashboard } from './TaskDashboard'
import type { TasksState, TaskActionResult } from '../hooks/useTasks'
import type { SSHConnectionsState } from '../hooks/useSSHConnections'
import type { Task, TaskAttach, TasksResponse, Workspace } from '../schemas'
import type { TaskTerminalStatus } from './TaskTerminal'

// Issue #284: the dashboard's Type in pane popup. The terminal itself is
// TaskTerminal's (tested on its own); here it is a stub whose status the test
// drives, and whose mounts are counted so a resize or a poll that remounted it
// — a new WebSocket, a lost input target — would show.
const { terminals } = vi.hoisted(() => ({
  terminals: { mounts: [] as string[], onStatus: null as ((status: TaskTerminalStatus) => void) | null },
}))

vi.mock('./TaskTerminal', () => ({
  TaskTerminal: ({ sessionId, onStatus }: { sessionId: string; onStatus: (status: TaskTerminalStatus) => void }) => {
    terminals.onStatus = onStatus
    useEffect(() => {
      terminals.mounts.push(sessionId)
    }, [sessionId])
    return (
      <div data-testid="task-terminal" data-session={sessionId}>
        <textarea className="xterm-helper-textarea" aria-label="Terminal input" />
      </div>
    )
  },
}))

const NOW = Date.parse('2026-09-25T12:00:00Z')

function task(overrides: Partial<Task>): Task {
  return {
    id: 'local:claude:a',
    host: '',
    agent: 'claude',
    session_id: 'aaaaaaaa-0000',
    cwd: '/workspace/user/panemux',
    state: 'wait',
    status_since: '2026-09-25T11:57:00Z',
    location: { kind: 'tmux', tmux_session: 'task-a', attachable: true },
    ...overrides,
  }
}

const waiting = task({ id: 'wait-1', state: 'wait', waiting_for: 'approve go test', cwd: '/workspace/user/review-api',
  location: { kind: 'tmux', tmux_session: 'review-api', attachable: true } })
const idle = task({ id: 'idle-1', state: 'idle', host: 'dev-server', cwd: '/remote/home/demo/docs',
  location: { kind: 'tmux', tmux_session: 'docs', attachable: true } })
const busy = task({ id: 'busy-1', state: 'busy', cwd: '/workspace/user/search',
  location: { kind: 'tmux', tmux_session: 'search', attachable: true } })
const outside = task({ id: 'idle-out', state: 'idle', cwd: '/workspace/user/notes', location: { kind: 'outside', attachable: false } })
const daemon = task({ id: 'wait-daemon', state: 'wait', agent: 'codex', cwd: '/workspace/user/codex',
  location: { kind: 'daemon', attachable: false } })
const withPane = task({ id: 'wait-pane', state: 'wait', cwd: '/workspace/user/infra',
  location: { kind: 'tmux', tmux_session: 'infra', attachable: true } })

function response(tasks: Task[]): TasksResponse {
  return { hosts: [{ name: '', status: 'ok' }, { name: 'dev-server', status: 'ok' }], tasks }
}

const ALL = [waiting, idle, busy, outside, daemon, withPane]

const workspaces: Workspace[] = [
  {
    id: 'main',
    title: 'Main',
    layout: {
      direction: 'horizontal',
      children: [{ size: 100, pane: { id: 'p-infra', type: 'tmux', tmux_session: 'infra', title: 'infra' } }],
    },
  },
]

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

type AttachResult = TaskActionResult<TaskAttach>

function deferred() {
  let resolve!: (value: AttachResult) => void
  const promise = new Promise<AttachResult>((r) => { resolve = r })
  return { promise, resolve }
}

function attached(sessionId: string, tmux = 'review-api'): AttachResult {
  return { ok: true, launched: { session_id: sessionId, tmux_session: tmux } }
}

function tasksState(overrides: Partial<TasksState> = {}, tasks: Task[] = ALL): TasksState {
  return {
    data: response(tasks),
    error: null,
    loading: false,
    updatedAt: NOW - 8000,
    refresh: vi.fn().mockResolvedValue(undefined),
    reconnect: vi.fn().mockResolvedValue(undefined),
    saveRecord: vi.fn().mockResolvedValue(null),
    launch: vi.fn().mockResolvedValue({ ok: false, error: 'not stubbed' }),
    resume: vi.fn().mockResolvedValue({ ok: false, error: 'not stubbed' }),
    requestSummary: vi.fn().mockResolvedValue(null),
    attach: vi.fn().mockResolvedValue(attached('board-0000000000000001')),
    detach: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

function view(state: TasksState, onOpenTask = vi.fn()) {
  return (
    <TaskDashboard
      tasksState={state}
      hostsState={hostsState()}
      workspaces={workspaces}
      onOpenTask={onOpenTask}
      onShowWorkspaces={vi.fn()}
      now={() => NOW}
    />
  )
}

function renderDashboard(state: TasksState = tasksState(), onOpenTask = vi.fn()) {
  const utils = render(view(state, onOpenTask))
  return { ...utils, state, onOpenTask, rerenderWith: (next: TasksState) => utils.rerender(view(next, onOpenTask)) }
}

const card = (id: string) => screen.getByTestId(`task-card-${id}`)
const typeIn = (id: string) => within(card(id)).getByRole('button', { name: /^Type in pane: / })
const dialog = () => screen.getByRole('dialog')
const chip = () => within(dialog()).getByTestId('task-input-status')

async function openPopup(id = 'wait-1') {
  await act(async () => {
    fireEvent.click(typeIn(id))
  })
}

function setStatus(status: TaskTerminalStatus) {
  act(() => terminals.onStatus?.(status))
}

function key(target: Element | Window, init: KeyboardEventInit) {
  const event = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init })
  act(() => {
    target.dispatchEvent(event)
  })
  return event
}

function setNarrow(narrow: boolean) {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: (query: string) => ({
      matches: narrow,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }),
  })
}

beforeEach(() => {
  terminals.mounts = []
  terminals.onStatus = null
  window.localStorage.clear()
  setNarrow(false)
})

afterEach(() => {
  window.localStorage.clear()
})

describe('TaskDashboard Type in pane: entry', () => {
  it('offers Type in pane on waiting and idle cards that can be attached, and on the detail panel', () => {
    renderDashboard()
    expect(typeIn('wait-1')).toHaveTextContent('Type in pane')
    expect(typeIn('idle-1')).toBeInTheDocument()
    expect(within(card('busy-1')).queryByRole('button', { name: /^Type in pane: / })).toBeNull()

    fireEvent.click(within(card('wait-1')).getByRole('button', { name: 'review-api' }))
    const detail = screen.getByRole('complementary', { name: 'Task details' })
    expect(within(detail).getByRole('button', { name: /^Type in pane: / })).toBeInTheDocument()
    // The existing way to the workspace stays a button of its own.
    expect(within(detail).getByRole('button', { name: /^Open: / })).toBeInTheDocument()
  })

  it('shows why a waiting or idle task cannot be typed into, instead of the button', () => {
    renderDashboard()
    expect(within(card('idle-out')).queryByRole('button', { name: /^Type in pane: / })).toBeNull()
    expect(within(card('wait-daemon')).queryByRole('button', { name: /^Type in pane: / })).toBeNull()

    fireEvent.click(within(card('wait-daemon')).getByRole('button', { name: 'codex' }))
    const detail = screen.getByRole('complementary', { name: 'Task details' })
    expect(detail).toHaveTextContent("Type in pane is not available: run by codex's shared daemon")
  })
})

describe('TaskDashboard Type in pane: opening', () => {
  it('opens one attach per press: the button shows Connecting… and is disabled until it settles', async () => {
    const pending = deferred()
    const state = tasksState({ attach: vi.fn().mockReturnValue(pending.promise) })
    renderDashboard(state)

    await openPopup()
    expect(state.attach).toHaveBeenCalledTimes(1)
    expect(state.attach).toHaveBeenCalledWith(waiting)
    const button = within(card('wait-1')).getByRole('button', { name: /^Type in pane: / })
    expect(button).toHaveTextContent('Connecting…')
    expect(button).toBeDisabled()
    fireEvent.click(button)
    expect(state.attach).toHaveBeenCalledTimes(1)

    expect(dialog()).toHaveAccessibleName('Type in pane: review-api')
    expect(chip()).toHaveTextContent('Connecting')
    expect(screen.queryByTestId('task-terminal')).toBeNull()

    await act(async () => pending.resolve(attached('board-0000000000000001')))
    expect(screen.getByTestId('task-terminal')).toHaveAttribute('data-session', 'board-0000000000000001')
    setStatus('connecting')
    expect(chip()).toHaveTextContent('Connecting')
    setStatus('connected')
    expect(chip()).toHaveTextContent('Connected')
  })

  it('shows the task, its state, host, agent and the tmux session input goes to', async () => {
    renderDashboard(tasksState({ attach: vi.fn().mockResolvedValue(attached('board-0000000000000001', 'docs')) }))
    await openPopup('idle-1')
    const header = within(dialog()).getByTestId('task-input-header')
    expect(header).toHaveTextContent('docs')
    expect(header).toHaveTextContent('Idle')
    expect(header).toHaveTextContent('dev-server · claude · tmux: docs')
  })

  it('discards an attach that answers after the popup was closed and another task opened', async () => {
    const first = deferred()
    const second = deferred()
    const state = tasksState({ attach: vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise) })
    renderDashboard(state)

    await openPopup('wait-1')
    fireEvent.click(within(dialog()).getByRole('button', { name: 'Close' }))
    await openPopup('idle-1')

    await act(async () => first.resolve(attached('board-000000000000000a')))
    expect(screen.queryByTestId('task-terminal')).toBeNull()
    expect(state.detach).toHaveBeenCalledWith('board-000000000000000a')
    expect(dialog()).toHaveAccessibleName('Type in pane: docs')

    await act(async () => second.resolve(attached('board-000000000000000b', 'docs')))
    expect(screen.getByTestId('task-terminal')).toHaveAttribute('data-session', 'board-000000000000000b')
    expect(state.detach).not.toHaveBeenCalledWith('board-000000000000000b')
  })

  it('keeps the attach a reopened popup for the same task shares with the closed one', async () => {
    const first = deferred()
    const second = deferred()
    const state = tasksState({ attach: vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise) })
    renderDashboard(state)

    await openPopup('wait-1')
    fireEvent.click(within(dialog()).getByRole('button', { name: 'Close' }))
    await openPopup('wait-1')

    // The server answers both requests with the same attach.
    await act(async () => first.resolve(attached('board-00000000000000aa')))
    expect(state.detach).not.toHaveBeenCalled()
    await act(async () => second.resolve(attached('board-00000000000000aa')))
    expect(screen.getByTestId('task-terminal')).toHaveAttribute('data-session', 'board-00000000000000aa')
    expect(state.detach).not.toHaveBeenCalled()
  })

  it('ends a shared attach when the reopened popup closes before either answer', async () => {
    const first = deferred()
    const second = deferred()
    const state = tasksState({ attach: vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise) })
    renderDashboard(state)

    await openPopup('wait-1')
    fireEvent.click(within(dialog()).getByRole('button', { name: 'Close' }))
    await openPopup('wait-1')
    await act(async () => first.resolve(attached('board-00000000000000aa')))
    fireEvent.click(within(dialog()).getByRole('button', { name: 'Close' }))
    await act(async () => second.resolve(attached('board-00000000000000aa')))

    expect(screen.queryByRole('dialog')).toBeNull()
    expect(state.detach).toHaveBeenCalledWith('board-00000000000000aa')
  })
})

describe('TaskDashboard Type in pane: closing', () => {
  it('ends only the attach, keeps the selection and filter, and returns focus to the button it was opened from', async () => {
    const { state } = renderDashboard()
    fireEvent.click(within(card('wait-1')).getByRole('button', { name: 'review-api' }))
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filter tasks' }), { target: { value: 'workspace' } })

    await openPopup()
    setStatus('connected')
    fireEvent.click(within(dialog()).getByRole('button', { name: 'Close' }))

    expect(screen.queryByRole('dialog')).toBeNull()
    expect(state.detach).toHaveBeenCalledWith('board-0000000000000001')
    expect(screen.getByRole('searchbox', { name: 'Filter tasks' })).toHaveValue('workspace')
    expect(within(card('wait-1')).getByRole('button', { name: 'review-api' })).toHaveAttribute('aria-pressed', 'true')
    expect(document.activeElement).toBe(typeIn('wait-1'))
  })

  it('returns focus to the detail panel button when opened from there', async () => {
    renderDashboard()
    fireEvent.click(within(card('wait-1')).getByRole('button', { name: 'review-api' }))
    const detail = screen.getByRole('complementary', { name: 'Task details' })
    await act(async () => {
      fireEvent.click(within(detail).getByRole('button', { name: /^Type in pane: / }))
    })
    fireEvent.click(within(dialog()).getByRole('button', { name: 'Close' }))
    expect(document.activeElement).toBe(within(detail).getByRole('button', { name: /^Type in pane: / }))
  })

  it('ends the attach when the dashboard goes away with the popup open', async () => {
    const { state, unmount } = renderDashboard()
    await openPopup()
    unmount()
    expect(state.detach).toHaveBeenCalledWith('board-0000000000000001')
  })

  it('ends the attach when the page is left with the popup open', async () => {
    const { state } = renderDashboard()
    await openPopup()
    act(() => {
      window.dispatchEvent(new Event('pagehide'))
    })
    expect(state.detach).toHaveBeenCalledWith('board-0000000000000001')
  })
})

describe('TaskDashboard Type in pane: keyboard', () => {
  it('sends Escape to the terminal while it has focus', async () => {
    renderDashboard()
    await openPopup()
    setStatus('connected')
    const input = screen.getByLabelText('Terminal input')
    input.focus()
    const event = key(input, { key: 'Escape' })
    expect(event.defaultPrevented).toBe(false)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('closes on Escape when focus is in the header', async () => {
    const { state } = renderDashboard()
    await openPopup()
    const close = within(dialog()).getByRole('button', { name: 'Close' })
    close.focus()
    key(close, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(state.detach).toHaveBeenCalled()
  })

  it('does not close on Escape while focus is on the popup itself or below the header', async () => {
    renderDashboard(tasksState({ attach: vi.fn().mockResolvedValue({ ok: false, error: 'the tmux session has ended' }) }))
    await openPopup()
    // Focus starts on the popup itself, before any terminal can take it.
    expect(document.activeElement).toBe(dialog())
    key(dialog(), { key: 'Escape' })
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    const retry = within(dialog()).getByRole('button', { name: 'Retry' })
    retry.focus()
    key(retry, { key: 'Escape' })
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('does not close on Escape after a disconnect moves focus to the popup', async () => {
    renderDashboard()
    await openPopup()
    setStatus('connected')
    screen.getByLabelText('Terminal input').focus()
    setStatus('disconnected')
    // A browser drops focus from the now inert terminal and the popup takes
    // it; jsdom does not, so put it where the browser would.
    dialog().focus()
    key(dialog(), { key: 'Escape' })
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('closes on Cmd/Ctrl+Shift+Escape even from the terminal, without sending it there', async () => {
    renderDashboard()
    await openPopup()
    setStatus('connected')
    const input = screen.getByLabelText('Terminal input')
    input.focus()
    const reached = vi.fn()
    input.addEventListener('keydown', reached)
    key(input, { key: 'Escape', ctrlKey: true, shiftKey: true })
    expect(reached).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('keeps Tab inside the popup from the header, and leaves Tab to the terminal', async () => {
    renderDashboard()
    await openPopup()
    setStatus('connected')
    const buttons = within(dialog()).getAllByRole('button')
    const first = buttons[0]
    first.focus()
    const back = key(first, { key: 'Tab', shiftKey: true })
    expect(back.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(screen.getByLabelText('Terminal input'))

    const outside = document.body
    ;(document.activeElement as HTMLElement).blur()
    const pulled = key(outside, { key: 'Tab' })
    expect(pulled.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(first)

    const input = screen.getByLabelText('Terminal input')
    input.focus()
    expect(key(input, { key: 'Tab' }).defaultPrevented).toBe(false)
  })

  it('makes everything behind the popup inert while it is open', async () => {
    renderDashboard()
    await openPopup()
    expect(screen.getByRole('button', { name: 'Refresh', hidden: true }).closest('[inert]')).not.toBeNull()
    expect(card('busy-1').closest('[inert]')).not.toBeNull()
    expect(dialog().closest('[inert]')).toBeNull()

    fireEvent.click(within(dialog()).getByRole('button', { name: 'Close' }))
    expect(screen.getByRole('button', { name: 'Refresh' }).closest('[inert]')).toBeNull()
  })
})

describe('TaskDashboard Type in pane: while open', () => {
  it('stays on the same task and terminal when a poll moves it, and says so', async () => {
    const state = tasksState()
    const { rerenderWith } = renderDashboard(state)
    await openPopup()
    setStatus('connected')

    rerenderWith(tasksState({ attach: state.attach, detach: state.detach }, ALL.map((t) => (t.id === 'wait-1' ? { ...t, state: 'busy' } : t))))

    expect(within(dialog()).getByRole('status')).toHaveTextContent('Moved to Working on the board. This terminal stays on review-api.')
    expect(terminals.mounts).toEqual(['board-0000000000000001'])
    expect(chip()).toHaveTextContent('Connected')
    const moved = screen.getByTestId('task-card-wait-1')
    expect(moved.closest('[data-column="busy"]')).not.toBeNull()
    expect(moved).toHaveTextContent('Typing')
  })

  it('says when the task is no longer listed, and keeps the terminal', async () => {
    const state = tasksState()
    const { rerenderWith } = renderDashboard(state)
    await openPopup()
    setStatus('connected')
    rerenderWith(tasksState({ attach: state.attach, detach: state.detach }, ALL.filter((t) => t.id !== 'wait-1')))
    expect(within(dialog()).getByRole('status')).toHaveTextContent('The board no longer lists this task.')
    expect(screen.getByTestId('task-terminal')).toBeInTheDocument()
  })

  it('shows a failed attach as not sent, and retries it', async () => {
    const state = tasksState({
      attach: vi.fn()
        .mockResolvedValueOnce({ ok: false, error: 'the tmux session has ended' })
        .mockResolvedValueOnce(attached('board-0000000000000002')),
    })
    renderDashboard(state)
    await openPopup()

    expect(chip()).toHaveTextContent('Failed')
    expect(within(dialog()).getByRole('alert')).toHaveTextContent(
      'Could not connect: the tmux session has ended. Input is not being sent.',
    )
    await act(async () => {
      fireEvent.click(within(dialog()).getByRole('button', { name: 'Retry' }))
    })
    expect(state.attach).toHaveBeenCalledTimes(2)
    expect(screen.getByTestId('task-terminal')).toHaveAttribute('data-session', 'board-0000000000000002')
  })

  it('shows a disconnect as not sent, and reconnects through a new attach request', async () => {
    const state = tasksState({
      attach: vi.fn()
        .mockResolvedValueOnce(attached('board-0000000000000001'))
        .mockResolvedValueOnce(attached('board-0000000000000001')),
    })
    renderDashboard(state)
    await openPopup()
    setStatus('connected')
    setStatus('disconnected')

    expect(chip()).toHaveTextContent('Disconnected')
    expect(within(dialog()).getByRole('alert')).toHaveTextContent('Disconnected. Input is not being sent.')
    await act(async () => {
      fireEvent.click(within(dialog()).getByRole('button', { name: 'Reconnect' }))
    })
    expect(state.attach).toHaveBeenCalledTimes(2)
    // The same attach is read again over a new WebSocket.
    expect(terminals.mounts).toEqual(['board-0000000000000001', 'board-0000000000000001'])
  })

  it('shows an ended tmux client as disconnected', async () => {
    renderDashboard()
    await openPopup()
    setStatus('connected')
    setStatus('ended')
    expect(chip()).toHaveTextContent('Disconnected')
    expect(within(dialog()).getByRole('alert')).toHaveTextContent('The tmux client ended. Input is not being sent.')
  })

  it('a WebSocket that never opened is failed', async () => {
    renderDashboard()
    await openPopup()
    setStatus('failed')
    expect(chip()).toHaveTextContent('Failed')
    expect(within(dialog()).getByRole('button', { name: 'Retry' })).toBeInTheDocument()
  })

  it('offers Go to pane and says who sizes the window when a workspace pane shows the same session', async () => {
    const onOpenTask = vi.fn()
    const { state } = renderDashboard(tasksState(), onOpenTask)
    await openPopup('wait-pane')
    expect(dialog()).toHaveTextContent('Pane infra shows this tmux session too')
    expect(dialog()).toHaveTextContent('the window takes the size of the client that was used last')

    fireEvent.click(within(dialog()).getByRole('button', { name: /^Go to pane/ }))
    expect(onOpenTask).toHaveBeenCalledWith(withPane, expect.objectContaining({ kind: 'goto' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(state.detach).toHaveBeenCalled()
  })
})

describe('TaskDashboard Type in pane: maximize', () => {
  it('toggles with the button, a header double-click and Cmd/Ctrl+Shift+Enter, without remounting the terminal', async () => {
    renderDashboard()
    await openPopup()
    setStatus('connected')
    const maximize = within(dialog()).getByRole('button', { name: 'Maximize' })
    expect(maximize).toHaveAttribute('aria-pressed', 'false')

    fireEvent.click(maximize)
    expect(dialog()).toHaveAttribute('data-maximized', 'true')
    expect(within(dialog()).getByRole('button', { name: 'Restore size' })).toHaveAttribute('aria-pressed', 'true')

    fireEvent.doubleClick(within(dialog()).getByTestId('task-input-header'))
    expect(dialog()).toHaveAttribute('data-maximized', 'false')

    const input = screen.getByLabelText('Terminal input')
    input.focus()
    const reached = vi.fn()
    input.addEventListener('keydown', reached)
    key(input, { key: 'Enter', metaKey: true, shiftKey: true })
    expect(dialog()).toHaveAttribute('data-maximized', 'true')
    expect(reached).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(input)

    expect(terminals.mounts).toEqual(['board-0000000000000001'])
  })

  it('a double-click on a header button does not toggle', async () => {
    renderDashboard()
    await openPopup()
    fireEvent.doubleClick(within(dialog()).getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog')?.getAttribute('data-maximized') ?? 'false').toBe('false')
  })

  it('opens maximized next time once maximized', async () => {
    renderDashboard()
    await openPopup()
    fireEvent.click(within(dialog()).getByRole('button', { name: 'Maximize' }))
    fireEvent.click(within(dialog()).getByRole('button', { name: 'Close' }))

    await openPopup('idle-1')
    expect(dialog()).toHaveAttribute('data-maximized', 'true')
  })

  it('is a full sheet with no maximize on a narrow screen', async () => {
    setNarrow(true)
    window.localStorage.setItem('panemux:task-terminal-maximized', 'true')
    renderDashboard()
    await openPopup()
    expect(dialog()).toHaveAttribute('data-sheet', 'true')
    expect(dialog()).toHaveAttribute('data-maximized', 'false')
    expect(within(dialog()).queryByRole('button', { name: 'Maximize' })).toBeNull()
    expect(within(dialog()).queryByRole('button', { name: 'Restore size' })).toBeNull()

    key(window, { key: 'Enter', ctrlKey: true, shiftKey: true })
    expect(dialog()).toHaveAttribute('data-maximized', 'false')
    expect(window.localStorage.getItem('panemux:task-terminal-maximized')).toBe('true')
  })
})

// Keep React in scope for the JSX in the mock factory above.
void React
