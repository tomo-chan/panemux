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
