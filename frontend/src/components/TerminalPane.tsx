import React, { useCallback, useContext, useEffect, useRef, useState } from 'react'
import { DisplayConfig, PaneConfig } from '../types'
import { useTerminal } from '../hooks/useTerminal'
import { PaneHeader } from './PaneHeader'
import { PaneStatusBar } from './PaneStatusBar'
import { LayoutActionsContext } from './SplitContainer'

const DEFAULT_DISPLAY: DisplayConfig = { show_header: true, show_status_bar: false }

interface TerminalPaneProps {
  pane: PaneConfig
}

export const TerminalPane: React.FC<TerminalPaneProps> = ({ pane }) => {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const [containerEl, setContainerEl] = React.useState<HTMLElement | null>(null)
  const [isDragOver, setIsDragOver] = useState(false)

  const setRef = useCallback((el: HTMLDivElement | null) => {
    containerRef.current = el
    setContainerEl(el)
  }, [])

  const ctx = useContext(LayoutActionsContext)
  const displayConfig = ctx?.displayConfig ?? DEFAULT_DISPLAY
  const editMode = ctx?.editMode ?? false
  // Derive from context rather than local state: when the DOM element is moved
  // by React during a swap, the browser may not fire dragend on the source pane,
  // leaving local isDragging=true permanently. Using the global dragSourcePaneId
  // (cleared by handleDrop before dragend would fire) avoids this.
  const isDragging = ctx?.dragSourcePaneId === pane.id

  const { handleResize, connected, dims, sessionExited, restartSession } = useTerminal({
    sessionId: pane.id,
    container: containerEl,
    editMode,
  })

  // Observe resize events for this pane
  useEffect(() => {
    if (!containerEl) return
    const observer = new ResizeObserver(() => {
      handleResize()
    })
    observer.observe(containerEl)
    return () => observer.disconnect()
  }, [containerEl, handleResize])

  const handleDragStart = (e: React.DragEvent) => {
    e.dataTransfer.effectAllowed = 'move'
    ctx?.setDragSourcePaneId(pane.id)
  }

  const handleDragEnd = () => {
    ctx?.setDragSourcePaneId(null)
    setIsDragOver(false)
  }

  const handleDragOver = (e: React.DragEvent) => {
    if (!ctx?.dragSourcePaneId || ctx.dragSourcePaneId === pane.id) return
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
    setIsDragOver(true)
  }

  const handleDragLeave = () => setIsDragOver(false)

  const handleOpenVSCode = useCallback(() => {
    fetch(`/api/sessions/${pane.id}/open-vscode`, { method: 'POST' })
      .catch((err) => console.error('open-vscode failed:', err))
  }, [pane.id])

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragOver(false)
    const sourceId = ctx?.dragSourcePaneId
    if (!sourceId || sourceId === pane.id) return
    ctx?.onSwapPanes(sourceId, pane.id)
    ctx?.setDragSourcePaneId(null)
  }

  return (
    <div
      draggable={editMode}
      onDragStart={editMode ? handleDragStart : undefined}
      onDragEnd={editMode ? handleDragEnd : undefined}
      onDragOver={editMode ? handleDragOver : undefined}
      onDragLeave={editMode ? handleDragLeave : undefined}
      onDrop={editMode ? handleDrop : undefined}
      style={{
        display: 'flex',
        flexDirection: 'column',
        width: '100%',
        height: '100%',
        overflow: 'hidden',
        backgroundColor: '#1a1b1e',
        // Outline priority: drop-target > drag-source > none
        outline: isDragOver
          ? '2px solid #569cd6'
          : isDragging
          ? '2px dashed rgba(86, 156, 214, 0.7)'
          : 'none',
        outlineOffset: '-2px',
        // Subtle inset frame to mark pane as "in edit zone"
        boxShadow: editMode && !isDragOver
          ? 'inset 0 0 0 1px rgba(86, 156, 214, 0.18)'
          : 'none',
        // "Lifted" appearance when this pane is the drag source
        opacity: isDragging ? 0.35 : 1,
        transition: 'opacity 0.15s ease, box-shadow 0.2s ease',
      }}
    >
      <PaneHeader
        pane={pane}
        connected={connected}
        displayConfig={displayConfig}
        isMaximized={ctx?.maximizedPaneId === pane.id}
        editMode={editMode}
        onSplit={(direction) => ctx?.onSplit(pane.id, direction)}
        onClose={() => ctx?.onClose(pane.id)}
        onMaximize={() => ctx?.onMaximize(ctx.maximizedPaneId === pane.id ? null : pane.id)}
        onSettings={() => ctx?.onSettings(pane.id)}
        onOpenVSCode={handleOpenVSCode}
      />
      <div
        ref={setRef}
        style={{
          flex: 1,
          overflow: 'hidden',
          padding: '4px',
          position: 'relative',
        }}
      >
        {editMode && (
          <div style={{
            position: 'absolute',
            inset: 0,
            zIndex: 5,
            cursor: isDragging ? 'grabbing' : 'grab',
            // Blue-tinted dark overlay — clearly dims terminal content
            backgroundColor: 'rgba(10, 20, 38, 0.54)',
          }} />
        )}
        {sessionExited && (
          <div style={{
            position: 'absolute',
            inset: 0,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            backgroundColor: 'rgba(0, 0, 0, 0.6)',
            zIndex: 10,
          }}>
            <button
              onClick={restartSession}
              style={{
                padding: '6px 18px',
                backgroundColor: '#3f3f46',
                color: '#d4d4d4',
                border: '1px solid #52525b',
                borderRadius: '4px',
                fontSize: '13px',
                cursor: 'pointer',
              }}
            >
              Restart Session
            </button>
          </div>
        )}
      </div>
      <PaneStatusBar
        pane={pane}
        displayConfig={displayConfig}
        cols={dims?.cols}
        rows={dims?.rows}
      />
    </div>
  )
}
