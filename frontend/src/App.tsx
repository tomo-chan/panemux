import React, { useState, useCallback, useMemo } from 'react'
import { SplitContainer, LayoutActionsContext } from './components/SplitContainer'
import { EditModeToggle } from './components/EditModeToggle'
import { PaneSettingsDialog } from './components/PaneSettingsDialog'
import { AddSSHHostDialog } from './components/AddSSHHostDialog'
import { WorkspaceTabs } from './components/WorkspaceTabs'
import { useLayout } from './hooks/useLayout'
import { useEditMode } from './hooks/useEditMode'
import { usePaneSettings } from './hooks/usePaneSettings'
import { useWorkspaceAttentionMonitor } from './hooks/useWorkspaceAttentionMonitor'
import { useBrowserNotificationPermission } from './hooks/useBrowserNotificationPermission'
import { DisplayConfig } from './types'
import { TERMINAL_FONT_FAMILY } from './utils/fonts'
import { findPaneById, layoutContainsPane } from './utils/layoutTree'
import type { LayoutChild, LayoutNode, SSHConfigHost } from './schemas'

const DEFAULT_DISPLAY: DisplayConfig = { show_header: true, show_status_bar: true }

export const App: React.FC = () => {
  const { layout, workspaces, displayConfig, error, updateSizes, splitPane, closePane, swapPanes, setActiveWorkspace, addWorkspace, deleteWorkspace, renameWorkspace, setWorkspaceTabPosition } = useLayout()
  const { editMode, toggleEditMode } = useEditMode()
  const [maximizedPaneId, setMaximizedPaneId] = useState<string | null>(null)
  const [dragSourcePaneId, setDragSourcePaneId] = useState<string | null>(null)
  const [attentionPaneIds, setAttentionPaneIds] = useState<Set<string>>(() => new Set())
  const { isOpen, currentPane, sshConnectionNames, saveError, isSaving, openSettings, closeSettings, saveSettings, addSSHConfigHost, detectShell } =
    usePaneSettings(layout, updateSizes)

  const [isAddSSHHostOpen, setIsAddSSHHostOpen] = useState(false)
  const [addSSHHostError, setAddSSHHostError] = useState<string | null>(null)
  const [isAddSSHHostSaving, setIsAddSSHHostSaving] = useState(false)

  const paneMetadataByID = useMemo(() => {
    const metadata = new Map<string, { paneTitle: string; workspaceId: string; workspaceTitle: string }>()
    if (!workspaces) return metadata

    for (const workspace of workspaces.items) {
      collectPaneMetadata(workspace.layout, workspace.id, workspace.title, metadata)
    }

    return metadata
  }, [workspaces])

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
      onClose: closePane,
      onMaximize: setMaximizedPaneId,
      onSettings: (paneId: string) => {
        const pane = findPaneById(layout, paneId)
        if (pane) openSettings(pane)
      },
      onSwapPanes: swapPanes,
      maximizedPaneId,
      dragSourcePaneId,
      setDragSourcePaneId,
      displayConfig: displayConfig ?? DEFAULT_DISPLAY,
      editMode,
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
        {workspaces && (workspaces.items.length > 1 || editMode) && (
          <WorkspaceTabs
            workspaces={workspaces.items}
            activeWorkspaceId={workspaces.active}
            tabPosition={workspaces.tab_position}
            onSelect={setActiveWorkspace}
            attentionWorkspaceIds={attentionWorkspaceIds}
            onClearAttention={clearWorkspaceAttention}
            onAdd={editMode ? addWorkspace : undefined}
            onRename={editMode ? renameWorkspace : undefined}
            onTabPositionChange={editMode ? setWorkspaceTabPosition : undefined}
            onDelete={editMode ? (workspaceId) => {
              const workspace = workspaces.items.find((item) => item.id === workspaceId)
              if (!workspace) return
              if (window.confirm(`Delete workspace "${workspace.title}"?`)) {
                void deleteWorkspace(workspaceId)
              }
            } : undefined}
          />
        )}
        <div style={{ position: 'relative', flex: 1, minWidth: 0, minHeight: 0 }}>
          <SplitContainer layout={layout} onLayoutChange={updateSizes} />
        </div>
        <EditModeToggle editMode={editMode} onToggle={toggleEditMode} />
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
