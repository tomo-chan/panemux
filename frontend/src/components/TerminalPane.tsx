import React, { useCallback, useContext, useEffect, useRef } from 'react'
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

  const setRef = useCallback((el: HTMLDivElement | null) => {
    containerRef.current = el
    setContainerEl(el)
  }, [])

  const { handleResize, connected, dims, sessionExited, restartSession } = useTerminal({
    sessionId: pane.id,
    container: containerEl,
  })

  const ctx = useContext(LayoutActionsContext)
  const displayConfig = ctx?.displayConfig ?? DEFAULT_DISPLAY

  // Observe resize events for this pane
  useEffect(() => {
    if (!containerEl) return
    const observer = new ResizeObserver(() => {
      handleResize()
    })
    observer.observe(containerEl)
    return () => observer.disconnect()
  }, [containerEl, handleResize])

  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      width: '100%',
      height: '100%',
      overflow: 'hidden',
      backgroundColor: '#1a1b1e',
    }}>
      <PaneHeader
        pane={pane}
        connected={connected}
        displayConfig={displayConfig}
        isMaximized={ctx?.maximizedPaneId === pane.id}
        editMode={ctx?.editMode ?? false}
        onSplit={(direction) => ctx?.onSplit(pane.id, direction)}
        onClose={() => ctx?.onClose(pane.id)}
        onMaximize={() => ctx?.onMaximize(ctx.maximizedPaneId === pane.id ? null : pane.id)}
        onSettings={() => ctx?.onSettings(pane.id)}
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
