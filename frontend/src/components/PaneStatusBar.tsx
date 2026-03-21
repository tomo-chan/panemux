import React from 'react'
import { DisplayConfig, PaneConfig } from '../types'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface PaneStatusBarProps {
  pane: PaneConfig
  displayConfig: DisplayConfig
  cols?: number
  rows?: number
}

const TYPE_LABELS: Record<string, string> = {
  local: 'LOCAL',
  ssh: 'SSH',
  tmux: 'TMUX',
  ssh_tmux: 'SSH+TMUX',
}

export const PaneStatusBar: React.FC<PaneStatusBarProps> = ({ pane, displayConfig, cols, rows }) => {
  const showStatusBar = pane.show_status_bar ?? displayConfig.show_status_bar
  if (!showStatusBar) return null

  const label = TYPE_LABELS[pane.type] ?? pane.type.toUpperCase()

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        padding: '1px 8px',
        fontSize: '10px',
        fontFamily: TERMINAL_FONT_FAMILY,
        color: '#555',
        backgroundColor: '#252526',
        borderTop: '1px solid #333',
        userSelect: 'none',
        flexShrink: 0,
      }}
    >
      <span>{label}</span>
      {pane.connection && <span>{pane.connection}</span>}
      {cols != null && rows != null && (
        <span style={{ marginLeft: 'auto' }}>
          {cols}×{rows}
        </span>
      )}
    </div>
  )
}
