import React from 'react'
import { DisplayConfig, PaneConfig } from '../types'
import { GitInfo } from '../schemas'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'
import {
  AddPaneBottomIcon,
  AddPaneRightIcon,
  CloseIcon,
  CodeIcon,
  MaximizeIcon,
  RestoreIcon,
  SettingsIcon,
  SplitHorizontalIcon,
  SplitVerticalIcon,
} from './PaneHeaderIcons'

interface PaneHeaderProps {
  pane: PaneConfig
  connected: boolean
  displayConfig: DisplayConfig
  isMaximized: boolean
  isDragging?: boolean
  gitInfo?: GitInfo
  onSplit: (direction: 'horizontal' | 'vertical') => void
  onCreateDefaultPane: (edge: 'right' | 'bottom') => void
  onClose: () => void
  onMaximize: () => void
  onSettings: () => void
  onOpenVSCode?: () => void
  moveHandleProps?: Pick<React.HTMLAttributes<HTMLSpanElement>, 'onDragStart' | 'onDragEnd' | 'onMouseDown'>
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
  appearance: 'none',
  backgroundColor: 'transparent',
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
  transition: 'background-color 0.12s ease, box-shadow 0.12s ease, color 0.12s ease, transform 0.12s ease',
}

const vscodeButtonStyle: React.CSSProperties = {
  ...buttonStyle,
  color: '#007acc',
}

interface HeaderIconButtonProps {
  title: string
  onClick: () => void
  children: React.ReactNode
  style?: React.CSSProperties
}

const HeaderIconButton: React.FC<HeaderIconButtonProps> = ({ title, onClick, children, style }) => {
  const [hovered, setHovered] = React.useState(false)
  const [pressed, setPressed] = React.useState(false)
  const mergedStyle = style ?? buttonStyle
  const baseColor = mergedStyle.color ?? buttonStyle.color

  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      onClick={onClick}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => {
        setHovered(false)
        setPressed(false)
      }}
      onMouseDown={(event) => {
        if (event.button !== 0) return
        setPressed(true)
      }}
      onMouseUp={() => setPressed(false)}
      onBlur={() => setPressed(false)}
      style={{
        ...mergedStyle,
        backgroundColor: pressed ? 'rgba(255, 255, 255, 0.12)' : hovered ? 'rgba(255, 255, 255, 0.07)' : mergedStyle.backgroundColor,
        boxShadow: pressed ? 'inset 0 1px 2px rgba(0, 0, 0, 0.45)' : hovered ? 'inset 0 0 0 1px rgba(255, 255, 255, 0.06)' : 'none',
        color: pressed ? '#ffffff' : hovered ? '#d7dce5' : baseColor,
        transform: pressed ? 'translateY(1px)' : 'translateY(0)',
      }}
    >
      {children}
    </button>
  )
}

export const PaneHeader: React.FC<PaneHeaderProps> = ({
  pane,
  connected,
  displayConfig,
  isMaximized,
  isDragging = false,
  gitInfo,
  onSplit,
  onCreateDefaultPane,
  onClose,
  onMaximize,
  onSettings,
  onOpenVSCode,
  moveHandleProps,
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
        cursor: 'default',
      }}
    >
      <span
        title="Drag to move pane"
        draggable={Boolean(moveHandleProps)}
        onDragStart={moveHandleProps?.onDragStart}
        onDragEnd={moveHandleProps?.onDragEnd}
        onMouseDown={moveHandleProps?.onMouseDown}
        style={{
          color: '#4a7ea5',
          fontSize: '13px',
          lineHeight: '1',
          flexShrink: 0,
          cursor: moveHandleProps ? (isDragging ? 'grabbing' : 'grab') : 'default',
          userSelect: 'none',
          opacity: isDragging ? 0.85 : 1,
        }}
      >
        ⠿
      </span>
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
        <HeaderIconButton title="Pane settings" onClick={onSettings}>
          <SettingsIcon />
        </HeaderIconButton>
        {connected && onOpenVSCode && (
          <HeaderIconButton title="Open in VSCode" onClick={onOpenVSCode} style={vscodeButtonStyle}>
            <CodeIcon />
          </HeaderIconButton>
        )}
        <HeaderIconButton title={isMaximized ? 'Restore' : 'Maximize'} onClick={onMaximize}>
          {isMaximized ? <RestoreIcon /> : <MaximizeIcon />}
        </HeaderIconButton>
        <HeaderIconButton title="Split horizontal" onClick={() => onSplit('horizontal')}>
          <SplitHorizontalIcon />
        </HeaderIconButton>
        <HeaderIconButton title="Split vertical" onClick={() => onSplit('vertical')}>
          <SplitVerticalIcon />
        </HeaderIconButton>
        <HeaderIconButton title="Add new pane to the right" onClick={() => onCreateDefaultPane('right')}>
          <AddPaneRightIcon />
        </HeaderIconButton>
        <HeaderIconButton title="Add new pane below" onClick={() => onCreateDefaultPane('bottom')}>
          <AddPaneBottomIcon />
        </HeaderIconButton>
        <HeaderIconButton title="Close pane" onClick={onClose} style={{ ...buttonStyle, color: '#f44747' }}>
          <CloseIcon />
        </HeaderIconButton>
      </div>
    </div>
  )
}
