import React, { useState, useCallback } from 'react'
import { SplitContainer, LayoutActionsContext } from './components/SplitContainer'
import { EditModeToggle } from './components/EditModeToggle'
import { PaneSettingsDialog } from './components/PaneSettingsDialog'
import { AddSSHHostDialog } from './components/AddSSHHostDialog'
import { WorkspaceTabs } from './components/WorkspaceTabs'
import { useLayout } from './hooks/useLayout'
import { useEditMode } from './hooks/useEditMode'
import { usePaneSettings } from './hooks/usePaneSettings'
import { DisplayConfig } from './types'
import { TERMINAL_FONT_FAMILY } from './utils/fonts'
import { findPaneById } from './utils/layoutTree'
import type { SSHConfigHost } from './schemas'

const DEFAULT_DISPLAY: DisplayConfig = { show_header: true, show_status_bar: true }

export const App: React.FC = () => {
  const { layout, workspaces, displayConfig, error, updateSizes, splitPane, closePane, swapPanes, setActiveWorkspace, addWorkspace, deleteWorkspace } = useLayout()
  const { editMode, toggleEditMode } = useEditMode()
  const [maximizedPaneId, setMaximizedPaneId] = useState<string | null>(null)
  const [dragSourcePaneId, setDragSourcePaneId] = useState<string | null>(null)
  const { isOpen, currentPane, sshConnectionNames, saveError, isSaving, openSettings, closeSettings, saveSettings, addSSHConfigHost, detectShell } =
    usePaneSettings(layout, updateSizes)

  const [isAddSSHHostOpen, setIsAddSSHHostOpen] = useState(false)
  const [addSSHHostError, setAddSSHHostError] = useState<string | null>(null)
  const [isAddSSHHostSaving, setIsAddSSHHostSaving] = useState(false)

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
            onAdd={editMode ? addWorkspace : undefined}
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
