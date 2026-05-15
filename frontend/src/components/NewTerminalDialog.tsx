import React, { useEffect, useMemo, useState } from 'react'
import type { PaneConfig } from '../schemas'
import type { PanePlacement } from '../hooks/useLayout'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'
import { generateTmuxSessionName } from '../utils/layoutTree'

interface NewTerminalDialogProps {
  isOpen: boolean
  panes: PaneConfig[]
  sshConnectionNames: string[]
  saveError: string | null
  isSaving: boolean
  onSave: (pane: Omit<PaneConfig, 'id'>, placement: PanePlacement) => Promise<void>
  onClose: () => void
  onAddSSHHost: () => void
  onDetectShell: (type: PaneConfig['type'], connection?: string) => Promise<string>
}

const PANE_TYPES: Array<{ value: PaneConfig['type']; label: string }> = [
  { value: 'local', label: 'Local' },
  { value: 'ssh', label: 'SSH' },
  { value: 'tmux', label: 'Tmux (local)' },
  { value: 'ssh_tmux', label: 'SSH + Tmux' },
]

const EDGE_OPTIONS = [
  { value: 'top', label: 'Top' },
  { value: 'bottom', label: 'Bottom' },
  { value: 'left', label: 'Left' },
  { value: 'right', label: 'Right' },
] as const

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '5px 8px',
  backgroundColor: '#3c3c3c',
  color: '#d4d4d4',
  border: '1px solid #555',
  borderRadius: '3px',
  fontFamily: TERMINAL_FONT_FAMILY,
  fontSize: '13px',
  boxSizing: 'border-box',
}

const labelStyle: React.CSSProperties = {
  display: 'block',
  fontSize: '11px',
  color: '#888',
  marginBottom: '4px',
  fontFamily: TERMINAL_FONT_FAMILY,
}

const fieldStyle: React.CSSProperties = {
  marginBottom: '12px',
}

export const NewTerminalDialog: React.FC<NewTerminalDialogProps> = ({
  isOpen,
  panes,
  sshConnectionNames,
  saveError,
  isSaving,
  onSave,
  onClose,
  onAddSSHHost,
  onDetectShell,
}) => {
  const defaultBasePaneId = panes[0]?.id ?? ''
  const [baseMode, setBaseMode] = useState<'blank' | 'existing'>('blank')
  const [basePaneId, setBasePaneId] = useState(defaultBasePaneId)
  const [placementMode, setPlacementMode] = useState<'workspace-edge' | 'pane-edge'>('workspace-edge')
  const [workspaceEdge, setWorkspaceEdge] = useState<PanePlacement['edge']>('right')
  const [targetPaneId, setTargetPaneId] = useState(defaultBasePaneId)
  const [targetEdge, setTargetEdge] = useState<PanePlacement['edge']>('right')
  const [type, setType] = useState<PaneConfig['type']>('local')
  const [shell, setShell] = useState('')
  const [connection, setConnection] = useState('')
  const [tmuxSession, setTmuxSession] = useState('')
  const [cwd, setCwd] = useState('')
  const [title, setTitle] = useState('')
  const [validationError, setValidationError] = useState<string | null>(null)
  const [isDetecting, setIsDetecting] = useState(false)

  const basePane = useMemo(
    () => panes.find((pane) => pane.id === basePaneId) ?? null,
    [basePaneId, panes],
  )

  useEffect(() => {
    if (!isOpen) return
    setBaseMode('blank')
    setBasePaneId(defaultBasePaneId)
    setPlacementMode('workspace-edge')
    setWorkspaceEdge('right')
    setTargetPaneId(defaultBasePaneId)
    setTargetEdge('right')
    setType('local')
    setShell('')
    setConnection('')
    setTmuxSession('')
    setCwd('')
    setTitle('')
    setValidationError(null)
  }, [defaultBasePaneId, isOpen])

  useEffect(() => {
    if (!isOpen || isSaving) return
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, isSaving, onClose])

  useEffect(() => {
    if (!isOpen) return
    if (baseMode === 'blank') {
      setType('local')
      setShell('')
      setConnection('')
      setTmuxSession('')
      setCwd('')
      setTitle('')
      return
    }
    if (!basePane) return
    setType(basePane.type)
    setShell(basePane.shell ?? '')
    setConnection(basePane.connection ?? '')
    setTmuxSession(
      basePane.type === 'tmux' || basePane.type === 'ssh_tmux'
        ? generateTmuxSessionName(basePane.tmux_session ?? 'session')
        : '',
    )
    setCwd(basePane.cwd ?? '')
    setTitle(basePane.title ?? '')
  }, [baseMode, basePane, isOpen])

  if (!isOpen) return null

  const needsConnection = type === 'ssh' || type === 'ssh_tmux'
  const needsTmux = type === 'tmux' || type === 'ssh_tmux'
  const needsShell = type === 'local' || type === 'ssh' || type === 'ssh_tmux'
  const error = validationError ?? saveError

  const handleDetectShell = async () => {
    setIsDetecting(true)
    try {
      const detected = await onDetectShell(type, connection || undefined)
      setShell(detected)
    } catch {
      // User can still type manually.
    } finally {
      setIsDetecting(false)
    }
  }

  const handleSave = async () => {
    setValidationError(null)
    if (placementMode === 'pane-edge' && !targetPaneId) {
      setValidationError('Target pane is required.')
      return
    }
    if (needsConnection && !connection) {
      setValidationError('Connection is required for SSH panes.')
      return
    }
    if (needsTmux && !tmuxSession) {
      setValidationError('Tmux session name is required.')
      return
    }

    const pane: Omit<PaneConfig, 'id'> = {
      type,
      ...(title ? { title } : {}),
      ...(cwd ? { cwd } : {}),
      ...(needsShell && shell ? { shell } : {}),
      ...(needsConnection ? { connection } : {}),
      ...(needsTmux ? { tmux_session: tmuxSession } : {}),
      ...(basePane?.show_header !== undefined ? { show_header: basePane.show_header } : {}),
      ...(basePane?.show_status_bar !== undefined ? { show_status_bar: basePane.show_status_bar } : {}),
    }

    const placement: PanePlacement = placementMode === 'workspace-edge'
      ? { type: 'workspace-edge', edge: workspaceEdge }
      : { type: 'pane-edge', targetPaneId, edge: targetEdge }
    await onSave(pane, placement)
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Add terminal"
      style={{
        position: 'fixed',
        inset: 0,
        backgroundColor: 'rgba(0, 0, 0, 0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1200,
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget && !isSaving) onClose()
      }}
    >
      <div
        style={{
          backgroundColor: '#252526',
          border: '1px solid #444',
          borderRadius: '6px',
          padding: '20px 24px',
          width: '420px',
          maxWidth: 'calc(100vw - 32px)',
          maxHeight: 'calc(100vh - 32px)',
          overflowY: 'auto',
          boxSizing: 'border-box',
          fontFamily: TERMINAL_FONT_FAMILY,
          color: '#d4d4d4',
        }}
      >
        <div style={{ fontSize: '14px', fontWeight: 600, marginBottom: '16px', color: '#e0e0e0' }}>
          Add Terminal
        </div>

        <div style={fieldStyle}>
          <label style={labelStyle}>Base Settings</label>
          <div style={{ display: 'flex', gap: '8px', marginBottom: '8px' }}>
            <button type="button" onClick={() => setBaseMode('blank')} style={modeButtonStyle(baseMode === 'blank')}>Blank Local</button>
            <button type="button" onClick={() => setBaseMode('existing')} style={modeButtonStyle(baseMode === 'existing')}>Clone Existing</button>
          </div>
          {baseMode === 'existing' && (
            <select value={basePaneId} onChange={(e) => setBasePaneId(e.target.value)} style={inputStyle}>
              {panes.map((pane) => (
                <option key={pane.id} value={pane.id}>{pane.title ? `${pane.title} (${pane.id})` : pane.id}</option>
              ))}
            </select>
          )}
        </div>

        <div style={fieldStyle}>
          <label style={labelStyle}>Placement</label>
          <div style={{ display: 'flex', gap: '8px', marginBottom: '8px' }}>
            <button type="button" onClick={() => setPlacementMode('workspace-edge')} style={modeButtonStyle(placementMode === 'workspace-edge')}>Workspace Edge</button>
            <button type="button" onClick={() => setPlacementMode('pane-edge')} style={modeButtonStyle(placementMode === 'pane-edge')}>Beside Pane</button>
          </div>
          {placementMode === 'workspace-edge' ? (
            <select value={workspaceEdge} onChange={(e) => setWorkspaceEdge(e.target.value as PanePlacement['edge'])} style={inputStyle}>
              {EDGE_OPTIONS.map((edge) => <option key={edge.value} value={edge.value}>{edge.label}</option>)}
            </select>
          ) : (
            <div style={{ display: 'grid', gap: '8px' }}>
              <select value={targetPaneId} onChange={(e) => setTargetPaneId(e.target.value)} style={inputStyle}>
                {panes.map((pane) => (
                  <option key={pane.id} value={pane.id}>{pane.title ? `${pane.title} (${pane.id})` : pane.id}</option>
                ))}
              </select>
              <select value={targetEdge} onChange={(e) => setTargetEdge(e.target.value as PanePlacement['edge'])} style={inputStyle}>
                {EDGE_OPTIONS.map((edge) => <option key={edge.value} value={edge.value}>{edge.label}</option>)}
              </select>
            </div>
          )}
        </div>

        <div style={fieldStyle}>
          <label style={labelStyle}>Type</label>
          <select
            value={type}
            onChange={(e) => setType(e.target.value as PaneConfig['type'])}
            style={inputStyle}
          >
            {PANE_TYPES.map((paneType) => (
              <option key={paneType.value} value={paneType.value}>{paneType.label}</option>
            ))}
          </select>
        </div>

        {needsShell && (
          <div style={fieldStyle}>
            <label style={labelStyle}>Shell</label>
            <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
              <input
                type="text"
                value={shell}
                onChange={(e) => setShell(e.target.value)}
                placeholder="/bin/zsh"
                style={{ ...inputStyle, flex: 1 }}
              />
              <button
                type="button"
                onClick={handleDetectShell}
                disabled={isDetecting || (needsConnection && !connection)}
                style={secondaryButtonStyle(isDetecting || (needsConnection && !connection))}
              >
                {isDetecting ? '…' : 'Detect'}
              </button>
            </div>
          </div>
        )}

        {needsConnection && (
          <div style={fieldStyle}>
            <label style={labelStyle}>Connection</label>
            <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
              <select value={connection} onChange={(e) => setConnection(e.target.value)} style={{ ...inputStyle, flex: 1 }}>
                <option value="">— select connection —</option>
                {sshConnectionNames.map((name) => (
                  <option key={name} value={name}>{name}</option>
                ))}
              </select>
              <button type="button" onClick={onAddSSHHost} style={secondaryButtonStyle(false)}>+ Add</button>
            </div>
          </div>
        )}

        {needsTmux && (
          <div style={fieldStyle}>
            <label style={labelStyle}>Tmux Session</label>
            <input
              type="text"
              value={tmuxSession}
              onChange={(e) => setTmuxSession(e.target.value)}
              placeholder="session-name"
              style={inputStyle}
            />
          </div>
        )}

        <div style={fieldStyle}>
          <label style={labelStyle}>Working Directory</label>
          <input
            type="text"
            value={cwd}
            onChange={(e) => setCwd(e.target.value)}
            placeholder="~/project"
            style={inputStyle}
          />
        </div>

        <div style={fieldStyle}>
          <label style={labelStyle}>Title</label>
          <input
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Terminal"
            style={inputStyle}
          />
        </div>

        {error && (
          <div style={{ fontSize: '12px', color: '#f44747', marginBottom: '12px' }}>
            {error}
          </div>
        )}

        <div style={{ display: 'flex', gap: '8px', justifyContent: 'flex-end' }}>
          <button type="button" onClick={onClose} disabled={isSaving} style={secondaryButtonStyle(isSaving)}>
            Cancel
          </button>
          <button
            type="button"
            onClick={handleSave}
            disabled={isSaving}
            style={{
              padding: '5px 14px',
              backgroundColor: '#0e639c',
              color: '#fff',
              border: 'none',
              borderRadius: '3px',
              fontFamily: TERMINAL_FONT_FAMILY,
              fontSize: '13px',
              cursor: 'pointer',
              opacity: isSaving ? 0.6 : 1,
            }}
          >
            {isSaving ? 'Creating…' : 'Create'}
          </button>
        </div>
      </div>
    </div>
  )
}

function modeButtonStyle(active: boolean): React.CSSProperties {
  return {
    padding: '5px 10px',
    backgroundColor: active ? '#1f3b53' : 'transparent',
    color: active ? '#d7ecff' : '#9aa6b2',
    border: `1px solid ${active ? '#3d6b8f' : '#555'}`,
    borderRadius: '3px',
    fontFamily: TERMINAL_FONT_FAMILY,
    fontSize: '12px',
    cursor: 'pointer',
  }
}

function secondaryButtonStyle(disabled: boolean): React.CSSProperties {
  return {
    padding: '5px 10px',
    backgroundColor: 'transparent',
    color: '#888',
    border: '1px solid #555',
    borderRadius: '3px',
    fontFamily: TERMINAL_FONT_FAMILY,
    fontSize: '13px',
    cursor: disabled ? 'default' : 'pointer',
    whiteSpace: 'nowrap',
    opacity: disabled ? 0.5 : 1,
  }
}
