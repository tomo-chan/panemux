import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { WorkspaceOverviewDashboard } from './WorkspaceOverviewDashboard'

const workspaces = [
  {
    id: 'dev',
    title: 'Dev',
    layout: {
      direction: 'horizontal' as const,
      children: [
        { size: 50, pane: { id: 'main', type: 'local' as const, title: 'Main' } },
        { size: 50, pane: { id: 'side', type: 'ssh' as const, title: 'Side' } },
      ],
    },
  },
  {
    id: 'ops',
    title: 'Ops',
    layout: {
      direction: 'vertical' as const,
      children: [{ size: 100, pane: { id: 'ops-main', type: 'tmux' as const, title: 'Ops Main' } }],
    },
  },
]

describe('WorkspaceOverviewDashboard', () => {
  it('renders every workspace and pane with counts', () => {
    render(
      <WorkspaceOverviewDashboard
        workspaces={workspaces}
        activeWorkspaceId="dev"
        attentionPaneIds={new Set(['side'])}
        attentionWorkspaceIds={new Set(['dev'])}
        sessionsById={{
          main: { id: 'main', type: 'local', title: 'Main', state: 'connected' },
          side: { id: 'side', type: 'ssh', title: 'Side', state: 'disconnected' },
          'ops-main': { id: 'ops-main', type: 'tmux', title: 'Ops Main', state: 'exited' },
        }}
        gitInfoById={{
          main: { is_git: true, repo: 'panemux', branch: 'main', pr_number: 12, pr_url: 'https://example.com/pr/12' },
          side: { is_git: false },
          'ops-main': { is_git: false },
        }}
        onSelectWorkspace={() => {}}
        onSelectPane={() => {}}
      />,
    )

    expect(screen.getByText('Workspace Overview')).toBeInTheDocument()
    expect(screen.getByText('Dev')).toBeInTheDocument()
    expect(screen.getByText('Ops')).toBeInTheDocument()
    expect(screen.getByText('2 panes')).toBeInTheDocument()
    expect(screen.getByText('1 exited')).toBeInTheDocument()
    expect(screen.getByText('PR #12')).toBeInTheDocument()
    expect(screen.getByText('Needs input')).toBeInTheDocument()
  })

  it('routes workspace and pane clicks through the provided callbacks', () => {
    const onSelectWorkspace = vi.fn()
    const onSelectPane = vi.fn()

    render(
      <WorkspaceOverviewDashboard
        workspaces={workspaces}
        activeWorkspaceId="dev"
        attentionPaneIds={new Set()}
        attentionWorkspaceIds={new Set()}
        sessionsById={{}}
        gitInfoById={{}}
        onSelectWorkspace={onSelectWorkspace}
        onSelectPane={onSelectPane}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Open workspace Dev' }))
    fireEvent.click(screen.getByRole('button', { name: 'Open pane Main in Dev' }))

    expect(onSelectWorkspace).toHaveBeenCalledWith('dev')
    expect(onSelectPane).toHaveBeenCalledWith('dev', 'main')
  })
})
