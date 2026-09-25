import { useContext, useEffect } from 'react'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { LayoutActionsContext } from './components/SplitContainer'
import type { LayoutNode, WorkspacesResponse } from './schemas'

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

let currentWorkspaces = workspaces

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
const mockUseWorkspaceAttentionMonitor = vi.hoisted(() => vi.fn())
const mockUseBrowserNotificationPermission = vi.hoisted(() => vi.fn())
const mockUseSessionsOverview = vi.hoisted(() => vi.fn())
const mockUseGitInfoSnapshotMap = vi.hoisted(() => vi.fn())

vi.mock('./hooks/useLayout', () => ({
  useLayout: () => ({
    layout: currentWorkspaces.items.find((workspace) => workspace.id === currentWorkspaces.active)?.layout ?? currentWorkspaces.items[0].layout,
    workspaces: currentWorkspaces,
    displayConfig: { show_header: false, show_status_bar: false },
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

vi.mock('./hooks/useWorkspaceAttentionMonitor', () => ({
  useWorkspaceAttentionMonitor: mockUseWorkspaceAttentionMonitor,
}))

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
})

describe('App workspace deletion', () => {
  let originalNotification: typeof Notification | undefined
  let notificationInstance: { onclick: (() => void) | null; close: ReturnType<typeof vi.fn> } | null

  beforeEach(() => {
    originalNotification = window.Notification
    mockUseWorkspaceAttentionMonitor.mockImplementation(() => {})
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
    mockUseWorkspaceAttentionMonitor.mockReset()
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

  it('keeps workspace attention while another pane in that workspace still needs attention', () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    mockTerminalPane.mockImplementation(({ pane }: { pane: { id: string } }) => {
      const ctx = useContext(LayoutActionsContext)
      return (
        <div data-pane-id={pane.id} data-attention={ctx?.hasPaneAttention(pane.id) ? 'true' : undefined}>
          <button onClick={() => ctx?.onPaneAttention('main')}>Notify main</button>
          <button onClick={() => ctx?.onPaneAttention('side')}>Notify side</button>
          <button onClick={() => ctx?.clearPaneAttention('main')}>Clear main</button>
          <button onClick={() => ctx?.clearPaneAttention('side')}>Clear side</button>
        </div>
      )
    })

    const { rerender } = render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Notify main' }))
    fireEvent.click(screen.getByRole('button', { name: 'Notify side' }))

    expect(screen.getByRole('tab', { name: /^Dev\b/ })).toHaveAttribute('data-attention', 'true')

    currentWorkspaces = workspaces
    rerender(<App />)

    expect(document.querySelector('[data-pane-id="main"]')).toHaveAttribute('data-attention', 'true')
    expect(document.querySelector('[data-pane-id="side"]')).toHaveAttribute('data-attention', 'true')

    // Both visible panes render the same mock controls, so target the first one.
    fireEvent.click(screen.getAllByRole('button', { name: 'Clear main' })[0])

    expect(document.querySelector('[data-pane-id="main"]')).not.toHaveAttribute('data-attention')
    expect(document.querySelector('[data-pane-id="side"]')).toHaveAttribute('data-attention', 'true')

    fireEvent.click(screen.getAllByRole('button', { name: 'Clear side' })[0])

    expect(document.querySelector('[data-pane-id="side"]')).not.toHaveAttribute('data-attention')
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

  it('marks an inactive workspace when the attention monitor reports one of its panes', () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    mockUseWorkspaceAttentionMonitor.mockImplementation(({ onAttention }: { onAttention: (paneId: string) => void }) => {
      useEffect(() => {
        onAttention('main')
      }, [onAttention])
    })

    render(<App />)

    expect(screen.getByRole('tab', { name: /^Dev\b/ })).toHaveAttribute('data-attention', 'true')
    expect(screen.getByRole('tab', { name: /^Ops\b/ })).not.toHaveAttribute('data-attention')
  })

  it('shows a browser notification with pane and workspace titles for inactive workspace attention', () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    mockUseWorkspaceAttentionMonitor.mockImplementation(({ onAttention }: { onAttention: (paneId: string) => void }) => {
      useEffect(() => {
        onAttention('main')
      }, [onAttention])
    })

    render(<App />)

    expect(window.Notification).toHaveBeenCalledWith('Agent confirmation requested', {
      body: 'main in Dev',
    })
  })

  it('switches to the relevant workspace when the browser notification is clicked', () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    const focusSpy = vi.spyOn(window, 'focus').mockImplementation(() => {})
    mockSetActiveWorkspace.mockResolvedValue(undefined)

    mockUseWorkspaceAttentionMonitor.mockImplementation(({ onAttention }: { onAttention: (paneId: string) => void }) => {
      useEffect(() => {
        onAttention('main')
      }, [onAttention])
    })

    render(<App />)
    notificationInstance?.onclick?.()

    expect(focusSpy).toHaveBeenCalled()
    expect(mockSetActiveWorkspace).toHaveBeenCalledWith('dev')
    expect(notificationInstance?.close).toHaveBeenCalled()
  })

  it('logs workspace switch failures from notification clicks', async () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    const switchError = new Error('switch failed')
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.spyOn(window, 'focus').mockImplementation(() => {})
    mockSetActiveWorkspace.mockRejectedValueOnce(switchError)

    mockUseWorkspaceAttentionMonitor.mockImplementation(({ onAttention }: { onAttention: (paneId: string) => void }) => {
      useEffect(() => {
        onAttention('main')
      }, [onAttention])
    })

    render(<App />)
    notificationInstance?.onclick?.()
    await Promise.resolve()

    expect(consoleErrorSpy).toHaveBeenCalledWith(switchError)
  })

  it('does not mark the active workspace tab when attention comes from the active workspace', () => {
    currentWorkspaces = { ...workspaces, active: 'dev' }
    mockUseWorkspaceAttentionMonitor.mockImplementation(({ onAttention }: { onAttention: (paneId: string) => void }) => {
      useEffect(() => {
        onAttention('main')
      }, [onAttention])
    })

    render(<App />)

    expect(screen.getByRole('tab', { name: /^Dev\b/ })).not.toHaveAttribute('data-attention')
    expect(screen.getByRole('tab', { name: /^Ops\b/ })).not.toHaveAttribute('data-attention')
  })

  it('does not request browser notification permission when attention is reported', () => {
    Object.defineProperty(window.Notification, 'permission', {
      configurable: true,
      value: 'default',
    })
    mockUseWorkspaceAttentionMonitor.mockImplementation(({ onAttention }: { onAttention: (paneId: string) => void }) => {
      useEffect(() => {
        onAttention('main')
      }, [onAttention])
    })

    render(<App />)

    expect(window.Notification.requestPermission).not.toHaveBeenCalled()
    expect(window.Notification).not.toHaveBeenCalled()
  })

  it('does not show a browser notification when attention is already visible', () => {
    currentWorkspaces = { ...workspaces, active: 'dev' }
    mockUseWorkspaceAttentionMonitor.mockImplementation(({ onAttention }: { onAttention: (paneId: string, showBrowserNotification?: boolean) => void }) => {
      useEffect(() => {
        onAttention('main', false)
      }, [onAttention])
    })

    render(<App />)

    expect(window.Notification).not.toHaveBeenCalled()
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
    mockUseWorkspaceAttentionMonitor.mockImplementation(() => {})
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
    mockUseWorkspaceAttentionMonitor.mockReset()
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
    mockUseWorkspaceAttentionMonitor.mockImplementation(() => {})
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

  it('keeps the panes mounted while the dashboard is shown', () => {
    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Tasks' }))
    expect(document.querySelector('[data-pane-id="main"]')).not.toBeNull()
  })

  it('counts waiting tasks on the back button', () => {
    mockUseTasks.mockReturnValue(tasksStateWith([waitingTask, { ...waitingTask, id: 'b', state: 'busy' }]))
    render(<App />)
    expect(screen.getByRole('button', { name: 'Tasks, 1 waiting for input' })).toHaveTextContent('← Tasks1')
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
