import React from 'react'
import { DisplayConfig, PaneConfig } from '../types'
import { GitInfo } from '../schemas'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface PaneHeaderProps {
  pane: PaneConfig
  connected: boolean
  displayConfig: DisplayConfig
  isMaximized: boolean
  editMode: boolean
  gitInfo?: GitInfo
  onSplit: (direction: 'horizontal' | 'vertical') => void
  onClose: () => void
  onMaximize: () => void
  onSettings: () => void
  onOpenVSCode?: () => void
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
  fontSize: '16px',
  padding: '3px 5px',
  lineHeight: '1',
  borderRadius: '3px',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  minWidth: '22px',
  minHeight: '22px',
}

export const PaneHeader: React.FC<PaneHeaderProps> = ({
  pane,
  connected,
  displayConfig,
  isMaximized,
  editMode,
  gitInfo,
  onSplit,
  onClose,
  onMaximize,
  onSettings,
  onOpenVSCode,
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
        // Shift header toward a blue-gray tint in edit mode for clear mode indication
        backgroundColor: editMode ? '#1d2b3a' : '#252526',
        borderBottom: editMode ? '1px solid #2a3f55' : '1px solid #333',
        userSelect: 'none',
        flexShrink: 0,
        cursor: editMode ? 'grab' : 'default',
        transition: 'background-color 0.2s ease, border-color 0.2s ease',
      }}
    >
      {editMode && (
        <span
          title="Drag to move pane"
          style={{ color: '#4a7ea5', fontSize: '13px', lineHeight: '1', flexShrink: 0 }}
        >
          ⠿
        </span>
      )}
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
      {gitInfo?.is_git && (gitInfo.repo || gitInfo.branch) && (
        <span style={{ color: '#6e8a6e', fontSize: '11px' }}>
          {gitInfo.repo && <span>{gitInfo.repo}</span>}
          {gitInfo.repo && gitInfo.branch && <span style={{ color: '#4a6a4a' }}>{' '}⎇{' '}</span>}
          {gitInfo.branch && <span>{gitInfo.branch}</span>}
        </span>
      )}
      {!connected && <span style={{ color: '#555' }}>reconnecting…</span>}
      <div style={{ marginLeft: 'auto', display: 'flex', gap: '4px' }}>
        {editMode && (
          <button
            title="Pane settings"
            onClick={onSettings}
            style={buttonStyle}
          >
            ⚙
          </button>
        )}
        {connected && onOpenVSCode && (
          <button
            title="Open in VSCode"
            onClick={onOpenVSCode}
            style={buttonStyle}
          >
            {'</>'}
          </button>
        )}
        <button
          title={isMaximized ? 'Restore' : 'Maximize'}
          onClick={onMaximize}
          style={buttonStyle}
        >
          {isMaximized ? '⤡' : '⤢'}
        </button>
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
