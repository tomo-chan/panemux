import React, { useState } from 'react'
import { SplitContainer, LayoutActionsContext } from './components/SplitContainer'
import { TerminalPane } from './components/TerminalPane'
import { useLayout } from './hooks/useLayout'
import { DisplayConfig } from './types'
import { TERMINAL_FONT_FAMILY } from './utils/fonts'
import { findPaneById } from './utils/layoutTree'

const DEFAULT_DISPLAY: DisplayConfig = { show_header: true, show_status_bar: false }

export const App: React.FC = () => {
  const { layout, displayConfig, error, updateSizes, splitPane, closePane } = useLayout()
  const [maximizedPaneId, setMaximizedPaneId] = useState<string | null>(null)

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

  const maximizedPane = maximizedPaneId ? findPaneById(layout, maximizedPaneId) : null

  return (
    <LayoutActionsContext.Provider value={{
      onSplit: splitPane,
      onClose: closePane,
      onMaximize: setMaximizedPaneId,
      maximizedPaneId,
      displayConfig: displayConfig ?? DEFAULT_DISPLAY,
    }}>
      <div style={{ position: 'relative', width: '100%', height: '100%' }}>
        <SplitContainer layout={layout} onLayoutChange={updateSizes} />
        {maximizedPane && (
          <div style={{ position: 'absolute', inset: 0, zIndex: 10, backgroundColor: '#1a1b1e' }}>
            <TerminalPane pane={maximizedPane} />
          </div>
        )}
      </div>
    </LayoutActionsContext.Provider>
  )
}
