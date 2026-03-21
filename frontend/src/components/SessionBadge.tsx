import React from 'react'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface SessionBadgeProps {
  type: string
  title?: string
  connected: boolean
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

export const SessionBadge: React.FC<SessionBadgeProps> = ({ type, title, connected }) => {
  const color = TYPE_COLORS[type] ?? '#888'
  const label = TYPE_LABELS[type] ?? type.toUpperCase()

  return (
    <div style={{
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
    }}>
      <span style={{
        display: 'inline-block',
        width: '8px',
        height: '8px',
        borderRadius: '50%',
        backgroundColor: connected ? color : '#555',
        flexShrink: 0,
      }} />
      <span style={{ color, fontWeight: 600 }}>{label}</span>
      {title && <span style={{ color: '#aaa' }}>{title}</span>}
      {!connected && <span style={{ color: '#555', marginLeft: 'auto' }}>reconnecting…</span>}
    </div>
  )
}
