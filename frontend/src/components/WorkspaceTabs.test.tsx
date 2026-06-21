import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { WorkspaceTabs } from './WorkspaceTabs'
import type { Workspace } from '../types'
import type { WorkspaceSummary } from './WorkspaceTabs'

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

const workspaceSummaries: Record<string, WorkspaceSummary> = {
  dev: {
    paneCount: 2,
    connectedCount: 1,
    disconnectedCount: 1,
    exitedCount: 0,
    pendingCount: 0,
    panes: [
      { id: 'main', title: 'Main', type: 'local', state: 'connected', repo: 'panemux', branch: 'main', prNumber: 12, attention: false },
      { id: 'side', title: 'Side', type: 'ssh', state: 'disconnected', connection: 'prod', attention: true },
    ],
  },
  ops: {
    paneCount: 1,
    connectedCount: 0,
    disconnectedCount: 0,
    exitedCount: 1,
    pendingCount: 0,
    panes: [
      { id: 'ops', title: 'Ops Main', type: 'ssh_tmux', state: 'exited', connection: 'bastion', attention: false },
    ],
  },
}

describe('WorkspaceTabs', () => {
  it('renders workspace tabs and calls onSelect', () => {
    const onSelect = vi.fn()
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={onSelect} />)

    expect(screen.getByRole('tab', { name: 'Dev' })).toHaveAttribute('aria-selected', 'true')
    fireEvent.click(screen.getByRole('tab', { name: 'Ops' }))
    expect(onSelect).toHaveBeenCalledWith('ops')
  })

  it('renders compact workspace summaries in the tabs when provided', () => {
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        workspaceSummaries={workspaceSummaries}
        onSelect={() => {}}
      />,
    )

    expect(screen.getByText('2 panes · 1 up · 1 down')).toBeInTheDocument()
    expect(screen.getByText('Main · panemux · main')).toBeInTheDocument()
    expect(screen.getByText('Side · prod')).toBeInTheDocument()
    expect(screen.getByText('Ops Main · bastion')).toBeInTheDocument()
  })

  it('keeps every workspace details section open by default', () => {
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        workspaceSummaries={workspaceSummaries}
        onSelect={() => {}}
      />,
    )

    expect(screen.getByRole('region', { name: 'Dev workspace details' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Ops workspace details' })).toBeInTheDocument()
    expect(screen.getByText('Main')).toBeInTheDocument()
    expect(screen.getByText('PR #12')).toBeInTheDocument()
    expect(screen.getByText('prod')).toBeInTheDocument()
    expect(screen.getByText('bastion')).toBeInTheDocument()
    expect(screen.getByText('Input')).toBeInTheDocument()
  })

  it('routes pane selection from the integrated workspace details panel', () => {
    const onSelectPaneFromSummary = vi.fn()
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        workspaceSummaries={workspaceSummaries}
        onSelect={() => {}}
        onSelectPaneFromSummary={onSelectPaneFromSummary}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Open pane Side in Dev' }))

    expect(onSelectPaneFromSummary).toHaveBeenCalledWith('dev', 'side')
  })

  it('starts dragging from a workspace pane summary card', () => {
    const onStartPaneDragFromSummary = vi.fn()
    const dataTransfer = {
      effectAllowed: 'none',
      setData: vi.fn(),
    }

    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        workspaceSummaries={workspaceSummaries}
        onSelect={() => {}}
        onStartPaneDragFromSummary={onStartPaneDragFromSummary}
      />,
    )

    fireEvent.dragStart(screen.getByRole('button', { name: 'Open pane Side in Dev' }), { dataTransfer })

    expect(dataTransfer.effectAllowed).toBe('move')
    expect(dataTransfer.setData).toHaveBeenCalledWith('text/plain', 'side')
    expect(onStartPaneDragFromSummary).toHaveBeenCalledWith('side')
  })

  it('highlights the active pane summary card', () => {
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        activePaneId="side"
        tabPosition="top"
        workspaceSummaries={workspaceSummaries}
        onSelect={() => {}}
      />,
    )

    expect(screen.getByRole('button', { name: 'Open pane Side in Dev' })).toHaveStyle({
      background: 'rgba(86, 156, 214, 0.16)',
    })
  })

  it('shows hover affordance on workspace tabs', () => {
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} />)

    const tab = screen.getByRole('tab', { name: 'Ops' })
    fireEvent.mouseEnter(tab)

    expect(tab).toHaveStyle({
      backgroundColor: 'rgba(255, 255, 255, 0.07)',
      boxShadow: 'inset 0 0 0 1px rgba(255, 255, 255, 0.06)',
    })
  })

  it('keeps hover affordance after a component re-render', () => {
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onRename={() => {}} />)

    const tab = screen.getByRole('tab', { name: 'Ops' })
    fireEvent.mouseEnter(tab)
    fireEvent.click(screen.getByRole('button', { name: 'Rename Dev workspace' }))

    expect(tab).toHaveStyle({
      backgroundColor: 'rgba(255, 255, 255, 0.07)',
      boxShadow: 'inset 0 0 0 1px rgba(255, 255, 255, 0.06)',
    })
  })

  it('shows a pressed state on workspace add button while held down', () => {
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onAdd={() => {}} />)

    const button = screen.getByRole('button', { name: 'Add workspace' })
    fireEvent.mouseDown(button, { button: 0 })

    expect(button).toHaveStyle({
      backgroundColor: 'rgba(255, 255, 255, 0.12)',
      boxShadow: 'inset 0 1px 2px rgba(0, 0, 0, 0.45)',
      transform: 'translateY(1px)',
    })
  })

  it('marks inactive workspaces that need attention', () => {
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        attentionWorkspaceIds={new Set(['ops'])}
        onSelect={() => {}}
      />,
    )

    expect(screen.getByRole('tab', { name: /^Ops\b/ })).toHaveAttribute('data-attention', 'true')
    expect(screen.getByRole('tab', { name: /^Dev\b/ })).not.toHaveAttribute('data-attention')
  })

  it('clears workspace attention before selecting the tab', () => {
    const onSelect = vi.fn()
    const onClearAttention = vi.fn()
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        attentionWorkspaceIds={new Set(['ops'])}
        onSelect={onSelect}
        onClearAttention={onClearAttention}
      />,
    )

    fireEvent.click(screen.getByRole('tab', { name: /^Ops\b/ }))

    expect(onClearAttention).toHaveBeenCalledWith('ops')
    expect(onSelect).toHaveBeenCalledWith('ops')
  })

  it('moves a dragged pane into another workspace when the tab receives a drop', () => {
    const onMovePaneToWorkspace = vi.fn()
    const dataTransfer = { dropEffect: 'none' }
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        dragSourcePaneId="pane-source"
        onMovePaneToWorkspace={onMovePaneToWorkspace}
        onSelect={() => {}}
      />,
    )

    const tab = screen.getByRole('tab', { name: /^Ops\b/ })
    fireEvent.dragOver(tab, { dataTransfer })
    fireEvent.drop(tab, { dataTransfer })

    expect(onMovePaneToWorkspace).toHaveBeenCalledWith('pane-source', 'ops')
  })

  it('moves a dragged pane into another workspace when the workspace details receive a drop', () => {
    const onMovePaneToWorkspace = vi.fn()
    const dataTransfer = { dropEffect: 'none' }
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        dragSourcePaneId="pane-source"
        workspaceSummaries={workspaceSummaries}
        onMovePaneToWorkspace={onMovePaneToWorkspace}
        onSelect={() => {}}
      />,
    )

    const details = screen.getByRole('region', { name: 'Ops workspace details' })
    fireEvent.dragOver(details, { dataTransfer })
    fireEvent.drop(details, { dataTransfer })

    expect(onMovePaneToWorkspace).toHaveBeenCalledWith('pane-source', 'ops')
  })

  it('moves a dragged pane into another workspace on pointer drag release', () => {
    const onMovePaneToWorkspace = vi.fn()
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        dragSourcePaneId="pane-source"
        onMovePaneToWorkspace={onMovePaneToWorkspace}
        onSelect={() => {}}
      />,
    )

    const tab = screen.getByRole('tab', { name: /^Ops\b/ })
    fireEvent.mouseEnter(tab)
    fireEvent.mouseUp(tab)

    expect(onMovePaneToWorkspace).toHaveBeenCalledWith('pane-source', 'ops')
  })

  it('moves a dragged pane into another workspace when released over a pane card', () => {
    const onMovePaneToWorkspace = vi.fn()
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        dragSourcePaneId="pane-source"
        workspaceSummaries={workspaceSummaries}
        onMovePaneToWorkspace={onMovePaneToWorkspace}
        onSelect={() => {}}
      />,
    )

    const paneCard = screen.getByRole('button', { name: 'Open pane Ops Main in Ops' })
    fireEvent.mouseEnter(paneCard)
    fireEvent.mouseUp(paneCard)

    expect(onMovePaneToWorkspace).toHaveBeenCalledWith('pane-source', 'ops')
  })

  it('clears the workspace drop highlight when dragging is cancelled externally', () => {
    const { rerender } = render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        dragSourcePaneId="pane-source"
        onMovePaneToWorkspace={() => {}}
        onSelect={() => {}}
      />,
    )

    const tab = screen.getByRole('tab', { name: /^Ops\b/ }).parentElement
    expect(tab).not.toBeNull()
    fireEvent.mouseEnter(tab!)
    expect(tab).toHaveStyle({
      backgroundColor: 'rgba(86, 156, 214, 0.24)',
      boxShadow: 'inset 0 0 0 1px rgba(137, 196, 244, 0.45)',
    })

    rerender(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        dragSourcePaneId={null}
        onMovePaneToWorkspace={() => {}}
        onSelect={() => {}}
      />,
    )

    const rerenderedTab = screen.getByRole('tab', { name: /^Ops\b/ }).parentElement
    expect(rerenderedTab).not.toBeNull()
    expect(rerenderedTab).not.toHaveStyle({
      backgroundColor: 'rgba(86, 156, 214, 0.24)',
      boxShadow: 'inset 0 0 0 1px rgba(137, 196, 244, 0.45)',
    })
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

    expect(screen.getByRole('tab', { name: /^Dev\b/ })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Add workspace' }))
    expect(onAdd).toHaveBeenCalled()
  })

  it('renders the add workspace control with the same tab height', () => {
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onAdd={() => {}} />)

    expect(screen.getByRole('button', { name: 'Add workspace' }).parentElement).toHaveStyle({ minWidth: '40px' })
  })

  it('does not render a workspace-bar add terminal button', () => {
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onAdd={() => {}} />)

    expect(screen.queryByRole('button', { name: 'Add terminal' })).not.toBeInTheDocument()
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
    expect(screen.getByTestId('workspace-tab-position-cluster').children).toHaveLength(4)
    expect(screen.getByTestId('workspace-tab-position-cluster')).toHaveStyle({ display: 'flex', flexDirection: 'row' })
    expect(screen.getByRole('button', { name: 'Place workspace tabs at top' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(screen.getByRole('button', { name: 'Place workspace tabs on left' }))
    expect(onTabPositionChange).toHaveBeenCalledWith('left')
  })

  it('places the tab position controls after the tablist and action group for horizontal bars', () => {
    const { container } = render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="top"
        onSelect={() => {}}
        onAdd={() => {}}
        onTabPositionChange={() => {}}
      />,
    )

    const bar = container.firstElementChild
    expect(bar?.children.item(0)).toHaveAttribute('role', 'tablist')
    expect(bar?.children.item(1)?.textContent).toContain('+')
    expect(bar?.children.item(2)).toHaveAttribute('role', 'group')
  })

  it('places the tab position controls at the opposite end for vertical bars', () => {
    render(
      <WorkspaceTabs
        workspaces={workspaces}
        activeWorkspaceId="dev"
        tabPosition="left"
        onSelect={() => {}}
        onTabPositionChange={() => {}}
      />,
    )

    expect(screen.getByRole('group', { name: 'Workspace tab position' })).toHaveStyle({ marginTop: 'auto' })
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

    fireEvent.doubleClick(screen.getByRole('tab', { name: /^Dev\b/ }))
    const input = screen.getByLabelText('Workspace name')
    fireEvent.change(input, { target: { value: 'Development' } })
    fireEvent.keyDown(input, { key: 'Escape' })

    expect(onRename).not.toHaveBeenCalled()
    expect(screen.getByRole('tab', { name: /^Dev\b/ })).toBeInTheDocument()
  })

  it('does not save when Escape is followed by blur', () => {
    const onRename = vi.fn()
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onRename={onRename} />)

    fireEvent.doubleClick(screen.getByRole('tab', { name: /^Dev\b/ }))
    const input = screen.getByLabelText('Workspace name')
    fireEvent.change(input, { target: { value: 'Development' } })
    fireEvent.keyDown(input, { key: 'Escape' })
    fireEvent.blur(input)

    expect(onRename).not.toHaveBeenCalled()
    expect(screen.getByRole('tab', { name: /^Dev\b/ })).toBeInTheDocument()
  })

  it('does not save a blank inline rename', () => {
    const onRename = vi.fn()
    render(<WorkspaceTabs workspaces={workspaces} activeWorkspaceId="dev" tabPosition="top" onSelect={() => {}} onRename={onRename} />)

    fireEvent.click(screen.getByRole('button', { name: 'Rename Dev workspace' }))
    const input = screen.getByLabelText('Workspace name')
    fireEvent.change(input, { target: { value: '   ' } })
    fireEvent.blur(input)

    expect(onRename).not.toHaveBeenCalled()
    expect(screen.getByRole('tab', { name: /^Dev\b/ })).toBeInTheDocument()
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
