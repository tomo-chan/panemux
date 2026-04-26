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

  it('uses vertical orientation for left and right positions', () => {
    const { rerender } = render(
      <WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="left" onSelect={() => {}} />,
    )
    expect(screen.getByRole('tablist')).toHaveAttribute('aria-orientation', 'vertical')

    rerender(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="bottom" onSelect={() => {}} />)
    expect(screen.getByRole('tablist')).toHaveAttribute('aria-orientation', 'horizontal')
  })
})
