import React, { useState, useEffect } from 'react'
import type { PaneConfig } from '../types'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface PaneSettingsDialogProps {
  isOpen: boolean
  pane: PaneConfig | null
  sshConnectionNames: string[]
  saveError: string | null
  isSaving: boolean
  onSave: (updated: PaneConfig) => Promise<void>
  onClose: () => void
}

const PANE_TYPES: Array<{ value: PaneConfig['type']; label: string }> = [
  { value: 'local', label: 'Local' },
  { value: 'ssh', label: 'SSH' },
  { value: 'tmux', label: 'Tmux (local)' },
  { value: 'ssh_tmux', label: 'SSH + Tmux' },
]

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

export const PaneSettingsDialog: React.FC<PaneSettingsDialogProps> = ({
  isOpen,
  pane,
  sshConnectionNames,
  saveError,
  isSaving,
  onSave,
  onClose,
}) => {
  const [type, setType] = useState<PaneConfig['type']>('local')
  const [shell, setShell] = useState('')
  const [connection, setConnection] = useState('')
  const [tmuxSession, setTmuxSession] = useState('')
  const [cwd, setCwd] = useState('')
  const [title, setTitle] = useState('')
  const [validationError, setValidationError] = useState<string | null>(null)

  useEffect(() => {
    if (pane) {
      setType(pane.type)
      setShell(pane.shell ?? '')
      setConnection(pane.connection ?? '')
      setTmuxSession(pane.tmux_session ?? '')
      setCwd(pane.cwd ?? '')
      setTitle(pane.title ?? '')
      setValidationError(null)
    }
  }, [pane])

  if (!isOpen || !pane) return null

  const needsConnection = type === 'ssh' || type === 'ssh_tmux'
  const needsTmux = type === 'tmux' || type === 'ssh_tmux'
  const needsShell = type === 'local'

  const handleSave = async () => {
    setValidationError(null)
    if (needsConnection && !connection) {
      setValidationError('Connection is required for SSH panes.')
      return
    }
    if (needsTmux && !tmuxSession) {
      setValidationError('Tmux session name is required.')
      return
    }

    const updated: PaneConfig = {
      id: pane.id,
      type,
      title: title || undefined,
      cwd: cwd || undefined,
      show_header: pane.show_header,
      show_status_bar: pane.show_status_bar,
      ...(needsShell ? { shell: shell || undefined } : {}),
      ...(needsConnection ? { connection } : {}),
      ...(needsTmux ? { tmux_session: tmuxSession } : {}),
    }
    await onSave(updated)
  }

  const error = validationError ?? saveError

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Pane settings"
      style={{
        position: 'fixed',
        inset: 0,
        backgroundColor: 'rgba(0, 0, 0, 0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        style={{
          backgroundColor: '#252526',
          border: '1px solid #444',
          borderRadius: '6px',
          padding: '20px 24px',
          width: '360px',
          fontFamily: TERMINAL_FONT_FAMILY,
          color: '#d4d4d4',
        }}
      >
        <div style={{ fontSize: '14px', fontWeight: 600, marginBottom: '16px', color: '#e0e0e0' }}>
          Pane Settings
          <span style={{ fontSize: '11px', color: '#666', marginLeft: '8px' }}>({pane.id})</span>
        </div>

        <div style={fieldStyle}>
          <label style={labelStyle}>Type</label>
          <select
            value={type}
            onChange={(e) => { setType(e.target.value as PaneConfig['type']); setValidationError(null) }}
            style={inputStyle}
          >
            {PANE_TYPES.map((t) => (
              <option key={t.value} value={t.value}>{t.label}</option>
            ))}
          </select>
        </div>

        {needsShell && (
          <div style={fieldStyle}>
            <label style={labelStyle}>Shell</label>
            <input
              type="text"
              value={shell}
              onChange={(e) => setShell(e.target.value)}
              placeholder="/bin/zsh"
              style={inputStyle}
            />
          </div>
        )}

        {needsConnection && (
          <div style={fieldStyle}>
            <label style={labelStyle}>Connection</label>
            {sshConnectionNames.length === 0 ? (
              <div style={{ fontSize: '12px', color: '#666', fontStyle: 'italic' }}>
                No SSH connections configured in config.yaml
              </div>
            ) : (
              <select
                value={connection}
                onChange={(e) => { setConnection(e.target.value); setValidationError(null) }}
                style={inputStyle}
              >
                <option value="">— select connection —</option>
                {sshConnectionNames.map((name) => (
                  <option key={name} value={name}>{name}</option>
                ))}
              </select>
            )}
          </div>
        )}

        {needsTmux && (
          <div style={fieldStyle}>
            <label style={labelStyle}>Tmux Session</label>
            <input
              type="text"
              value={tmuxSession}
              onChange={(e) => { setTmuxSession(e.target.value); setValidationError(null) }}
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
            placeholder="~/projects/myapp"
            style={inputStyle}
          />
        </div>

        <div style={fieldStyle}>
          <label style={labelStyle}>Title</label>
          <input
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="My Terminal"
            style={inputStyle}
          />
        </div>

        {error && (
          <div style={{ fontSize: '12px', color: '#f44747', marginBottom: '12px' }}>
            {error}
          </div>
        )}

        <div style={{ display: 'flex', gap: '8px', justifyContent: 'flex-end' }}>
          <button
            onClick={onClose}
            disabled={isSaving}
            style={{
              padding: '5px 14px',
              backgroundColor: 'transparent',
              color: '#888',
              border: '1px solid #555',
              borderRadius: '3px',
              fontFamily: TERMINAL_FONT_FAMILY,
              fontSize: '13px',
              cursor: 'pointer',
            }}
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={isSaving || (needsConnection && sshConnectionNames.length === 0)}
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
            {isSaving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}
