import { useContext, useEffect } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { LayoutActionsContext } from './components/SplitContainer'
import type { LayoutNode, WorkspacesResponse } from './schemas'

const mockTerminalPane = vi.hoisted(() => vi.fn(({ pane }: { pane: { id: string } }) => <div data-pane-id={pane.id} />))

vi.mock('./components/TerminalPane', () => ({
  TerminalPane: mockTerminalPane,
}))

const layout: LayoutNode = {
  direction: 'horizontal',
  children: [
    { size: 50, pane: { id: 'main', type: 'local' } },
    { size: 50, pane: { id: 'side', type: 'local' } },
  ],
}

const workspaces: WorkspacesResponse = {
  active: 'dev',
  tab_position: 'top',
  items: [
    { id: 'dev', title: 'Dev', layout },
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
const mockUseGitInfoMap = vi.hoisted(() => vi.fn())

vi.mock('./hooks/useLayout', () => ({
  useLayout: () => ({
    layout,
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

vi.mock('./hooks/useGitInfoMap', () => ({
  useGitInfoMap: mockUseGitInfoMap,
}))

vi.mock('./hooks/usePaneSettings', () => ({
  usePaneSettings: () => ({
    isOpen: false,
    currentPane: null,
    sshConnectionNames: [],
    saveError: null,
    isSaving: false,
    openSettings: vi.fn(),
    closeSettings: vi.fn(),
    saveSettings: vi.fn(),
    addSSHConfigHost: vi.fn(),
    detectShell: vi.fn(),
    browseDirectories: vi.fn(),
  }),
}))

describe('App workspace deletion', () => {
  let originalNotification: typeof Notification | undefined
  let notificationInstance: { onclick: (() => void) | null; close: ReturnType<typeof vi.fn> } | null

  beforeEach(() => {
    originalNotification = window.Notification
    mockUseWorkspaceAttentionMonitor.mockImplementation(() => {})
    mockUseBrowserNotificationPermission.mockImplementation(() => {})
    mockUseSessionsOverview.mockReturnValue({})
    mockUseGitInfoMap.mockReturnValue({})
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
    mockUseGitInfoMap.mockReset()
    currentWorkspaces = workspaces
    vi.restoreAllMocks()
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
    mockUseGitInfoMap.mockReturnValue({
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
          <button onClick={() => ctx?.onPaneAttention(pane.id)}>Notify {pane.id}</button>
          <button onClick={() => ctx?.clearPaneAttention(pane.id)}>Clear {pane.id}</button>
        </div>
      )
    })

    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Notify main' }))
    fireEvent.click(screen.getByRole('button', { name: 'Notify side' }))

    expect(screen.getByRole('tab', { name: /^Dev\b/ })).toHaveAttribute('data-attention', 'true')
    expect(screen.getByText('Notify main').closest('[data-pane-id="main"]')).toHaveAttribute('data-attention', 'true')
    expect(screen.getByText('Notify side').closest('[data-pane-id="side"]')).toHaveAttribute('data-attention', 'true')

    fireEvent.click(screen.getByRole('button', { name: 'Clear main' }))

    expect(screen.getByRole('tab', { name: /^Dev\b/ })).toHaveAttribute('data-attention', 'true')
    expect(screen.getByText('Notify main').closest('[data-pane-id="main"]')).not.toHaveAttribute('data-attention')
    expect(screen.getByText('Notify side').closest('[data-pane-id="side"]')).toHaveAttribute('data-attention', 'true')

    fireEvent.click(screen.getByRole('button', { name: 'Clear side' }))

    expect(screen.getByRole('tab', { name: /^Dev\b/ })).not.toHaveAttribute('data-attention')
  })

  it('confirms before deleting a workspace from edit-mode tabs', () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)

    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Delete Dev workspace' }))

    expect(window.confirm).toHaveBeenCalledWith('Delete workspace "Dev"?')
    expect(mockDeleteWorkspace).toHaveBeenCalledWith('dev')
  })

  it('keeps the workspace when delete confirmation is cancelled', () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)

    render(<App />)
    fireEvent.click(screen.getByRole('button', { name: 'Delete Dev workspace' }))

    expect(mockDeleteWorkspace).not.toHaveBeenCalled()
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
})
