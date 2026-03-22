import React, { useState, useEffect } from 'react'
import type { SSHConfigHost } from '../schemas'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface AddSSHHostDialogProps {
  isOpen: boolean
  isSaving: boolean
  saveError: string | null
  onSave: (host: SSHConfigHost) => Promise<void>
  onClose: () => void
}

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

export const AddSSHHostDialog: React.FC<AddSSHHostDialogProps> = ({
  isOpen,
  isSaving,
  saveError,
  onSave,
  onClose,
}) => {
  const [name, setName] = useState('')
  const [hostname, setHostname] = useState('')
  const [user, setUser] = useState('')
  const [port, setPort] = useState('')
  const [identityFile, setIdentityFile] = useState('')
  const [validationError, setValidationError] = useState<string | null>(null)

  // Reset form to empty every time dialog opens
  useEffect(() => {
    if (isOpen) {
      setName('')
      setHostname('')
      setUser('')
      setPort('')
      setIdentityFile('')
      setValidationError(null)
    }
  }, [isOpen])

  if (!isOpen) return null

  const handleSave = async () => {
    setValidationError(null)

    if (!name.trim()) {
      setValidationError('Name is required.')
      return
    }
    if (!hostname.trim()) {
      setValidationError('Hostname is required.')
      return
    }
    if (!user.trim()) {
      setValidationError('User is required.')
      return
    }

    let portNum: number | undefined
    if (port.trim() !== '') {
      portNum = parseInt(port, 10)
      if (isNaN(portNum) || portNum < 1 || portNum > 65535) {
        setValidationError('Port must be between 1 and 65535.')
        return
      }
    }

    const host: SSHConfigHost = {
      name: name.trim(),
      hostname: hostname.trim(),
      user: user.trim(),
      ...(portNum !== undefined ? { port: portNum } : {}),
      ...(identityFile.trim() ? { identity_file: identityFile.trim() } : {}),
    }

    await onSave(host)
  }

  const error = validationError ?? saveError

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Add SSH host"
      style={{
        position: 'fixed',
        inset: 0,
        backgroundColor: 'rgba(0, 0, 0, 0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1100,
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
          Add SSH Host
        </div>

        <div style={fieldStyle}>
          <label htmlFor="ssh-host-name" style={labelStyle}>Alias</label>
          <input
            id="ssh-host-name"
            type="text"
            value={name}
            onChange={(e) => { setName(e.target.value); setValidationError(null) }}
            placeholder="prod-web"
            style={inputStyle}
            aria-label="Name"
          />
        </div>

        <div style={fieldStyle}>
          <label htmlFor="ssh-host-hostname" style={labelStyle}>Hostname</label>
          <input
            id="ssh-host-hostname"
            type="text"
            value={hostname}
            onChange={(e) => { setHostname(e.target.value); setValidationError(null) }}
            placeholder="prod.example.com"
            style={inputStyle}
            aria-label="Hostname"
          />
        </div>

        <div style={fieldStyle}>
          <label htmlFor="ssh-host-user" style={labelStyle}>User</label>
          <input
            id="ssh-host-user"
            type="text"
            value={user}
            onChange={(e) => { setUser(e.target.value); setValidationError(null) }}
            placeholder="ubuntu"
            style={inputStyle}
            aria-label="User"
          />
        </div>

        <div style={fieldStyle}>
          <label htmlFor="ssh-host-port" style={labelStyle}>Port (optional)</label>
          <input
            id="ssh-host-port"
            type="number"
            value={port}
            onChange={(e) => { setPort(e.target.value); setValidationError(null) }}
            placeholder="22"
            style={inputStyle}
            aria-label="Port"
          />
        </div>

        <div style={fieldStyle}>
          <label htmlFor="ssh-host-identity-file" style={labelStyle}>Identity File (optional)</label>
          <input
            id="ssh-host-identity-file"
            type="text"
            value={identityFile}
            onChange={(e) => setIdentityFile(e.target.value)}
            placeholder="~/.ssh/id_rsa"
            style={inputStyle}
            aria-label="Identity File"
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
            {isSaving ? 'Saving…' : 'Add'}
          </button>
        </div>
      </div>
    </div>
  )
}
