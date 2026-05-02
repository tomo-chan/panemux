import { useContext } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
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
    setActiveWorkspace: vi.fn(),
    addWorkspace: vi.fn(),
    deleteWorkspace: mockDeleteWorkspace,
    renameWorkspace: mockRenameWorkspace,
    setWorkspaceTabPosition: mockSetWorkspaceTabPosition,
  }),
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
  afterEach(() => {
    mockDeleteWorkspace.mockClear()
    mockRenameWorkspace.mockClear()
    mockSetWorkspaceTabPosition.mockClear()
    mockTerminalPane.mockClear()
    currentWorkspaces = workspaces
    vi.restoreAllMocks()
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
})
