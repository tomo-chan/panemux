import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { WorkspaceTabs } from './WorkspaceTabs'
import type { Workspace } from '../types'

const workspaces: Workspace[] = [
  {
    id: 'dev',
    title: 'Dev',
    layout: { direction: 'horizontal', children: [{ size: 100, pane: { id: 'main', type: 'local' } }] },
  },
  {
    id: 'ops',
    title: 'Ops',
    layout: { direction: 'vertical', children: [{ size: 100, pane: { id: 'ops', type: 'local' } }] },
  },
]

describe('WorkspaceTabs', () => {
  it('renders workspace tabs and calls onSelect', () => {
    const onSelect = vi.fn()
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={onSelect} />)

    expect(screen.getByRole('tab', { name: 'Dev' })).toHaveAttribute('aria-selected', 'true')
    fireEvent.click(screen.getByRole('tab', { name: 'Ops' }))
    expect(onSelect).toHaveBeenCalledWith('ops')
  })

  it('marks workspaces that contain pending agent confirmations', () => {
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        notifyingWorkspaceIds={new Set(['ops'])}
        onSelect={() => {}}
      />,
    )

    expect(screen.getByRole('tab', { name: 'Ops' })).toHaveAttribute('data-agent-confirmation', 'true')
    expect(screen.getByRole('tab', { name: 'Dev' })).not.toHaveAttribute('data-agent-confirmation')
  })

  it('hides workspace tabs for a single workspace while keeping add available', () => {
    const onAdd = vi.fn()
    render(
      <WorkspaceTabs
        workspaces={[workspaces[0]]}
        activeWorkspaceId="dev"
        tabPosition="top"
        onSelect={() => {}}
        onAdd={onAdd}
      />,
    )

    expect(screen.queryByRole('tab', { name: 'Dev' })).not.toBeInTheDocument()
    expect(screen.queryByRole('tablist')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Add workspace' }))
    expect(onAdd).toHaveBeenCalled()
  })

  it('renders add control with multiple workspace tabs', () => {
    const onAdd = vi.fn()
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onAdd={onAdd} />)

    expect(screen.getByRole('tab', { name: 'Dev' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Add workspace' }))
    expect(onAdd).toHaveBeenCalled()
  })

  it('renders tab position controls when a position change handler is provided', () => {
    const onTabPositionChange = vi.fn()
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        onSelect={() => {}}
        onTabPositionChange={onTabPositionChange}
      />,
    )

    expect(screen.getByRole('group', { name: 'Workspace tab position' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Place workspace tabs at top' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(screen.getByRole('button', { name: 'Place workspace tabs on left' }))
    expect(onTabPositionChange).toHaveBeenCalledWith('left')
  })

  it('hides tab position controls when the position change handler is omitted', () => {
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} />)

    expect(screen.queryByRole('group', { name: 'Workspace tab position' })).not.toBeInTheDocument()
  })

  it('renders delete controls when delete handler is provided', () => {
    const onDelete = vi.fn()
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onDelete={onDelete} />)

    fireEvent.click(screen.getByRole('button', { name: 'Delete Dev workspace' }))
    expect(onDelete).toHaveBeenCalledWith('dev')
  })

  it('hides delete controls when delete handler is omitted', () => {
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} />)

    expect(screen.queryByRole('button', { name: 'Delete Dev workspace' })).not.toBeInTheDocument()
  })

  it('renames a workspace inline with Enter', () => {
    const onRename = vi.fn()
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onRename={onRename} />)

    fireEvent.click(screen.getByRole('button', { name: 'Rename Dev workspace' }))
    const input = screen.getByLabelText('Workspace name')
    fireEvent.change(input, { target: { value: 'Development' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(onRename).toHaveBeenCalledWith('dev', 'Development')
  })

  it('saves a valid inline rename on blur', () => {
    const onRename = vi.fn()
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onRename={onRename} />)

    fireEvent.click(screen.getByRole('button', { name: 'Rename Dev workspace' }))
    const input = screen.getByLabelText('Workspace name')
    fireEvent.change(input, { target: { value: 'Development' } })
    fireEvent.blur(input)

    expect(onRename).toHaveBeenCalledTimes(1)
    expect(onRename).toHaveBeenCalledWith('dev', 'Development')
  })

  it('does not save twice when Enter is followed by blur', () => {
    const onRename = vi.fn()
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onRename={onRename} />)

    fireEvent.click(screen.getByRole('button', { name: 'Rename Dev workspace' }))
    const input = screen.getByLabelText('Workspace name')
    fireEvent.change(input, { target: { value: 'Development' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    fireEvent.blur(input)

    expect(onRename).toHaveBeenCalledTimes(1)
    expect(onRename).toHaveBeenCalledWith('dev', 'Development')
  })

  it('cancels inline rename with Escape', () => {
    const onRename = vi.fn()
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onRename={onRename} />)

    fireEvent.doubleClick(screen.getByRole('tab', { name: 'Dev' }))
    const input = screen.getByLabelText('Workspace name')
    fireEvent.change(input, { target: { value: 'Development' } })
    fireEvent.keyDown(input, { key: 'Escape' })

    expect(onRename).not.toHaveBeenCalled()
    expect(screen.getByRole('tab', { name: 'Dev' })).toBeInTheDocument()
  })

  it('does not save when Escape is followed by blur', () => {
    const onRename = vi.fn()
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onRename={onRename} />)

    fireEvent.doubleClick(screen.getByRole('tab', { name: 'Dev' }))
    const input = screen.getByLabelText('Workspace name')
    fireEvent.change(input, { target: { value: 'Development' } })
    fireEvent.keyDown(input, { key: 'Escape' })
    fireEvent.blur(input)

    expect(onRename).not.toHaveBeenCalled()
    expect(screen.getByRole('tab', { name: 'Dev' })).toBeInTheDocument()
  })

  it('does not save a blank inline rename', () => {
    const onRename = vi.fn()
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onRename={onRename} />)

    fireEvent.click(screen.getByRole('button', { name: 'Rename Dev workspace' }))
    const input = screen.getByLabelText('Workspace name')
    fireEvent.change(input, { target: { value: '   ' } })
    fireEvent.blur(input)

    expect(onRename).not.toHaveBeenCalled()
    expect(screen.getByRole('tab', { name: 'Dev' })).toBeInTheDocument()
  })

  it('uses vertical orientation for left and right positions', () => {
    for (const tabPosition of ['left', 'right'] as const) {
      const { unmount } = render(
        <WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition={tabPosition} onSelect={() => {}} />,
      )
      expect(screen.getByRole('tablist')).toHaveAttribute('aria-orientation', 'vertical')
      unmount()
    }
  })

  it('uses horizontal orientation for top and bottom positions', () => {
    for (const tabPosition of ['top', 'bottom'] as const) {
      const { unmount } = render(
        <WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition={tabPosition} onSelect={() => {}} />,
      )
      expect(screen.getByRole('tablist')).toHaveAttribute('aria-orientation', 'horizontal')
      unmount()
    }
  })
})
