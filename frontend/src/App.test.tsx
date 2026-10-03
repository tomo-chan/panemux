import { useContext } from 'react'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { LayoutActionsContext } from './components/SplitContainer'
import type { LayoutNode, WorkspacesResponse } from './schemas'
import { applyTaskEventFrame, type TaskEventStore } from './utils/taskEvents'

const mockTerminalPane = vi.hoisted(() => vi.fn(({ pane }: { pane: { id: string } }) => <div data-pane-id={pane.id} />))

vi.mock('./components/TerminalPane', () => ({
  TerminalPane: mockTerminalPane,
}))

const devLayout: LayoutNode = {
  direction: 'horizontal',
  children: [
    { size: 50, pane: { id: 'main', type: 'local' } },
    { size: 50, pane: { id: 'side', type: 'local' } },
  ],
}

const workspaces: WorkspacesResponse = {
  active: 'dev',
  tab_position: 'top',
  vertical_bar_width: 280,
  items: [
    { id: 'dev', title: 'Dev', layout: devLayout },
    {
      id: 'ops',
      title: 'Ops',
      layout: { direction: 'vertical', children: [{ size: 100, pane: { id: 'ops-main', type: 'local' } }] },
    },
  ],
}

let currentWorkspaces: WorkspacesResponse | null = workspaces
let currentDisplayConfig: { show_header: boolean; show_status_bar: boolean; task_dashboard_shortcut?: string } = {
  show_header: false,
  show_status_bar: false,
}

const mockDeleteWorkspace = vi.fn()
const mockRenameWorkspace = vi.fn()
const mockSetWorkspaceTabPosition = vi.fn()
const mockSetWorkspaceVerticalBarWidth = vi.fn()
const mockSetActiveWorkspace = vi.fn()
const mockUpdateSizes = vi.fn()
const mockSplitPane = vi.fn()
const mockClosePane = vi.fn()
const mockSwapPanes = vi.fn()
const mockCreatePane = vi.fn().mockResolvedValue(undefined)
const mockMovePane = vi.fn().mockResolvedValue(undefined)
const mockAddWorkspace = vi.fn()
const taskEventsMock = vi.hoisted(() => ({
  onChange: null as null | ((change: unknown) => void),
  state: { store: null as unknown, status: 'connecting' as string },
}))
const mockUseBrowserNotificationPermission = vi.hoisted(() => vi.fn())
const mockUseSessionsOverview = vi.hoisted(() => vi.fn())
const mockUseGitInfoSnapshotMap = vi.hoisted(() => vi.fn())

vi.mock('./hooks/useLayout', () => ({
  useLayout: () => ({
    layout: currentWorkspaces
      ? currentWorkspaces.items.find((workspace) => workspace.id === currentWorkspaces?.active)?.layout ?? currentWorkspaces.items[0].layout
      : null,
    workspaces: currentWorkspaces,
    displayConfig: currentDisplayConfig,
    error: null,
    updateSizes: mockUpdateSizes,
    splitPane: mockSplitPane,
    closePane: mockClosePane,
    swapPanes: mockSwapPanes,
    createPane: mockCreatePane,
    movePane: mockMovePane,
    setActiveWorkspace: mockSetActiveWorkspace,
    addWorkspace: mockAddWorkspace,
    deleteWorkspace: mockDeleteWorkspace,
    renameWorkspace: mockRenameWorkspace,
    setWorkspaceTabPosition: mockSetWorkspaceTabPosition,
    setWorkspaceVerticalBarWidth: mockSetWorkspaceVerticalBarWidth,
  }),
}))

vi.mock('./hooks/useTaskEvents', () => ({
  useTaskEvents: (onChange: (change: unknown) => void) => {
    taskEventsMock.onChange = onChange
    return taskEventsMock.state
  },
}))

// Delivers one frame of /ws/tasks/events to the App, through the same
// reducer the real hook uses.
function emitTaskFrame(frame: unknown) {
  const before = taskEventsMock.state.store as TaskEventStore | null
  const result = applyTaskEventFrame(before, frame)
  if (!result.ok) throw new Error(result.reason)
  taskEventsMock.state = { store: result.store, status: 'live' }
  act(() => {
    taskEventsMock.onChange?.({ before, frame: result.frame, after: result.store })
  })
}

let taskFrameSeq = 0

function emitTaskSnapshot(tasks: unknown[], hosts: unknown[] = [{ name: '', status: 'ok' }]) {
  taskFrameSeq += 10
  emitTaskFrame({ type: 'snapshot', epoch: 'e0', seq: taskFrameSeq, hosts, tasks })
}

function emitTaskChange(op: 'added' | 'changed' | 'removed', task: unknown, prevState?: string) {
  taskFrameSeq += 1
  emitTaskFrame({ type: 'task', epoch: 'e0', seq: taskFrameSeq, op, task, ...(prevState ? { prev_state: prevState } : {}) })
}

function paneTask(id: string, paneId: string, state = 'busy', extra: Record<string, unknown> = {}) {
  return {
    id,
    host: '',
    agent: 'claude',
    session_id: id,
    cwd: '/workspace/user/project',
    state,
    location: { kind: 'outside', pane_id: paneId, attachable: false },
    ...extra,
  }
}

function waitingTask(id: string, paneId: string, waitId: string) {
  return paneTask(id, paneId, 'wait', { waiting_for: 'input needed', wait_id: waitId })
}

vi.mock('./hooks/useBrowserNotificationPermission', () => ({
  useBrowserNotificationPermission: mockUseBrowserNotificationPermission,
}))

vi.mock('./hooks/useSessionsOverview', () => ({
  useSessionsOverview: mockUseSessionsOverview,
}))

vi.mock('./hooks/useGitInfo', () => ({
  useGitInfoSnapshotMap: mockUseGitInfoSnapshotMap,
}))

const mockUseBoardSessionToken = vi.hoisted(() => vi.fn())

vi.mock('./hooks/useBoardSessionToken', () => ({
  useBoardSessionToken: mockUseBoardSessionToken,
}))

// Held in a box rather than returned literally so a test can open the settings
// dialog, which is the only way into the add-SSH-host flow below. Everything
// else in this file relies on the closed-dialog defaults, so each test that
// changes it restores them afterwards.
const paneSettings = vi.hoisted(() => {
  const defaults = () => ({
    isOpen: false,
    currentPane: null as { id: string, type: string, connection?: string } | null,
    sshConnectionNames: [] as string[],
    saveError: null as string | null,
    isSaving: false,
    openSettings: vi.fn(),
    closeSettings: vi.fn(),
    saveSettings: vi.fn(),
    addSSHConfigHost: vi.fn(),
    detectShell: vi.fn(),
    browseDirectories: vi.fn(),
  })
  return { defaults, value: defaults() }
})

vi.mock('./hooks/usePaneSettings', () => ({
  usePaneSettings: () => paneSettings.value,
}))

const mockUseTasks = vi.hoisted(() => vi.fn())

vi.mock('./hooks/useTasks', () => ({
  TASKS_POLL_INTERVAL_MS: 10000,
  useTasks: mockUseTasks,
}))

function tasksStateWith(tasks: unknown[]) {
  return {
    data: { hosts: [{ name: '', status: 'ok' }, { name: 'dev-server', status: 'ok' }], tasks },
    error: null,
    loading: false,
    updatedAt: null,
    refresh: vi.fn(),
    reconnect: vi.fn(),
  }
}

beforeEach(() => {
  mockUseTasks.mockReturnValue(tasksStateWith([]))
  taskEventsMock.state = { store: null, status: 'connecting' }
  taskEventsMock.onChange = null
  window.sessionStorage.clear()
})

describe('App workspace deletion', () => {
  let originalNotification: typeof Notification | undefined
  let notificationInstance: { onclick: (() => void) | null; close: ReturnType<typeof vi.fn> } | null

  beforeEach(() => {
    originalNotification = window.Notification
    mockUseBrowserNotificationPermission.mockImplementation(() => {})
    mockUseSessionsOverview.mockReturnValue({})
    mockUseGitInfoSnapshotMap.mockReturnValue({})
    mockUseBoardSessionToken.mockReturnValue({ token: '', commandCenterEnabled: false, agentBoardEnabled: false })
    notificationInstance = null
    vi.stubGlobal('Notification', vi.fn(function MockNotification(this: Notification) {
      notificationInstance = {
        onclick: null,
        close: vi.fn(),
      }
      return notificationInstance as unknown as Notification
    }) as unknown as typeof Notification)
    Object.defineProperty(window.Notification, 'permission', {
      configurable: true,
      value: 'granted',
    })
    Object.defineProperty(window.Notification, 'requestPermission', {
      configurable: true,
      value: vi.fn().mockResolvedValue('granted'),
    })
  })

  afterEach(() => {
    mockDeleteWorkspace.mockClear()
    mockRenameWorkspace.mockClear()
    mockSetWorkspaceTabPosition.mockClear()
    mockSetWorkspaceVerticalBarWidth.mockClear()
    mockSetActiveWorkspace.mockClear()
    mockUpdateSizes.mockClear()
    mockSplitPane.mockClear()
    mockClosePane.mockClear()
    mockSwapPanes.mockClear()
    mockCreatePane.mockClear()
    mockMovePane.mockClear()
    mockAddWorkspace.mockClear()
    mockTerminalPane.mockClear()
    mockUseBrowserNotificationPermission.mockReset()
    mockUseSessionsOverview.mockReset()
    mockUseGitInfoSnapshotMap.mockReset()
    mockUseBoardSessionToken.mockReset()
    currentWorkspaces = workspaces
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    if (originalNotification === undefined) {
      // @ts-expect-error test cleanup
      delete window.Notification
    } else {
      window.Notification = originalNotification
    }
  })


  it('does not mount inactive workspace panes or background readers that would consume hidden output', () => {
    render(<App />)

    expect(mockTerminalPane).toHaveBeenCalledWith(
      expect.objectContaining({ pane: expect.objectContaining({ id: 'main' }) }),
      expect.anything(),
    )
    expect(mockTerminalPane).not.toHaveBeenCalledWith(
      expect.objectContaining({ pane: expect.objectContaining({ id: 'ops-main' }) }),
      expect.anything(),
    )
  })

  it('renders workspace summaries in the tabs and opens integrated details', () => {
    mockUseSessionsOverview.mockReturnValue({
      main: { id: 'main', type: 'local', title: 'Main', state: 'connected' },
      side: { id: 'side', type: 'local', title: 'Side', state: 'disconnected' },
      'ops-main': { id: 'ops-main', type: 'local', title: 'Ops Main', state: 'exited' },
    })
    mockUseGitInfoSnapshotMap.mockReturnValue({
      main: { is_git: true, repo: 'panemux', branch: 'feature/dashboard', pr_number: 42, pr_url: 'https://github.com/example/panemux/pull/42' },
    })

    render(<App />)

    expect(screen.getByText('2 panes · 1 up · 1 down')).toBeInTheDocument()
    fireEvent.mouseEnter(screen.getByRole('tab', { name: /^Dev\b/ }))
    expect(screen.getByRole('region', { name: 'Dev workspace details' })).toBeInTheDocument()
    expect(screen.getByText('Main')).toBeInTheDocument()
    expect(screen.getByText('feature/dashboard')).toBeInTheDocument()
    expect(screen.getByText('PR #42')).toBeInTheDocument()
  })

  it('switches workspace from the integrated details panel', () => {
    mockUseSessionsOverview.mockReturnValue({
      'ops-main': { id: 'ops-main', type: 'local', title: 'Ops Main', state: 'connected' },
    })
    render(<App />)

    fireEvent.mouseEnter(screen.getByRole('tab', { name: /^Ops\b/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Open pane Ops Main in Ops' }))

    expect(mockSetActiveWorkspace).toHaveBeenCalledWith('ops')
  })

  it('focuses the pane when a pane summary is selected in the active workspace', async () => {
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => (
      <div data-pane-id={pane.id}>
        <button type="button">Focus target {pane.id}</button>
      </div>
    ))

    render(<App />)
    fireEvent.mouseEnter(screen.getByRole('tab', { name: /^Dev\b/ }))
    fireEvent.click(screen.getByRole('button', { name: /Open pane main in Dev/i }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Focus target main' })).toHaveFocus()
    })
    expect(screen.getByRole('button', { name: /Open pane main in Dev/i })).toHaveStyle({
      background: 'rgba(86, 156, 214, 0.16)',
    })
  })

  it('prefers the xterm focus target over header buttons when focusing from a pane summary', async () => {
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => (
      <div data-pane-id={pane.id}>
        <button type="button">Header action {pane.id}</button>
        <textarea className="xterm-helper-textarea" aria-label={`Terminal focus ${pane.id}`} />
      </div>
    ))

    render(<App />)
    fireEvent.mouseEnter(screen.getByRole('tab', { name: /^Dev\b/ }))
    fireEvent.click(screen.getByRole('button', { name: /Open pane main in Dev/i }))

    await waitFor(() => {
      expect(screen.getByLabelText('Terminal focus main')).toHaveFocus()
    })
  })

  it('asks in the app\'s own dialog before deleting a workspace, and does not block on window.confirm', () => {
    const nativeConfirm = vi.spyOn(window, 'confirm').mockReturnValue(true)

    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Delete Dev workspace' }))

    expect(nativeConfirm).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: 'Delete workspace' })).toBeDefined()
    expect(screen.getByText(/Delete workspace "Dev"\?/)).toBeDefined()
    // Nothing is deleted by the question itself.
    expect(mockDeleteWorkspace).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    expect(mockDeleteWorkspace).toHaveBeenCalledWith('dev')
    expect(screen.queryByRole('dialog', { name: 'Delete workspace' })).toBeNull()
  })

  it('keeps the workspace when the delete confirmation is cancelled', () => {
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Delete Dev workspace' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(mockDeleteWorkspace).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog', { name: 'Delete workspace' })).toBeNull()
  })

  it('keeps the workspace when the delete confirmation is dismissed with Escape', () => {
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Delete Dev workspace' }))

    // Asserting the dialog is up before dismissing it is what makes this a
    // test of the dialog rather than of nothing: "delete was not called" is
    // equally true of a build that never asked in the first place.
    expect(screen.getByRole('dialog', { name: 'Delete workspace' })).toBeDefined()

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(mockDeleteWorkspace).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog', { name: 'Delete workspace' })).toBeNull()
  })

  it('creates a default local pane to the right of the current pane', async () => {
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return (
        <div data-pane-id={pane.id}>
          <button onClick={() => ctx?.onCreatePaneBeside(pane.id, 'right')}>Add right of {pane.id}</button>
        </div>
      )
    })

    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Add right of main' }))

    expect(mockCreatePane).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'local', id: expect.any(String) }),
      { type: 'pane-edge', targetPaneId: 'main', edge: 'right' },
    )
  })

  it('preserves maximize state per workspace when switching tabs', () => {
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      const maximized = ctx?.maximizedPaneId === pane.id
      return (
        <div data-pane-id={pane.id} data-maximized={maximized ? 'true' : 'false'}>
          <button onClick={() => ctx?.onMaximize(maximized ? null : pane.id)}>
            Toggle maximize {pane.id}
          </button>
        </div>
      )
    })

    const { rerender } = render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Toggle maximize main' }))
    expect(screen.getByText('Toggle maximize main').closest('[data-pane-id="main"]')).toHaveAttribute('data-maximized', 'true')

    currentWorkspaces = { ...workspaces, active: 'ops' }
    rerender(<App />)

    expect(screen.getByText('Toggle maximize ops-main').closest('[data-pane-id="ops-main"]')).toHaveAttribute('data-maximized', 'false')

    fireEvent.click(screen.getByRole('button', { name: 'Toggle maximize ops-main' }))
    expect(screen.getByText('Toggle maximize ops-main').closest('[data-pane-id="ops-main"]')).toHaveAttribute('data-maximized', 'true')

    currentWorkspaces = workspaces
    rerender(<App />)

    expect(screen.getByText('Toggle maximize main').closest('[data-pane-id="main"]')).toHaveAttribute('data-maximized', 'true')

    currentWorkspaces = { ...workspaces, active: 'ops' }
    rerender(<App />)

    expect(screen.getByText('Toggle maximize ops-main').closest('[data-pane-id="ops-main"]')).toHaveAttribute('data-maximized', 'true')
  })

  it('drops maximize state for workspaces that no longer exist', () => {
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      const maximized = ctx?.maximizedPaneId === pane.id
      return (
        <div data-pane-id={pane.id} data-maximized={maximized ? 'true' : 'false'}>
          <button onClick={() => ctx?.onMaximize(maximized ? null : pane.id)}>
            Toggle maximize {pane.id}
          </button>
        </div>
      )
    })

    const { rerender } = render(<App />)

    currentWorkspaces = { ...workspaces, active: 'ops' }
    rerender(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Toggle maximize ops-main' }))
    expect(screen.getByText('Toggle maximize ops-main').closest('[data-pane-id="ops-main"]')).toHaveAttribute('data-maximized', 'true')

    currentWorkspaces = {
      ...workspaces,
      active: 'dev',
      items: [workspaces.items[0]],
    }
    rerender(<App />)

    currentWorkspaces = { ...workspaces, active: 'ops' }
    rerender(<App />)

    expect(screen.getByText('Toggle maximize ops-main').closest('[data-pane-id="ops-main"]')).toHaveAttribute('data-maximized', 'false')
  })

  it('shows a user-visible error when creating a pane fails', async () => {
    mockCreatePane.mockRejectedValueOnce(new Error('HTTP 500'))
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return (
        <div data-pane-id={pane.id}>
          <button onClick={() => ctx?.onCreatePaneBeside(pane.id, 'right')}>Add right of {pane.id}</button>
        </div>
      )
    })

    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Add right of main' }))

    expect(await screen.findByText('Failed to create terminal: HTTP 500')).toBeInTheDocument()
  })

  it('dismisses the create pane error banner when requested', async () => {
    mockCreatePane.mockRejectedValueOnce(new Error('HTTP 500'))
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return (
        <div data-pane-id={pane.id}>
          <button onClick={() => ctx?.onCreatePaneBeside(pane.id, 'right')}>Add right of {pane.id}</button>
        </div>
      )
    })

    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Add right of main' }))

    expect(await screen.findByRole('alert')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss create terminal error' }))

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows a generic create error when the rejection is not an Error instance', async () => {
    mockCreatePane.mockRejectedValueOnce('boom')
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return (
        <div data-pane-id={pane.id}>
          <button onClick={() => ctx?.onCreatePaneBeside(pane.id, 'right')}>Add right of {pane.id}</button>
        </div>
      )
    })

    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Add right of main' }))

    expect(await screen.findByText('Failed to create terminal: Something went wrong')).toBeInTheDocument()
  })

  it('passes workspace rename from edit-mode tabs', () => {
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Rename Dev workspace' }))
    const input = screen.getByLabelText('Workspace name')
    fireEvent.change(input, { target: { value: 'Development' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(mockRenameWorkspace).toHaveBeenCalledWith('dev', 'Development')
  })

  it('passes workspace tab position changes from edit-mode tabs', () => {
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Place workspace tabs on right' }))

    expect(mockSetWorkspaceTabPosition).toHaveBeenCalledWith('right')
  })

  it('passes vertical workspace bar width changes from the resizer', () => {
    currentWorkspaces = { ...workspaces, tab_position: 'left' }
    render(<App />)

    const resizer = screen.getByTestId('workspace-bar-resizer')
    fireEvent.mouseDown(resizer, { clientX: 280 })
    fireEvent.mouseMove(window, { clientX: 320 })
    fireEvent.mouseUp(window)

    expect(mockSetWorkspaceVerticalBarWidth).toHaveBeenCalledWith(320)
  })

  it('moves a dragged pane to another workspace tab', () => {
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return <button onMouseDown={() => ctx?.setDragSourcePaneId(pane.id)}>Start drag {pane.id}</button>
    })

    render(<App />)
    fireEvent.mouseDown(screen.getByRole('button', { name: 'Start drag main' }), { button: 0 })
    fireEvent.mouseEnter(screen.getByRole('tab', { name: /^Ops\b/ }))
    fireEvent.mouseUp(screen.getByRole('tab', { name: /^Ops\b/ }))

    expect(mockMovePane).toHaveBeenCalledWith('main', { type: 'workspace-tab', workspaceId: 'ops' })
  })

  it('shows a user-visible error when moving a pane fails to persist', async () => {
    mockMovePane.mockRejectedValueOnce(new Error('HTTP 500'))
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return <button onClick={() => ctx?.onMovePaneToWorkspaceEdge(pane.id, 'left')}>Move {pane.id}</button>
    })

    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Move main' }))

    expect(mockMovePane).toHaveBeenCalledWith('main', { type: 'workspace-edge', edge: 'left' })
    expect(await screen.findByText('Failed to move terminal: HTTP 500')).toBeInTheDocument()
  })

  it('dismisses the move error banner when requested', async () => {
    mockMovePane.mockRejectedValueOnce(new Error('HTTP 500'))
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return <button onClick={() => ctx?.onMovePaneToWorkspaceEdge(pane.id, 'left')}>Move {pane.id}</button>
    })

    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Move main' }))

    expect(await screen.findByRole('alert')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss move error' }))

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows a generic move error when the rejection is not an Error instance', async () => {
    mockMovePane.mockRejectedValueOnce('boom')
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return <button onClick={() => ctx?.onMovePaneToWorkspaceEdge(pane.id, 'left')}>Move {pane.id}</button>
    })

    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Move main' }))

    expect(await screen.findByText('Failed to move terminal: Something went wrong')).toBeInTheDocument()
  })

  it('marks an inactive workspace when a task in one of its panes starts waiting', () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    render(<App />)
    emitTaskSnapshot([paneTask('a', 'main')])
    expect(screen.getByRole('tab', { name: /^Dev\b/ })).not.toHaveAttribute('data-attention')

    emitTaskChange('changed', waitingTask('a', 'main', 'w1-a'), 'busy')

    expect(screen.getByRole('tab', { name: /^Dev\b/ })).toHaveAttribute('data-attention', 'true')
    expect(screen.getByRole('tab', { name: /^Ops\b/ })).not.toHaveAttribute('data-attention')
  })

  it('notifies a hidden wait once, tagged with its wait ID and without what it waits for', () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    render(<App />)
    emitTaskSnapshot([paneTask('a', 'main')])
    emitTaskChange('changed', waitingTask('a', 'main', 'w1-a'), 'busy')

    expect(window.Notification).toHaveBeenCalledTimes(1)
    expect(window.Notification).toHaveBeenCalledWith('Agent waiting', { body: 'claude on Local: project', tag: 'w1-a' })

    // A reconnect's snapshot carries the same wait: not notified again.
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a')])
    expect(window.Notification).toHaveBeenCalledTimes(1)

    // A later wait of the same task is.
    emitTaskChange('changed', paneTask('a', 'main'), 'wait')
    emitTaskChange('changed', waitingTask('a', 'main', 'w1-b'), 'busy')
    expect(window.Notification).toHaveBeenCalledTimes(2)
    expect(window.Notification).toHaveBeenLastCalledWith('Agent waiting', { body: 'claude on Local: project', tag: 'w1-b' })
  })

  it('does not notify a wait again after the tab reloads', () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    const { unmount } = render(<App />)
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a')])
    expect(window.Notification).toHaveBeenCalledTimes(1)
    unmount()

    taskEventsMock.state = { store: null, status: 'connecting' }
    render(<App />)
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a'), waitingTask('b', 'side', 'w1-b')])
    expect(window.Notification).toHaveBeenCalledTimes(2)
    expect(window.Notification).toHaveBeenLastCalledWith('Agent waiting', expect.objectContaining({ tag: 'w1-b' }))
  })

  it('reveals the pane when the notification is clicked: its workspace, out from behind a maximized pane', () => {
    const focusSpy = vi.spyOn(window, 'focus').mockImplementation(() => {})
    mockSetActiveWorkspace.mockResolvedValue(undefined)
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return (
        <div
          data-pane-id={pane.id}
          data-maximized={ctx?.maximizedPaneId === pane.id ? 'true' : 'false'}
          data-attention={ctx?.hasPaneAttention(pane.id) ? 'true' : undefined}
        >
          <button onClick={() => ctx?.onMaximize(pane.id)}>Maximize {pane.id}</button>
        </div>
      )
    })
    vi.spyOn(document, 'hasFocus').mockReturnValue(true)

    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Maximize side' }))
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a')])
    expect(window.Notification).toHaveBeenCalledTimes(1)
    expect(document.querySelector('[data-pane-id="main"]')).toHaveAttribute('data-attention', 'true')

    act(() => notificationInstance?.onclick?.())

    expect(focusSpy).toHaveBeenCalled()
    expect(mockSetActiveWorkspace).toHaveBeenCalledWith('dev')
    expect(notificationInstance?.close).toHaveBeenCalled()
    expect(document.querySelector('[data-pane-id="side"]')).toHaveAttribute('data-maximized', 'false')
    expect(document.querySelector('[data-pane-id="main"]')).not.toHaveAttribute('data-attention')
    expect(document.querySelector('[data-pane-id="main"]')).toHaveClass('panemux-pane-task-flash')
  })

  it('logs workspace switch failures from notification clicks', async () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    const switchError = new Error('switch failed')
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.spyOn(window, 'focus').mockImplementation(() => {})
    mockSetActiveWorkspace.mockRejectedValueOnce(switchError)

    render(<App />)
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a')])
    act(() => notificationInstance?.onclick?.())
    await Promise.resolve()

    expect(consoleErrorSpy).toHaveBeenCalledWith(switchError)
  })

  it('does not notify a wait whose pane is on screen while the browser is active, but still marks the pane', () => {
    vi.spyOn(document, 'hasFocus').mockReturnValue(true)
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return <div data-pane-id={pane.id} data-attention={ctx?.hasPaneAttention(pane.id) ? 'true' : undefined} />
    })

    render(<App />)
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a')])

    expect(window.Notification).not.toHaveBeenCalled()
    expect(document.querySelector('[data-pane-id="main"]')).toHaveAttribute('data-attention', 'true')
    expect(screen.getByRole('tab', { name: /^Dev\b/ })).not.toHaveAttribute('data-attention')
  })

  it('does not notify a wait that starts before the workspaces are loaded, even once they are', () => {
    vi.spyOn(document, 'hasFocus').mockReturnValue(true)
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return <div data-pane-id={pane.id} data-attention={ctx?.hasPaneAttention(pane.id) ? 'true' : undefined} />
    })
    currentWorkspaces = null

    const { rerender } = render(<App />)
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a')])
    currentWorkspaces = workspaces
    rerender(<App />)

    expect(window.Notification).not.toHaveBeenCalled()
    expect(document.querySelector('[data-pane-id="main"]')).toHaveAttribute('data-attention', 'true')
  })

  it('neither notifies nor marks a wait its host has not answered for, and marks the one still there once it does', () => {
    vi.spyOn(document, 'hasFocus').mockReturnValue(false)
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return <div data-pane-id={pane.id} data-attention={ctx?.hasPaneAttention(pane.id) ? 'true' : undefined} />
    })

    render(<App />)
    // Observation resumed: the snapshot keeps what was last observed, with
    // the host not answered yet.
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-stale'), waitingTask('b', 'side', 'w1-b')], [{ name: '', status: 'pending' }])
    expect(document.querySelector('[data-pane-id="main"]')).not.toHaveAttribute('data-attention')
    expect(document.querySelector('[data-pane-id="side"]')).not.toHaveAttribute('data-attention')

    // The host answers: its status first, then that the stale wait had ended.
    taskFrameSeq += 1
    emitTaskFrame({ type: 'host', epoch: 'e0', seq: taskFrameSeq, op: 'changed', host: { name: '', status: 'ok' } })
    emitTaskChange('changed', paneTask('a', 'main'), 'wait')

    expect(window.Notification).not.toHaveBeenCalled()
    expect(document.querySelector('[data-pane-id="main"]')).not.toHaveAttribute('data-attention')
    expect(document.querySelector('[data-pane-id="side"]')).toHaveAttribute('data-attention', 'true')
  })

  it('resolves a notification click against where the task is shown at the click', () => {
    vi.spyOn(window, 'focus').mockImplementation(() => {})
    vi.spyOn(document, 'hasFocus').mockReturnValue(false)

    render(<App />)
    emitTaskSnapshot([waitingTask('a', 'nowhere', 'w1-a')])
    expect(window.Notification).toHaveBeenCalledTimes(1)
    emitTaskChange('changed', waitingTask('a', 'side', 'w1-a'), 'wait')

    act(() => notificationInstance?.onclick?.())

    expect(mockSetActiveWorkspace).toHaveBeenCalledWith('dev')
    expect(screen.queryByTestId('task-card-a')).not.toBeInTheDocument()
  })

  it('notifies a wait whose pane is on screen when the browser is not active', () => {
    vi.spyOn(document, 'hasFocus').mockReturnValue(false)
    render(<App />)
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a')])
    expect(window.Notification).toHaveBeenCalledTimes(1)
  })

  it('marks the pane but neither notifies nor asks for permission when it is not granted', () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    Object.defineProperty(window.Notification, 'permission', {
      configurable: true,
      value: 'default',
    })

    render(<App />)
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a')])

    expect(window.Notification.requestPermission).not.toHaveBeenCalled()
    expect(window.Notification).not.toHaveBeenCalled()
    expect(screen.getByRole('tab', { name: /^Dev\b/ })).toHaveAttribute('data-attention', 'true')
  })

  it('clears the attention when the wait ends wherever it was answered, and while it reads unknown', () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    render(<App />)
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a')])
    const dev = () => screen.getByRole('tab', { name: /^Dev\b/ })
    expect(dev()).toHaveAttribute('data-attention', 'true')

    emitTaskChange('changed', paneTask('a', 'main', 'unknown'), 'wait')
    expect(dev()).not.toHaveAttribute('data-attention')
    emitTaskChange('changed', waitingTask('a', 'main', 'w1-a'), 'unknown')
    expect(dev()).toHaveAttribute('data-attention', 'true')

    emitTaskChange('changed', paneTask('a', 'main', 'busy'), 'wait')
    expect(dev()).not.toHaveAttribute('data-attention')

    emitTaskChange('changed', waitingTask('a', 'main', 'w1-b'), 'busy')
    expect(dev()).toHaveAttribute('data-attention', 'true')
    emitTaskChange('removed', waitingTask('a', 'main', 'w1-b'), 'wait')
    expect(dev()).not.toHaveAttribute('data-attention')
  })

  it('keeps a pane marked while another task in it still waits', () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    render(<App />)
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a'), waitingTask('b', 'main', 'w1-b')])

    emitTaskChange('changed', paneTask('a', 'main', 'idle'), 'wait')
    expect(screen.getByRole('tab', { name: /^Dev\b/ })).toHaveAttribute('data-attention', 'true')

    emitTaskChange('changed', paneTask('b', 'main', 'idle'), 'wait')
    expect(screen.getByRole('tab', { name: /^Dev\b/ })).not.toHaveAttribute('data-attention')
  })

  it('notifies a waiting task no pane shows, and opens the dashboard on it when clicked', async () => {
    vi.spyOn(window, 'focus').mockImplementation(() => {})
    const daemonTask = {
      id: 'local:codex:d1', host: '', agent: 'codex', session_id: 'd1', cwd: '/workspace/user/other',
      state: 'wait', waiting_for: 'input needed', wait_id: 'e1-e0-3',
      location: { kind: 'daemon', attachable: false },
    }
    mockUseTasks.mockReturnValue(tasksStateWith([{ ...daemonTask, state: 'busy', waiting_for: undefined, wait_id: undefined }]))

    render(<App />)
    emitTaskSnapshot([daemonTask])
    expect(window.Notification).toHaveBeenCalledWith('Agent waiting', { body: 'codex on Local: other', tag: 'e1-e0-3' })

    act(() => notificationInstance?.onclick?.())

    const card = await screen.findByTestId('task-card-local:codex:d1')
    await waitFor(() => expect(card).toHaveAttribute('data-selected', 'true'))
    expect(card).toHaveClass('td-card-flash')
  })

  it('does not notify a wait the dashboard on screen lists, and notifies one it does not', () => {
    vi.spyOn(document, 'hasFocus').mockReturnValue(true)
    mockUseTasks.mockReturnValue(tasksStateWith([paneTask('listed', 'nowhere')]))

    render(<App />)
    fireEvent.keyDown(window, { key: 's', shiftKey: true, ctrlKey: true, metaKey: true })
    expect(screen.getByTestId('task-card-listed')).toBeInTheDocument()

    emitTaskSnapshot([waitingTask('listed', 'nowhere', 'w1-l'), waitingTask('unlisted', 'main', 'w1-u')])

    expect(window.Notification).toHaveBeenCalledTimes(1)
    expect(window.Notification).toHaveBeenCalledWith('Agent waiting', expect.objectContaining({ tag: 'w1-u' }))
  })

  it('does not mark a pane again for a wait already cleared in this tab', () => {
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return (
        <div data-pane-id={pane.id} data-attention={ctx?.hasPaneAttention(pane.id) ? 'true' : undefined}>
          <button onClick={() => ctx?.clearPaneAttention(pane.id)}>Clear {pane.id}</button>
        </div>
      )
    })

    const { unmount } = render(<App />)
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a'), waitingTask('b', 'side', 'w1-b')])
    fireEvent.click(screen.getByRole('button', { name: 'Clear main' }))
    expect(document.querySelector('[data-pane-id="main"]')).not.toHaveAttribute('data-attention')
    expect(document.querySelector('[data-pane-id="side"]')).toHaveAttribute('data-attention', 'true')

    unmount()
    taskEventsMock.state = { store: null, status: 'connecting' }
    render(<App />)
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a'), waitingTask('b', 'side', 'w1-b')])
    expect(document.querySelector('[data-pane-id="main"]')).not.toHaveAttribute('data-attention')
    expect(document.querySelector('[data-pane-id="side"]')).toHaveAttribute('data-attention', 'true')
  })

  it('clears a workspace\'s attention when the workspace is selected', () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    render(<App />)
    emitTaskSnapshot([waitingTask('a', 'main', 'w1-a'), waitingTask('b', 'ops-main', 'w1-b')])
    expect(screen.getByRole('tab', { name: /^Dev\b/ })).toHaveAttribute('data-attention', 'true')

    fireEvent.click(screen.getByRole('tab', { name: /^Dev\b/ }))

    expect(screen.getByRole('tab', { name: /^Dev\b/ })).not.toHaveAttribute('data-attention')
  })

  it('does not show the Agent Board button when agent_board is disabled', () => {
    mockUseBoardSessionToken.mockReturnValue({ token: 'tok', commandCenterEnabled: false, agentBoardEnabled: false })

    render(<App />)

    expect(screen.queryByRole('button', { name: 'Open agent board' })).toBeNull()
  })

  it('does not show the Agent Board button when there is no token yet', () => {
    mockUseBoardSessionToken.mockReturnValue({ token: '', commandCenterEnabled: false, agentBoardEnabled: true })

    render(<App />)

    expect(screen.queryByRole('button', { name: 'Open agent board' })).toBeNull()
  })

  it('shows the Agent Board button and opens the panel when agent_board is enabled with a token', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ statuses: {}, messages: [] }),
    }))
    mockUseBoardSessionToken.mockReturnValue({ token: 'tok', commandCenterEnabled: false, agentBoardEnabled: true })

    render(<App />)

    const button = screen.getByRole('button', { name: 'Open agent board' })
    fireEvent.click(button)

    expect(screen.getByRole('dialog', { name: 'Agent board' })).toBeInTheDocument()
  })

  it('toggles the agent board panel with Cmd/Ctrl+Shift+B even while a pane has focus', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ statuses: {}, messages: [] }),
    }))
    mockUseBoardSessionToken.mockReturnValue({ token: 'tok', commandCenterEnabled: false, agentBoardEnabled: true })

    render(<App />)

    fireEvent.keyDown(window, { key: 'B', shiftKey: true, ctrlKey: true })
    expect(screen.getByRole('dialog', { name: 'Agent board' })).toBeInTheDocument()

    fireEvent.keyDown(window, { key: 'B', shiftKey: true, ctrlKey: true })
    expect(screen.queryByRole('dialog', { name: 'Agent board' })).toBeNull()
  })

  it('does not register the agent board shortcut when agent_board is disabled', () => {
    mockUseBoardSessionToken.mockReturnValue({ token: 'tok', commandCenterEnabled: false, agentBoardEnabled: false })

    render(<App />)

    fireEvent.keyDown(window, { key: 'B', shiftKey: true, ctrlKey: true })

    expect(screen.queryByRole('dialog', { name: 'Agent board' })).toBeNull()
  })
})

describe('App adding an SSH host from the pane settings dialog', () => {
  let addSSHConfigHost: ReturnType<typeof paneSettings.defaults>['addSSHConfigHost']

  beforeEach(() => {
    mockUseBrowserNotificationPermission.mockImplementation(() => {})
    mockUseSessionsOverview.mockReturnValue({})
    mockUseGitInfoSnapshotMap.mockReturnValue({})
    mockUseBoardSessionToken.mockReturnValue({ token: '', commandCenterEnabled: false, agentBoardEnabled: false })

    addSSHConfigHost = vi.fn().mockResolvedValue('prod-web')
    paneSettings.value = {
      ...paneSettings.defaults(),
      isOpen: true,
      // An ssh pane is what makes the settings dialog show the connection
      // picker the "+ Add" button lives beside.
      currentPane: { id: 'main', type: 'ssh', connection: '' },
      addSSHConfigHost,
    }
  })

  afterEach(() => {
    paneSettings.value = paneSettings.defaults()
    mockUseBrowserNotificationPermission.mockReset()
    mockUseSessionsOverview.mockReset()
    mockUseGitInfoSnapshotMap.mockReset()
    mockUseBoardSessionToken.mockReset()
    vi.restoreAllMocks()
  })

  function fillHost() {
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'prod-web' } })
    fireEvent.change(screen.getByLabelText('Hostname'), { target: { value: 'prod.example.com' } })
    fireEvent.change(screen.getByLabelText('User'), { target: { value: 'ubuntu' } })
  }

  it('offers no add-host dialog until it is asked for', () => {
    render(<App />)

    expect(screen.queryByLabelText('Add SSH host')).toBeNull()
  })

  it('opens the add-host dialog from the connection picker', () => {
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: '+ Add' }))

    expect(screen.getByLabelText('Add SSH host')).toBeInTheDocument()
  })

  it('writes the host to the ssh config and closes the dialog', async () => {
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '+ Add' }))

    fillHost()
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    await waitFor(() => {
      expect(addSSHConfigHost).toHaveBeenCalledWith({
        name: 'prod-web',
        hostname: 'prod.example.com',
        user: 'ubuntu',
      })
    })
    await waitFor(() => {
      expect(screen.queryByLabelText('Add SSH host')).toBeNull()
    })
  })

  it('keeps the dialog open and shows why when the write fails', async () => {
    addSSHConfigHost.mockRejectedValue(new Error('~/.ssh/config is read-only'))
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '+ Add' }))

    fillHost()
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    expect(await screen.findByText('~/.ssh/config is read-only')).toBeInTheDocument()
    expect(screen.getByLabelText('Add SSH host')).toBeInTheDocument()
  })

  it('falls back to a generic message when the failure carries no message', async () => {
    addSSHConfigHost.mockRejectedValue('nope')
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '+ Add' }))

    fillHost()
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    expect(await screen.findByText('Failed to add host')).toBeInTheDocument()
  })

  it('marks the dialog as saving while the write is in flight, and stops when it settles', async () => {
    let finishWrite: (name: string) => void = () => {}
    addSSHConfigHost.mockReturnValue(new Promise<string>((resolve) => { finishWrite = resolve }))
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '+ Add' }))

    fillHost()
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    expect(await screen.findByRole('button', { name: 'Saving…' })).toBeDisabled()

    finishWrite('prod-web')

    await waitFor(() => {
      expect(screen.queryByLabelText('Add SSH host')).toBeNull()
    })
  })

  it('abandons the dialog without writing anything when it is cancelled', () => {
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: '+ Add' }))
    fillHost()

    // Both dialogs are on screen, and both have a Cancel; this is the add-host
    // one.
    fireEvent.click(within(screen.getByLabelText('Add SSH host')).getByRole('button', { name: 'Cancel' }))

    expect(addSSHConfigHost).not.toHaveBeenCalled()
    expect(screen.queryByLabelText('Add SSH host')).toBeNull()
  })

  // efficacy:exempt unchanged by the task dashboard branch — the red-check maps the blank line
  // before the describe block appended below this one onto this test.
  it('offers a host that is already configured as a connection for the pane', () => {
    // The other half of the flow: once the hook reports the refreshed list, the
    // name has to reach the picker the pane is actually configured from.
    paneSettings.value = { ...paneSettings.value, sshConnectionNames: ['prod-web', 'staging'] }
    render(<App />)

    const connectionPicker = Array.from(
      screen.getByLabelText('Pane settings').querySelectorAll('select'),
    ).find((select) => select.querySelector('option[value=""]')?.textContent === '— select connection —')

    expect(connectionPicker).toBeDefined()
    expect(Array.from(connectionPicker!.options).map((option) => option.textContent))
      .toEqual(['— select connection —', 'prod-web', 'staging'])
  })
})

describe('App task dashboard layer', () => {
  beforeEach(() => {
    currentWorkspaces = workspaces
    mockUseBrowserNotificationPermission.mockImplementation(() => {})
    mockUseSessionsOverview.mockReturnValue({})
    mockUseGitInfoSnapshotMap.mockReturnValue({})
    mockUseBoardSessionToken.mockReturnValue({ token: '', commandCenterEnabled: false, agentBoardEnabled: false })
    mockCreatePane.mockClear()
    mockCreatePane.mockResolvedValue(undefined)
    mockSetActiveWorkspace.mockClear()
    mockSetActiveWorkspace.mockResolvedValue(undefined)
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => <div data-pane-id={pane.id} />)
  })

  afterEach(() => {
    currentWorkspaces = workspaces
    vi.clearAllMocks()
  })

  const waitingTask = {
    id: 'local:claude:a',
    host: '',
    agent: 'claude',
    session_id: 'aaaa',
    cwd: '/workspace/user/panemux',
    state: 'wait',
    waiting_for: 'input needed',
    location: { kind: 'tmux', tmux_session: 'task-a', attachable: true },
  }

  it('starts on the workspaces and collects tasks only while the dashboard is shown', () => {
    render(<App />)

    expect(screen.queryByRole('region', { name: 'Task dashboard' })).not.toBeInTheDocument()
    expect(mockUseTasks).toHaveBeenLastCalledWith(false)

    fireEvent.click(screen.getByRole('button', { name: 'Tasks' }))
    expect(screen.getByRole('region', { name: 'Task dashboard' })).toBeInTheDocument()
    expect(mockUseTasks).toHaveBeenLastCalledWith(true)
    expect(screen.getByTestId('workspace-layer')).toHaveAttribute('inert')

    fireEvent.click(screen.getByRole('button', { name: 'Workspaces' }))
    expect(screen.queryByRole('region', { name: 'Task dashboard' })).not.toBeInTheDocument()
    expect(mockUseTasks).toHaveBeenLastCalledWith(false)
    expect(screen.getByTestId('workspace-layer')).not.toHaveAttribute('inert')
  })

  it('switches layers with Cmd/Ctrl+Shift+S by default, even from inside a terminal', () => {
    render(<App />)
    const terminal = document.querySelector<HTMLElement>('[data-pane-id="main"]')!
    terminal.tabIndex = 0

    fireEvent.keyDown(terminal, { key: 'S', ctrlKey: true, shiftKey: true })
    expect(screen.getByRole('region', { name: 'Task dashboard' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Workspaces' })).toHaveAttribute('aria-keyshortcuts', 'Control+Shift+S')

    fireEvent.keyDown(window, { key: 's', metaKey: true, shiftKey: true })
    expect(screen.queryByRole('region', { name: 'Task dashboard' })).not.toBeInTheDocument()
  })

  it('uses the key configured in display.task_dashboard_shortcut', () => {
    currentDisplayConfig = { show_header: false, show_status_bar: false, task_dashboard_shortcut: 'J' }
    try {
      render(<App />)
      expect(screen.getByRole('button', { name: 'Tasks' })).toHaveAttribute('title', 'Task dashboard (Ctrl+Shift+J)')

      fireEvent.keyDown(window, { key: 'S', ctrlKey: true, shiftKey: true })
      expect(screen.queryByRole('region', { name: 'Task dashboard' })).not.toBeInTheDocument()

      fireEvent.keyDown(window, { key: 'J', ctrlKey: true, shiftKey: true })
      expect(screen.getByRole('region', { name: 'Task dashboard' })).toBeInTheDocument()
    } finally {
      currentDisplayConfig = { show_header: false, show_status_bar: false }
    }
  })

  // The palette, the history panel and the board live in the workspace
  // layer, which is inert while the dashboard is shown: opened there they
  // would be drawn above the dashboard and take no input.
  it('does not open the palette or the board over the dashboard, and closes them when it opens', () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ statuses: {}, messages: [] }) }))
    mockUseBoardSessionToken.mockReturnValue({ token: 'tok', commandCenterEnabled: true, agentBoardEnabled: true })
    render(<App />)

    fireEvent.keyDown(window, { key: 'S', ctrlKey: true, shiftKey: true })
    fireEvent.keyDown(window, { key: 'K', ctrlKey: true, shiftKey: true })
    fireEvent.keyDown(window, { key: 'B', ctrlKey: true, shiftKey: true })
    expect(screen.queryByRole('dialog', { name: 'Command center' })).toBeNull()
    expect(screen.queryByRole('dialog', { name: 'Agent board' })).toBeNull()

    fireEvent.keyDown(window, { key: 'S', ctrlKey: true, shiftKey: true })
    fireEvent.keyDown(window, { key: 'K', ctrlKey: true, shiftKey: true })
    fireEvent.click(screen.getByRole('button', { name: 'Open agent board' }))
    fireEvent.click(screen.getByRole('button', { name: 'Open command center history' }))
    expect(screen.getByRole('dialog', { name: 'Command center' })).toBeInTheDocument()

    fireEvent.keyDown(window, { key: 'S', ctrlKey: true, shiftKey: true })
    expect(screen.getByRole('region', { name: 'Task dashboard' })).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: 'Command center' })).toBeNull()
    expect(screen.queryByRole('dialog', { name: 'Agent board' })).toBeNull()
    expect(screen.queryByRole('dialog', { name: 'Command center history' })).toBeNull()
    vi.unstubAllGlobals()
  })

  it('opens one pane when Open is pressed twice before the first one exists', async () => {
    let finish: () => void = () => {}
    mockCreatePane.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve }))
    mockUseTasks.mockReturnValue(tasksStateWith([waitingTask]))
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: /^Tasks/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Open: panemux' }))
    fireEvent.click(screen.getByRole('button', { name: /^Tasks/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Open: panemux' }))
    expect(mockCreatePane).toHaveBeenCalledTimes(1)

    await act(async () => finish())
    fireEvent.click(screen.getByRole('button', { name: /^Tasks/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Open: panemux' }))
    expect(mockCreatePane).toHaveBeenCalledTimes(2)
  })

  it('keeps the panes mounted while the dashboard is shown', () => {
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Tasks' }))
    expect(document.querySelector('[data-pane-id="main"]')).not.toBeNull()
  })

  it('counts waiting tasks on the back button', () => {
    mockUseTasks.mockReturnValue(tasksStateWith([waitingTask, { ...waitingTask, id: 'b', state: 'busy' }]))
    render(<App />)
    expect(screen.getByRole('button', { name: 'Tasks, 1 waiting for input when last checked' }))
      .toHaveTextContent('← Tasks1')
  })

  it('opens a task in a new tmux pane attached to its session', async () => {
    mockUseTasks.mockReturnValue(tasksStateWith([waitingTask]))
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: /^Tasks/ }))

    fireEvent.click(screen.getByRole('button', { name: 'Open: panemux' }))

    await waitFor(() => expect(mockCreatePane).toHaveBeenCalledTimes(1))
    const [pane, placement] = mockCreatePane.mock.calls[0]
    expect(pane).toEqual(expect.objectContaining({ type: 'tmux', tmux_session: 'task-a', title: 'task-a' }))
    expect(placement).toEqual({ type: 'workspace-edge', edge: 'right' })
    expect(screen.queryByRole('region', { name: 'Task dashboard' })).not.toBeInTheDocument()
  })

  it('opens a remote task in an ssh_tmux pane on its connection', async () => {
    mockUseTasks.mockReturnValue(tasksStateWith([{ ...waitingTask, host: 'dev-server' }]))
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: /^Tasks/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Open: panemux' }))

    await waitFor(() => expect(mockCreatePane).toHaveBeenCalledTimes(1))
    expect(mockCreatePane.mock.calls[0][0]).toEqual(
      expect.objectContaining({ type: 'ssh_tmux', connection: 'dev-server', tmux_session: 'task-a' }),
    )
  })

  it('reports a pane that could not be created', async () => {
    mockUseTasks.mockReturnValue(tasksStateWith([waitingTask]))
    mockCreatePane.mockRejectedValueOnce(new Error('HTTP 500'))
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: /^Tasks/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Open: panemux' }))

    expect(await screen.findByText('Failed to create terminal: HTTP 500')).toBeInTheDocument()
  })

  it('goes to the workspace of a pane already attached to the task', async () => {
    currentWorkspaces = {
      ...workspaces,
      items: [
        workspaces.items[0],
        {
          id: 'ops',
          title: 'Ops',
          layout: {
            direction: 'vertical',
            children: [{ size: 100, pane: { id: 'ops-tmux', type: 'tmux', tmux_session: 'task-a', title: 'agent' } }],
          },
        },
      ],
    }
    mockUseTasks.mockReturnValue(tasksStateWith([waitingTask]))
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: /^Tasks/ }))

    fireEvent.click(screen.getByRole('button', { name: 'Go to pane: panemux' }))

    expect(mockSetActiveWorkspace).toHaveBeenCalledWith('ops')
    expect(mockCreatePane).not.toHaveBeenCalled()
    expect(screen.queryByRole('region', { name: 'Task dashboard' })).not.toBeInTheDocument()
  })
})
