import { useContext, useEffect } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
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
const mockUseWorkspaceAttentionMonitor = vi.hoisted(() => vi.fn())
const mockUseBrowserNotificationPermission = vi.hoisted(() => vi.fn())

vi.mock('./hooks/useLayout', () => ({
  useLayout: () => ({
    layout,
    workspaces: currentWorkspaces,
    displayConfig: { show_header: false, show_status_bar: false },
    error: null,
    updateSizes: vi.fn(),
    splitPane: vi.fn(),
    closePane: vi.fn(),
    swapPanes: vi.fn(),
    setActiveWorkspace: mockSetActiveWorkspace,
    addWorkspace: vi.fn(),
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

vi.mock('./hooks/useEditMode', () => ({
  useEditMode: () => ({ editMode: true, toggleEditMode: vi.fn() }),
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
  }),
}))

describe('App workspace deletion', () => {
  let originalNotification: typeof Notification | undefined
  let notificationInstance: { onclick: (() => void) | null; close: ReturnType<typeof vi.fn> } | null

  beforeEach(() => {
    originalNotification = window.Notification
    mockUseWorkspaceAttentionMonitor.mockImplementation(() => {})
    mockUseBrowserNotificationPermission.mockImplementation(() => {})
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
    mockTerminalPane.mockClear()
    mockUseWorkspaceAttentionMonitor.mockReset()
    mockUseBrowserNotificationPermission.mockReset()
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

    expect(screen.getByRole('tab', { name: 'Dev' })).toHaveAttribute('data-attention', 'true')
    expect(screen.getByText('Notify main').closest('[data-pane-id="main"]')).toHaveAttribute('data-attention', 'true')
    expect(screen.getByText('Notify side').closest('[data-pane-id="side"]')).toHaveAttribute('data-attention', 'true')

    fireEvent.click(screen.getByRole('button', { name: 'Clear main' }))

    expect(screen.getByRole('tab', { name: 'Dev' })).toHaveAttribute('data-attention', 'true')
    expect(screen.getByText('Notify main').closest('[data-pane-id="main"]')).not.toHaveAttribute('data-attention')
    expect(screen.getByText('Notify side').closest('[data-pane-id="side"]')).toHaveAttribute('data-attention', 'true')

    fireEvent.click(screen.getByRole('button', { name: 'Clear side' }))

    expect(screen.getByRole('tab', { name: 'Dev' })).not.toHaveAttribute('data-attention')
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

  it('marks an inactive workspace when the attention monitor reports one of its panes', () => {
    currentWorkspaces = { ...workspaces, active: 'ops' }
    mockUseWorkspaceAttentionMonitor.mockImplementation(({ onAttention }: { onAttention: (paneId: string) => void }) => {
      useEffect(() => {
        onAttention('main')
      }, [onAttention])
    })

    render(<App />)

    expect(screen.getByRole('tab', { name: 'Dev' })).toHaveAttribute('data-attention', 'true')
    expect(screen.getByRole('tab', { name: 'Ops' })).not.toHaveAttribute('data-attention')
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

    expect(screen.getByRole('tab', { name: 'Dev' })).not.toHaveAttribute('data-attention')
    expect(screen.getByRole('tab', { name: 'Ops' })).not.toHaveAttribute('data-attention')
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
})
