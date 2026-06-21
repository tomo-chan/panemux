import React, { useCallback, useContext, useEffect, useRef, useState } from 'react'
import { DisplayConfig, PaneConfig } from '../types'
import { useTerminal } from '../hooks/useTerminal'
import { useGitInfo } from '../hooks/useGitInfo'
import type { PaneEdge } from '../utils/layoutTree'
import { PaneHeader } from './PaneHeader'
import { PaneStatusBar } from './PaneStatusBar'
import { LayoutActionsContext } from './SplitContainer'

const DEFAULT_DISPLAY: DisplayConfig = { show_header: true, show_status_bar: true }
export const PANE_DROP_ZONE_RATIO = 0.5

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
  const gitInfoEnabled = !ctx?.maximizedPaneId || ctx.maximizedPaneId === pane.id

  const { gitInfo, refreshIfStale, refreshNow } = useGitInfo(pane.id, gitInfoEnabled)

  const { handleResize, connected, dims, sessionState, reconnectFailed, restartSession } = useTerminal({
    sessionId: pane.id,
    container: containerEl,
    repoURL: gitInfo.repo_url,
    onInteraction: refreshIfStale,
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

  const handleOpenVSCode = useCallback(() => {
    void refreshNow()
    fetch(`/api/sessions/${pane.id}/open-vscode`, { method: 'POST' })
      .catch((err) => console.error('open-vscode failed:', err))
  }, [pane.id, refreshNow])

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

    const previousCursor = document.body.style.cursor
    document.body.style.cursor = 'grabbing'

    const handleMouseUp = () => {
      ctx?.setDragSourcePaneId(null)
      setHoverEdge(null)
    }

    window.addEventListener('mouseup', handleMouseUp)
    return () => {
      document.body.style.cursor = previousCursor
      window.removeEventListener('mouseup', handleMouseUp)
    }
  }, [ctx, isDragSource])

  const handleEdgeDrop = useCallback((edge: PaneEdge) => {
    const sourceId = ctx?.dragSourcePaneId
    if (!sourceId || sourceId === pane.id) return
    ctx?.onMovePaneBeside(sourceId, pane.id, edge)
    ctx?.setDragSourcePaneId(null)
    setHoverEdge(null)
  }, [ctx, pane.id])

  const updateHoverEdge = useCallback((clientX: number, clientY: number) => {
    if (!containerRef.current || !ctx?.dragSourcePaneId || ctx.dragSourcePaneId === pane.id) return
    const rect = containerRef.current.getBoundingClientRect()
    setHoverEdge(resolvePaneDropEdge(rect, clientX, clientY))
  }, [ctx?.dragSourcePaneId, pane.id])

  return (
    <div
      data-pane-id={pane.id}
      className={hasAttention ? 'panemux-pane-attention' : undefined}
      data-attention={hasAttention ? 'true' : undefined}
      onMouseDown={() => {
        ctx?.clearPaneAttention(pane.id)
        void refreshIfStale()
      }}
      onFocusCapture={() => {
        ctx?.clearPaneAttention(pane.id)
        void refreshIfStale()
      }}
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
        opacity: isDragSource ? 0.38 : 1,
        transform: isDragSource ? 'scale(0.985)' : 'scale(1)',
        boxShadow: isDragSource ? '0 14px 36px rgba(0, 0, 0, 0.32)' : 'none',
        transition: 'opacity 0.15s ease, transform 0.15s ease, box-shadow 0.15s ease',
      }}
    >
      <PaneHeader
        pane={pane}
        connected={connected}
        displayConfig={displayConfig}
        isMaximized={ctx?.maximizedPaneId === pane.id}
        isDragging={isDragSource}
        gitInfo={gitInfo}
        onSplit={(direction) => ctx?.onSplit(pane.id, direction)}
        onCreateDefaultPane={(edge) => ctx?.onCreatePaneBeside(pane.id, edge)}
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
        onDragOver={(e) => {
          if (!ctx?.dragSourcePaneId || ctx.dragSourcePaneId === pane.id) return
          e.preventDefault()
          e.dataTransfer.dropEffect = 'move'
          updateHoverEdge(e.clientX, e.clientY)
        }}
        onDragLeave={() => setHoverEdge(null)}
        onDrop={(e) => {
          e.preventDefault()
          const rect = containerRef.current?.getBoundingClientRect()
          const edge = rect ? resolvePaneDropEdge(rect, e.clientX, e.clientY) : hoverEdge
          if (edge) handleEdgeDrop(edge)
        }}
        onMouseMove={(e) => {
          if (!ctx?.dragSourcePaneId || ctx.dragSourcePaneId === pane.id || (e.buttons & 1) !== 1) return
          updateHoverEdge(e.clientX, e.clientY)
        }}
        onMouseLeave={() => setHoverEdge(null)}
        onMouseUp={(e) => {
          if (!ctx?.dragSourcePaneId || ctx.dragSourcePaneId === pane.id) return
          const edge = resolvePaneDropEdge(e.currentTarget.getBoundingClientRect(), e.clientX, e.clientY)
          handleEdgeDrop(edge)
        }}
      >
        {dragActive && !isDragSource && hoverEdge && (
          <div
            data-pane-drop-preview={hoverEdge}
            style={dropZoneStyle(hoverEdge, true)}
          />
        )}
        {(sessionState === 'exited' || (sessionState === 'disconnected' && reconnectFailed)) && (
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
              {sessionState === 'exited' ? 'Restart Session' : 'Reconnect Session'}
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
    backgroundColor: active ? 'rgba(86, 156, 214, 0.24)' : 'transparent',
    boxShadow: active ? 'inset 0 0 0 1px rgba(137, 196, 244, 0.45)' : 'none',
    transition: 'background-color 0.15s ease, box-shadow 0.15s ease',
    pointerEvents: 'none',
  }

  switch (edge) {
    case 'top':
      return { ...common, top: 0, left: 0, right: 0, height: `${PANE_DROP_ZONE_RATIO * 100}%` }
    case 'bottom':
      return { ...common, bottom: 0, left: 0, right: 0, height: `${PANE_DROP_ZONE_RATIO * 100}%` }
    case 'left':
      return { ...common, top: 0, bottom: 0, left: 0, width: `${PANE_DROP_ZONE_RATIO * 100}%` }
    case 'right':
      return { ...common, top: 0, bottom: 0, right: 0, width: `${PANE_DROP_ZONE_RATIO * 100}%` }
  }
}

export function resolvePaneDropEdge(rect: DOMRect | Pick<DOMRect, 'left' | 'top' | 'width' | 'height'>, clientX: number, clientY: number): PaneEdge {
  const relativeX = (clientX - rect.left) / rect.width
  const relativeY = (clientY - rect.top) / rect.height
  const distanceLeft = relativeX
  const distanceRight = 1 - relativeX
  const distanceTop = relativeY
  const distanceBottom = 1 - relativeY
  const shortestDistance = Math.min(distanceLeft, distanceRight, distanceTop, distanceBottom)

  if (shortestDistance === distanceTop) return 'top'
  if (shortestDistance === distanceBottom) return 'bottom'
  if (shortestDistance === distanceLeft) return 'left'
  return 'right'
}
