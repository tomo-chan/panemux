import React from 'react'
import { DisplayConfig, PaneConfig } from '../types'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface PaneHeaderProps {
  pane: PaneConfig
  connected: boolean
  displayConfig: DisplayConfig
  onSplit: (direction: 'horizontal' | 'vertical') => void
  onClose: () => void
}

const TYPE_COLORS: Record<string, string> = {
  local: '#6a9955',
  ssh: '#569cd6',
  tmux: '#dcdcaa',
  ssh_tmux: '#c586c0',
}

const TYPE_LABELS: Record<string, string> = {
  local: 'LOCAL',
  ssh: 'SSH',
  tmux: 'TMUX',
  ssh_tmux: 'SSH+TMUX',
}

const buttonStyle: React.CSSProperties = {
  background: 'none',
  border: 'none',
  color: '#888',
  cursor: 'pointer',
  fontSize: '11px',
  padding: '0 3px',
  lineHeight: '1',
}

export const PaneHeader: React.FC<PaneHeaderProps> = ({
  pane,
  connected,
  displayConfig,
  onSplit,
  onClose,
}) => {
  const showHeader = pane.show_header ?? displayConfig.show_header
  if (!showHeader) return null

  const color = TYPE_COLORS[pane.type] ?? '#888'
  const label = TYPE_LABELS[pane.type] ?? pane.type.toUpperCase()

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '6px',
        padding: '2px 8px',
        fontSize: '11px',
        fontFamily: TERMINAL_FONT_FAMILY,
        color: '#888',
        backgroundColor: '#252526',
        borderBottom: '1px solid #333',
        userSelect: 'none',
        flexShrink: 0,
      }}
    >
      <span
        style={{
          display: 'inline-block',
          width: '8px',
          height: '8px',
          borderRadius: '50%',
          backgroundColor: connected ? color : '#555',
          flexShrink: 0,
        }}
      />
      <span style={{ color, fontWeight: 600 }}>{label}</span>
      {pane.title && <span style={{ color: '#aaa' }}>{pane.title}</span>}
      {!connected && <span style={{ color: '#555' }}>reconnecting…</span>}
      <div style={{ marginLeft: 'auto', display: 'flex', gap: '2px' }}>
        <button
          title="Split horizontal"
          onClick={() => onSplit('horizontal')}
          style={buttonStyle}
        >
          ⇔
        </button>
        <button
          title="Split vertical"
          onClick={() => onSplit('vertical')}
          style={buttonStyle}
        >
          ⇕
        </button>
        <button
          title="Close pane"
          onClick={onClose}
          style={{ ...buttonStyle, color: '#f44747' }}
        >
          ✕
        </button>
      </div>
    </div>
  )
}
