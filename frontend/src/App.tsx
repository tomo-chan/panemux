import React, { useState, useCallback, useMemo } from 'react'
import { SplitContainer, LayoutActionsContext } from './components/SplitContainer'
import { PaneSettingsDialog } from './components/PaneSettingsDialog'
import { AddSSHHostDialog } from './components/AddSSHHostDialog'
import { WorkspaceTabs } from './components/WorkspaceTabs'
import { WorkspaceOverviewDashboard } from './components/WorkspaceOverviewDashboard'
import { useLayout } from './hooks/useLayout'
import { usePaneSettings } from './hooks/usePaneSettings'
import { useWorkspaceAttentionMonitor } from './hooks/useWorkspaceAttentionMonitor'
import { useBrowserNotificationPermission } from './hooks/useBrowserNotificationPermission'
import { useSessionsOverview } from './hooks/useSessionsOverview'
import { useGitInfoMap } from './hooks/useGitInfoMap'
import { DisplayConfig } from './types'
import { TERMINAL_FONT_FAMILY } from './utils/fonts'
import { findPaneById, generatePaneId, layoutContainsPane } from './utils/layoutTree'
import type { MovePanePlacement } from './hooks/useLayout'
import type { LayoutChild, LayoutNode, SSHConfigHost } from './schemas'

const DEFAULT_DISPLAY: DisplayConfig = { show_header: true, show_status_bar: true }

export const App: React.FC = () => {
  const { layout, workspaces, displayConfig, error, updateSizes, splitPane, closePane, swapPanes, createPane, movePane, setActiveWorkspace, addWorkspace, deleteWorkspace, renameWorkspace, setWorkspaceTabPosition } = useLayout()
  const [maximizedPaneId, setMaximizedPaneId] = useState<string | null>(null)
  const [dragSourcePaneId, setDragSourcePaneId] = useState<string | null>(null)
  const [attentionPaneIds, setAttentionPaneIds] = useState<Set<string>>(() => new Set())
  const { isOpen, currentPane, sshConnectionNames, saveError, isSaving, openSettings, closeSettings, saveSettings, addSSHConfigHost, detectShell, browseDirectories } =
    usePaneSettings(layout, updateSizes)

  const [isAddSSHHostOpen, setIsAddSSHHostOpen] = useState(false)
  const [addSSHHostError, setAddSSHHostError] = useState<string | null>(null)
  const [isAddSSHHostSaving, setIsAddSSHHostSaving] = useState(false)
  const [createPaneError, setCreatePaneError] = useState<string | null>(null)
  const [movePaneError, setMovePaneError] = useState<string | null>(null)
  const sessionsById = useSessionsOverview(Boolean(workspaces))

  const paneMetadataByID = useMemo(() => {
    const metadata = new Map<string, { paneTitle: string; workspaceId: string; workspaceTitle: string }>()
    if (!workspaces) return metadata

    for (const workspace of workspaces.items) {
      collectPaneMetadata(workspace.layout, workspace.id, workspace.title, metadata)
    }

    return metadata
  }, [workspaces])
  const overviewPaneIds = useMemo(() => Array.from(paneMetadataByID.keys()), [paneMetadataByID])
  const gitInfoById = useGitInfoMap(overviewPaneIds, overviewPaneIds.length > 0)

  const findWorkspaceForPane = useCallback((paneId: string) => {
    return workspaces?.items.find((workspace) => layoutContainsPane(workspace.layout, paneId)) ?? null
  }, [workspaces])

  const attentionWorkspaceIds = useMemo(() => {
    const workspaceIds = new Set<string>()
    if (!workspaces) return workspaceIds

    for (const paneId of attentionPaneIds) {
      const workspace = workspaces.items.find((item) => layoutContainsPane(item.layout, paneId))
      if (workspace) workspaceIds.add(workspace.id)
    }

    return workspaceIds
  }, [attentionPaneIds, workspaces])

  const clearWorkspaceAttention = useCallback((workspaceId: string) => {
    const workspace = workspaces?.items.find((item) => item.id === workspaceId)
    if (!workspace) return

    setAttentionPaneIds((current) => {
      const next = new Set<string>()
      let removed = false
      for (const paneId of current) {
        if (layoutContainsPane(workspace.layout, paneId)) {
          removed = true
        } else {
          next.add(paneId)
        }
      }
      return removed ? next : current
    })
  }, [workspaces])

  const clearPaneAttention = useCallback((paneId: string) => {
    setAttentionPaneIds((current) => {
      if (!current.has(paneId)) return current
      const next = new Set(current)
      next.delete(paneId)
      return next
    })
  }, [])

  const notifyAttention = useCallback((paneId: string, showNotification = true) => {
    const paneMetadata = paneMetadataByID.get(paneId)
    const workspace = paneMetadata ? workspaces?.items.find((item) => item.id === paneMetadata.workspaceId) ?? null : findWorkspaceForPane(paneId)
    const pane = workspace ? findPaneById(workspace.layout, paneId) : layout ? findPaneById(layout, paneId) : null
    const paneTitle = paneMetadata?.paneTitle ?? pane?.title ?? paneId
    const workspaceTitle = paneMetadata?.workspaceTitle ?? workspace?.title

    setAttentionPaneIds((current) => new Set(current).add(paneId))
    if (!showNotification) return

    showBrowserNotification(
      'Agent confirmation requested',
      workspaceTitle ? `${paneTitle} in ${workspaceTitle}` : paneTitle,
      workspace ? () => {
        window.focus()
        setActiveWorkspace(workspace.id).catch(console.error)
      } : undefined,
    )
  }, [findWorkspaceForPane, layout, paneMetadataByID, setActiveWorkspace, workspaces])

  useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId, onAttention: notifyAttention })
  useBrowserNotificationPermission()

  const handleAddSSHHost = useCallback(async (host: SSHConfigHost) => {
    setIsAddSSHHostSaving(true)
    setAddSSHHostError(null)
    try {
      await addSSHConfigHost(host)
      setIsAddSSHHostOpen(false)
    } catch (err) {
      setAddSSHHostError(err instanceof Error ? err.message : 'Failed to add host')
    } finally {
      setIsAddSSHHostSaving(false)
    }
  }, [addSSHConfigHost])

  const handleCreateDefaultPane = useCallback((targetPaneId: string, edge: 'right' | 'bottom') => {
    setCreatePaneError(null)
    void createPane(
      { id: generatePaneId(), type: 'local' },
      { type: 'pane-edge', targetPaneId, edge },
    ).catch((err) => {
      setCreatePaneError(err instanceof Error ? err.message : 'Something went wrong')
    })
  }, [createPane])

  const handleMovePane = useCallback((sourcePaneId: string, placement: MovePanePlacement) => {
    setMovePaneError(null)
    void movePane(sourcePaneId, placement).catch((err) => {
      setMovePaneError(err instanceof Error ? err.message : 'Something went wrong')
    })
  }, [movePane])

  const handleSelectOverviewPane = useCallback((workspaceId: string, paneId: string) => {
    clearPaneAttention(paneId)
    clearWorkspaceAttention(workspaceId)
    void setActiveWorkspace(workspaceId)
  }, [clearPaneAttention, clearWorkspaceAttention, setActiveWorkspace])

  const handleSelectOverviewWorkspace = useCallback((workspaceId: string) => {
    clearWorkspaceAttention(workspaceId)
    void setActiveWorkspace(workspaceId)
  }, [clearWorkspaceAttention, setActiveWorkspace])

  if (error) {
    return (
      <div style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        height: '100%',
        color: '#f44747',
        fontFamily: TERMINAL_FONT_FAMILY,
        fontSize: '14px',
      }}>
        Failed to load layout: {error}
      </div>
    )
  }

  if (!layout) {
    return (
      <div style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        height: '100%',
        color: '#555',
        fontFamily: TERMINAL_FONT_FAMILY,
        fontSize: '14px',
      }}>
        Loading…
      </div>
    )
  }

  return (
    <LayoutActionsContext.Provider value={{
      onSplit: splitPane,
      onCreatePaneBeside: handleCreateDefaultPane,
      onClose: closePane,
      onMaximize: setMaximizedPaneId,
      onSettings: (paneId: string) => {
        const pane = findPaneById(layout, paneId)
        if (pane) openSettings(pane)
      },
      onSwapPanes: swapPanes,
      onMovePaneToWorkspaceEdge: (sourcePaneId, edge) => {
        handleMovePane(sourcePaneId, { type: 'workspace-edge', edge })
      },
      onMovePaneBeside: (sourcePaneId, targetPaneId, edge) => {
        handleMovePane(sourcePaneId, { type: 'pane-edge', targetPaneId, edge })
      },
      maximizedPaneId,
      dragSourcePaneId,
      setDragSourcePaneId,
      displayConfig: displayConfig ?? DEFAULT_DISPLAY,
      onPaneAttention: notifyAttention,
      clearPaneAttention,
      hasPaneAttention: (paneId: string) => attentionPaneIds.has(paneId),
    }}>
      <div
        style={{
          position: 'relative',
          width: '100%',
          height: '100%',
          display: 'flex',
          flexDirection: workspaces?.tab_position === 'bottom'
            ? 'column-reverse'
            : workspaces?.tab_position === 'left'
              ? 'row'
              : workspaces?.tab_position === 'right'
                ? 'row-reverse'
                : 'column',
          backgroundColor: '#1a1b1e',
        }}
      >
        {workspaces && (
          <WorkspaceTabs
            workspaces={workspaces.items}
            activeWorkspaceId={workspaces.active}
            tabPosition={workspaces.tab_position}
            dragSourcePaneId={dragSourcePaneId}
            onMovePaneToWorkspace={(sourcePaneId, workspaceId) => {
              handleMovePane(sourcePaneId, { type: 'workspace-tab', workspaceId })
              setDragSourcePaneId(null)
            }}
            onSelect={setActiveWorkspace}
            attentionWorkspaceIds={attentionWorkspaceIds}
            onClearAttention={clearWorkspaceAttention}
            onAdd={addWorkspace}
            onRename={renameWorkspace}
            onTabPositionChange={setWorkspaceTabPosition}
            onDelete={(workspaceId) => {
              const workspace = workspaces.items.find((item) => item.id === workspaceId)
              if (!workspace) return
              if (window.confirm(`Delete workspace "${workspace.title}"?`)) {
                void deleteWorkspace(workspaceId)
              }
            }}
          />
        )}
        <div style={{ display: 'flex', flex: 1, minWidth: 0, minHeight: 0 }}>
          <div style={{ position: 'relative', flex: 1, minWidth: 0, minHeight: 0 }}>
            <SplitContainer layout={layout} onLayoutChange={updateSizes} />
            {createPaneError && (
              <div
                role="alert"
                style={{
                  position: 'absolute',
                  top: 12,
                  right: 12,
                  zIndex: 30,
                  maxWidth: 320,
                  padding: '8px 12px',
                  border: '1px solid #7f1d1d',
                  borderRadius: 6,
                  backgroundColor: '#2f1313',
                  color: '#fca5a5',
                  fontFamily: TERMINAL_FONT_FAMILY,
                  fontSize: '12px',
                  boxShadow: '0 8px 20px rgba(0, 0, 0, 0.35)',
                }}
              >
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'flex-start',
                    gap: 12,
                  }}
                >
                  <span style={{ flex: 1 }}>Failed to create terminal: {createPaneError}</span>
                  <button
                    type="button"
                    aria-label="Dismiss create terminal error"
                    onClick={() => setCreatePaneError(null)}
                    style={{
                      appearance: 'none',
                      border: 'none',
                      background: 'transparent',
                      color: '#fca5a5',
                      cursor: 'pointer',
                      fontFamily: TERMINAL_FONT_FAMILY,
                      fontSize: '12px',
                      lineHeight: 1,
                      padding: 0,
                    }}
                  >
                    ×
                  </button>
                </div>
              </div>
            )}
            {movePaneError && (
              <div
                role="alert"
                style={{
                  position: 'absolute',
                  top: createPaneError ? 72 : 12,
                  right: 12,
                  zIndex: 30,
                  maxWidth: 320,
                  padding: '8px 12px',
                  border: '1px solid #7f1d1d',
                  borderRadius: 6,
                  backgroundColor: '#2f1313',
                  color: '#fca5a5',
                  fontFamily: TERMINAL_FONT_FAMILY,
                  fontSize: '12px',
                  boxShadow: '0 8px 20px rgba(0, 0, 0, 0.35)',
                }}
              >
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'flex-start',
                    gap: 12,
                  }}
                >
                  <span style={{ flex: 1 }}>Failed to move terminal: {movePaneError}</span>
                  <button
                    type="button"
                    aria-label="Dismiss move error"
                    onClick={() => setMovePaneError(null)}
                    style={{
                      appearance: 'none',
                      border: 'none',
                      background: 'transparent',
                      color: '#fca5a5',
                      cursor: 'pointer',
                      fontFamily: TERMINAL_FONT_FAMILY,
                      fontSize: '12px',
                      lineHeight: 1,
                      padding: 0,
                    }}
                  >
                    ×
                  </button>
                </div>
              </div>
            )}
          </div>
          {workspaces && (
            <WorkspaceOverviewDashboard
              workspaces={workspaces.items}
              activeWorkspaceId={workspaces.active}
              attentionPaneIds={attentionPaneIds}
              attentionWorkspaceIds={attentionWorkspaceIds}
              sessionsById={sessionsById}
              gitInfoById={gitInfoById}
              onSelectWorkspace={handleSelectOverviewWorkspace}
              onSelectPane={handleSelectOverviewPane}
            />
          )}
        </div>
        <PaneSettingsDialog
          isOpen={isOpen}
          pane={currentPane}
          sshConnectionNames={sshConnectionNames}
          saveError={saveError}
          isSaving={isSaving}
          onSave={saveSettings}
          onClose={closeSettings}
          onAddSSHHost={() => setIsAddSSHHostOpen(true)}
          onDetectShell={detectShell}
          onBrowseDirectories={browseDirectories}
        />
        <AddSSHHostDialog
          isOpen={isAddSSHHostOpen}
          isSaving={isAddSSHHostSaving}
          saveError={addSSHHostError}
          onSave={handleAddSSHHost}
          onClose={() => setIsAddSSHHostOpen(false)}
        />
      </div>
    </LayoutActionsContext.Provider>
  )
}

function showBrowserNotification(title: string, body: string, onClick?: () => void) {
  if (!('Notification' in window)) return

  if (Notification.permission !== 'granted') return

  const notification = new Notification(title, { body })
  if (onClick) {
    notification.onclick = () => {
      onClick()
      notification.close()
    }
  }
}

function collectPaneMetadata(
  layout: LayoutNode,
  workspaceId: string,
  workspaceTitle: string,
  metadata: Map<string, { paneTitle: string; workspaceId: string; workspaceTitle: string }>,
) {
  for (const child of layout.children) {
    collectChildPaneMetadata(child, workspaceId, workspaceTitle, metadata)
  }
}

function collectChildPaneMetadata(
  child: LayoutChild,
  workspaceId: string,
  workspaceTitle: string,
  metadata: Map<string, { paneTitle: string; workspaceId: string; workspaceTitle: string }>,
) {
  if (child.pane && (!child.children || child.children.length === 0)) {
    metadata.set(child.pane.id, {
      paneTitle: child.pane.title ?? child.pane.id,
      workspaceId,
      workspaceTitle,
    })
    return
  }

  if (!child.children?.length) return

  for (const nestedChild of child.children) {
    collectChildPaneMetadata(nestedChild, workspaceId, workspaceTitle, metadata)
  }
}
