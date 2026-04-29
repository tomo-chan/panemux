import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import type { LayoutNode, WorkspacesResponse } from './schemas'

vi.mock('./components/TerminalPane', () => ({
  TerminalPane: ({ pane }: { pane: { id: string } }) => <div data-pane-id={pane.id} />,
}))

const layout: LayoutNode = {
  direction: 'horizontal',
  children: [{ size: 100, pane: { id: 'main', type: 'local' } }],
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

const mockDeleteWorkspace = vi.fn()
const mockRenameWorkspace = vi.fn()

vi.mock('./hooks/useLayout', () => ({
  useLayout: () => ({
    layout,
    workspaces,
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
    vi.restoreAllMocks()
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
})
