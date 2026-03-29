import React, { useState, useCallback } from 'react'
import { SplitContainer, LayoutActionsContext } from './components/SplitContainer'
import { EditModeToggle } from './components/EditModeToggle'
import { PaneSettingsDialog } from './components/PaneSettingsDialog'
import { AddSSHHostDialog } from './components/AddSSHHostDialog'
import { useLayout } from './hooks/useLayout'
import { useEditMode } from './hooks/useEditMode'
import { usePaneSettings } from './hooks/usePaneSettings'
import { DisplayConfig } from './types'
import { TERMINAL_FONT_FAMILY } from './utils/fonts'
import { findPaneById } from './utils/layoutTree'
import type { SSHConfigHost } from './schemas'

const DEFAULT_DISPLAY: DisplayConfig = { show_header: true, show_status_bar: true }

export const App: React.FC = () => {
  const { layout, displayConfig, error, updateSizes, splitPane, closePane, swapPanes } = useLayout()
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
      <div style={{ position: 'relative', width: '100%', height: '100%' }}>
        <SplitContainer layout={layout} onLayoutChange={updateSizes} />
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
