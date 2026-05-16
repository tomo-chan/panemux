import React, { useCallback, useContext, useEffect, useRef, useState } from 'react'
import { DisplayConfig, PaneConfig } from '../types'
import { useTerminal } from '../hooks/useTerminal'
import { useGitInfo } from '../hooks/useGitInfo'
import type { PaneEdge } from '../utils/layoutTree'
import { PaneHeader } from './PaneHeader'
import { PaneStatusBar } from './PaneStatusBar'
import { LayoutActionsContext } from './SplitContainer'

const DEFAULT_DISPLAY: DisplayConfig = { show_header: true, show_status_bar: true }
export const PANE_DROP_ZONE_THICKNESS = 24

interface TerminalPaneProps {
  pane: PaneConfig
}

export const TerminalPane: React.FC<TerminalPaneProps> = ({ pane }) => {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const [containerEl, setContainerEl] = React.useState<HTMLElement | null>(null)
  const [hoverEdge, setHoverEdge] = useState<PaneEdge | null>(null)

  const setRef = useCallback((el: HTMLDivElement | null) => {
    containerRef.current = el
    setContainerEl(el)
  }, [])

  const ctx = useContext(LayoutActionsContext)
  const displayConfig = ctx?.displayConfig ?? DEFAULT_DISPLAY
  const hasAttention = ctx?.hasPaneAttention(pane.id) ?? false
  const isDragSource = ctx?.dragSourcePaneId === pane.id
  const dragActive = Boolean(ctx?.dragSourcePaneId)

  const { handleResize, connected, dims, sessionExited, restartSession } = useTerminal({
    sessionId: pane.id,
    container: containerEl,
  })

  const gitInfo = useGitInfo(pane.id)

  // Observe resize events for this pane
  useEffect(() => {
    if (!containerEl) return
    const observer = new ResizeObserver(() => {
      handleResize()
    })
    observer.observe(containerEl)
    return () => observer.disconnect()
  }, [containerEl, handleResize])

  const handleOpenVSCode = useCallback(() => {
    fetch(`/api/sessions/${pane.id}/open-vscode`, { method: 'POST' })
      .catch((err) => console.error('open-vscode failed:', err))
  }, [pane.id])

  const handleDragStart = useCallback((e: React.DragEvent) => {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', pane.id)
    ctx?.setDragSourcePaneId(pane.id)
  }, [ctx, pane.id])

  const handleDragEnd = useCallback(() => {
    ctx?.setDragSourcePaneId(null)
    setHoverEdge(null)
  }, [ctx])

  const handleMoveHandleMouseDown = useCallback((e: React.MouseEvent) => {
    if (e.button !== 0) return
    e.preventDefault()
    ctx?.setDragSourcePaneId(pane.id)
  }, [ctx, pane.id])

  useEffect(() => {
    if (!isDragSource) return

    const handleMouseUp = () => {
      ctx?.setDragSourcePaneId(null)
      setHoverEdge(null)
    }

    window.addEventListener('mouseup', handleMouseUp)
    return () => window.removeEventListener('mouseup', handleMouseUp)
  }, [ctx, isDragSource])

  const handleEdgeDrop = useCallback((edge: PaneEdge) => {
    const sourceId = ctx?.dragSourcePaneId
    if (!sourceId || sourceId === pane.id) return
    ctx?.onMovePaneBeside(sourceId, pane.id, edge)
    ctx?.setDragSourcePaneId(null)
    setHoverEdge(null)
  }, [ctx, pane.id])

  return (
    <div
      data-pane-id={pane.id}
      className={hasAttention ? 'panemux-pane-attention' : undefined}
      data-attention={hasAttention ? 'true' : undefined}
      onMouseDown={() => ctx?.clearPaneAttention(pane.id)}
      onFocusCapture={() => ctx?.clearPaneAttention(pane.id)}
      style={{
        display: 'flex',
        flexDirection: 'column',
        width: '100%',
        height: '100%',
        overflow: 'hidden',
        backgroundColor: '#1a1b1e',
        outline: hasAttention
          ? '2px solid rgba(244, 191, 79, 0.95)'
          : 'none',
        outlineOffset: '-2px',
        opacity: isDragSource ? 0.5 : 1,
        transition: 'opacity 0.15s ease',
      }}
    >
      <PaneHeader
        pane={pane}
        connected={connected}
        displayConfig={displayConfig}
        isMaximized={ctx?.maximizedPaneId === pane.id}
        gitInfo={gitInfo}
        onSplit={(direction) => ctx?.onSplit(pane.id, direction)}
        onClose={() => ctx?.onClose(pane.id)}
        onMaximize={() => ctx?.onMaximize(ctx.maximizedPaneId === pane.id ? null : pane.id)}
        onSettings={() => ctx?.onSettings(pane.id)}
        onOpenVSCode={handleOpenVSCode}
        moveHandleProps={{ onDragStart: handleDragStart, onDragEnd: handleDragEnd, onMouseDown: handleMoveHandleMouseDown }}
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
        {dragActive && !isDragSource && (
          <>
            {(['top', 'bottom', 'left', 'right'] as PaneEdge[]).map((edge) => (
              <div
                key={edge}
                data-pane-drop-edge={edge}
                onDragOver={(e) => {
                  if (!ctx?.dragSourcePaneId || ctx.dragSourcePaneId === pane.id) return
                  e.preventDefault()
                  e.dataTransfer.dropEffect = 'move'
                  setHoverEdge(edge)
                }}
                onDragLeave={() => setHoverEdge((current) => (current === edge ? null : current))}
                onDrop={(e) => {
                  e.preventDefault()
                  handleEdgeDrop(edge)
                }}
                onMouseEnter={() => {
                  if (!ctx?.dragSourcePaneId || ctx.dragSourcePaneId === pane.id) return
                  setHoverEdge(edge)
                }}
                onMouseLeave={() => setHoverEdge((current) => (current === edge ? null : current))}
                onMouseUp={() => handleEdgeDrop(edge)}
                style={dropZoneStyle(edge, hoverEdge === edge)}
              />
            ))}
          </>
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

export function dropZoneStyle(edge: PaneEdge, active: boolean): React.CSSProperties {
  const common: React.CSSProperties = {
    position: 'absolute',
    zIndex: 8,
    backgroundColor: active ? 'rgba(86, 156, 214, 0.35)' : 'transparent',
    transition: 'background-color 0.15s ease',
  }

  switch (edge) {
    case 'top':
      return { ...common, top: 0, left: 0, right: 0, height: PANE_DROP_ZONE_THICKNESS, cursor: 'row-resize' }
    case 'bottom':
      return { ...common, bottom: 0, left: 0, right: 0, height: PANE_DROP_ZONE_THICKNESS, cursor: 'row-resize' }
    case 'left':
      return { ...common, top: 0, bottom: 0, left: 0, width: PANE_DROP_ZONE_THICKNESS, cursor: 'col-resize' }
    case 'right':
      return { ...common, top: 0, bottom: 0, right: 0, width: PANE_DROP_ZONE_THICKNESS, cursor: 'col-resize' }
  }
}
