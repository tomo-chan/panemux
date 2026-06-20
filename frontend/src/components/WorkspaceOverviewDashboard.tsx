import React from 'react'
import type { GitInfo, LayoutChild, SessionInfo, Workspace } from '../schemas'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface WorkspaceOverviewDashboardProps {
  workspaces: Workspace[]
  activeWorkspaceId: string
  attentionPaneIds: ReadonlySet<string>
  attentionWorkspaceIds: ReadonlySet<string>
  sessionsById: Record<string, SessionInfo>
  gitInfoById: Record<string, GitInfo>
  onSelectWorkspace: (workspaceId: string) => void
  onSelectPane: (workspaceId: string, paneId: string) => void
}

export const WorkspaceOverviewDashboard: React.FC<WorkspaceOverviewDashboardProps> = ({
  workspaces,
  activeWorkspaceId,
  attentionPaneIds,
  attentionWorkspaceIds,
  sessionsById,
  gitInfoById,
  onSelectWorkspace,
  onSelectPane,
}) => {
  return (
    <aside
      aria-label="Workspace overview dashboard"
      style={{
        width: 320,
        minWidth: 280,
        maxWidth: 360,
        display: 'flex',
        flexDirection: 'column',
        gap: 12,
        padding: 12,
        borderLeft: '1px solid #333842',
        background: 'linear-gradient(180deg, #181a1f 0%, #15171b 100%)',
        overflowY: 'auto',
      }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <div style={{ color: '#f2f4f8', fontFamily: TERMINAL_FONT_FAMILY, fontSize: 13, fontWeight: 700 }}>
          Workspace Overview
        </div>
        <div style={{ color: '#8f98a8', fontFamily: TERMINAL_FONT_FAMILY, fontSize: 11, lineHeight: 1.4 }}>
          All workspaces, panes, and their current state.
        </div>
      </div>

      {workspaces.map((workspace) => {
        const panes = collectWorkspacePanes(workspace.layout.children)
        const counts = {
          connected: 0,
          disconnected: 0,
          exited: 0,
          pending: 0,
        }

        for (const pane of panes) {
          const state = sessionsById[pane.id]?.state
          if (state === 'disconnected') counts.disconnected += 1
          else if (state === 'exited') counts.exited += 1
          else if (state === 'connected' || state === 'connecting') counts.connected += 1
          else counts.pending += 1
        }

        const isActive = workspace.id === activeWorkspaceId
        const hasAttention = attentionWorkspaceIds.has(workspace.id)

        return (
          <section
            key={workspace.id}
            style={{
              border: isActive ? '1px solid #6d88a8' : '1px solid #2d323c',
              borderRadius: 10,
              backgroundColor: isActive ? '#202630' : '#1b1e24',
              boxShadow: hasAttention ? '0 0 0 1px rgba(244, 191, 79, 0.4)' : 'none',
              overflow: 'hidden',
            }}
          >
            <button
              type="button"
              aria-label={`Open workspace ${workspace.title}`}
              onClick={() => onSelectWorkspace(workspace.id)}
              style={{
                width: '100%',
                appearance: 'none',
                border: 'none',
                background: 'transparent',
                color: '#eef2f7',
                padding: '10px 12px',
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 12,
                textAlign: 'left',
                fontFamily: TERMINAL_FONT_FAMILY,
              }}
            >
              <div style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span style={{ fontSize: 12, fontWeight: 700, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {workspace.title}
                  </span>
                  {isActive && (
                    <span style={pillStyle('#29425d', '#8ec7ff')}>
                      Active
                    </span>
                  )}
                  {hasAttention && (
                    <span style={pillStyle('#5a4311', '#f4bf4f')}>
                      Attention
                    </span>
                  )}
                </div>
                <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', color: '#8f98a8', fontSize: 11 }}>
                  <span>{panes.length} panes</span>
                  <span>{counts.connected} up</span>
                  <span>{counts.disconnected} down</span>
                  <span>{counts.exited} exited</span>
                  {counts.pending > 0 && <span>{counts.pending} pending</span>}
                </div>
              </div>
              <span style={{ color: '#697385', fontSize: 12 }}>Open</span>
            </button>

            <div style={{ borderTop: '1px solid #2d323c' }}>
              {panes.map((pane) => {
                const session = sessionsById[pane.id]
                const gitInfo = gitInfoById[pane.id]
                const hasPaneAttention = attentionPaneIds.has(pane.id)
                const paneLabel = pane.title ?? session?.title ?? pane.id

                return (
                  <button
                    key={pane.id}
                    type="button"
                    aria-label={`Open pane ${paneLabel} in ${workspace.title}`}
                    onClick={() => onSelectPane(workspace.id, pane.id)}
                    style={{
                      width: '100%',
                      appearance: 'none',
                      border: 'none',
                      borderTop: '1px solid #262a33',
                      background: hasPaneAttention ? 'rgba(244, 191, 79, 0.08)' : 'transparent',
                      color: '#d7dce5',
                      padding: '10px 12px',
                      textAlign: 'left',
                      cursor: 'pointer',
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 5,
                      fontFamily: TERMINAL_FONT_FAMILY,
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                      <span style={statusDotStyle(session?.state)} />
                      <span style={{ fontSize: 12, fontWeight: 600, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {paneLabel}
                      </span>
                      <span style={{ color: '#8f98a8', fontSize: 11, textTransform: 'uppercase' }}>{pane.type}</span>
                      {hasPaneAttention && (
                        <span style={pillStyle('#5a4311', '#f4bf4f')}>
                          Needs input
                        </span>
                      )}
                    </div>
                    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', color: '#8f98a8', fontSize: 11 }}>
                      <span>{session?.state ?? 'connecting'}</span>
                      {gitInfo?.repo && <span>{gitInfo.repo}</span>}
                      {gitInfo?.branch && <span>{gitInfo.branch}</span>}
                      {gitInfo?.pr_number && <span>PR #{gitInfo.pr_number}</span>}
                    </div>
                  </button>
                )
              })}
            </div>
          </section>
        )
      })}
    </aside>
  )
}

function collectWorkspacePanes(children: LayoutChild[]) {
  const panes: Array<Workspace['layout']['children'][number]['pane'] & { id: string; type: string; title?: string }> = []

  for (const child of children) {
    collectChildPanes(child, panes)
  }

  return panes
}

function collectChildPanes(
  child: LayoutChild,
  panes: Array<{ id: string; type: string; title?: string }>,
) {
  // Dashboard rows mirror terminal leaf panes. If a malformed layout node ever
  // carries both pane metadata and nested children, prefer the pane and stop so
  // the dashboard does not silently drop it.
  if (child.pane) {
    panes.push(child.pane)
    return
  }

  if (!child.children?.length) return

  for (const nestedChild of child.children) {
    collectChildPanes(nestedChild, panes)
  }
}

function pillStyle(backgroundColor: string, color: string): React.CSSProperties {
  return {
    display: 'inline-flex',
    alignItems: 'center',
    padding: '2px 6px',
    borderRadius: 999,
    backgroundColor,
    color,
    fontSize: 10,
    fontWeight: 700,
    letterSpacing: '0.02em',
  }
}

function statusDotStyle(state: SessionInfo['state'] | undefined): React.CSSProperties {
  const color = state === 'disconnected'
    ? '#f0c674'
    : state === 'exited'
      ? '#f08b8b'
      : '#7bd88f'

  return {
    width: 8,
    height: 8,
    borderRadius: '50%',
    backgroundColor: color,
    boxShadow: `0 0 0 1px ${color}33`,
    flexShrink: 0,
  }
}
