import React, { useEffect, useState } from 'react'
import type { DirectoryBrowserResponse, PaneConfig } from '../types'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface PaneSettingsDialogProps {
  isOpen: boolean
  pane: PaneConfig | null
  sshConnectionNames: string[]
  saveError: string | null
  isSaving: boolean
  onSave: (updated: PaneConfig) => Promise<void>
  onClose: () => void
  onAddSSHHost: () => void
  onDetectShell: (type: PaneConfig['type'], connection?: string) => Promise<string>
  onBrowseDirectories: (
    type: PaneConfig['type'],
    connection?: string,
    path?: string,
    showHidden?: boolean,
  ) => Promise<DirectoryBrowserResponse>
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

const explorerButtonStyle: React.CSSProperties = {
  padding: 0,
  backgroundColor: 'transparent',
  color: '#d4d4d4',
  border: 'none',
  fontFamily: TERMINAL_FONT_FAMILY,
  fontSize: '12px',
  cursor: 'pointer',
}

export const PaneSettingsDialog: React.FC<PaneSettingsDialogProps> = ({
  isOpen,
  pane,
  sshConnectionNames,
  saveError,
  isSaving,
  onSave,
  onClose,
  onAddSSHHost,
  onDetectShell,
  onBrowseDirectories,
}) => {
  const [type, setType] = useState<PaneConfig['type']>('local')
  const [shell, setShell] = useState('')
  const [connection, setConnection] = useState('')
  const [tmuxSession, setTmuxSession] = useState('')
  const [cwd, setCwd] = useState('')
  const [title, setTitle] = useState('')
  const [validationError, setValidationError] = useState<string | null>(null)
  const [isDetecting, setIsDetecting] = useState(false)
  const [showDirectoryBrowser, setShowDirectoryBrowser] = useState(false)
  const [directoryResponses, setDirectoryResponses] = useState<Record<string, DirectoryBrowserResponse>>({})
  const [expandedDirectories, setExpandedDirectories] = useState<Record<string, boolean>>({})
  const [browserPath, setBrowserPath] = useState('')
  const [browserInputPath, setBrowserInputPath] = useState('')
  const [browserSelection, setBrowserSelection] = useState('')
  const [browseError, setBrowseError] = useState<string | null>(null)
  const [isBrowsingDirectories, setIsBrowsingDirectories] = useState(false)
  const [showHiddenDirectories, setShowHiddenDirectories] = useState(false)

  useEffect(() => {
    if (pane) {
      setType(pane.type)
      const existingShell = pane.shell ?? ''
      setShell(existingShell)
      setConnection(pane.connection ?? '')
      setTmuxSession(pane.tmux_session ?? '')
      setCwd(pane.cwd ?? '')
      setTitle(pane.title ?? '')
      setValidationError(null)
      resetDirectoryBrowser()
      if (pane.type === 'local' && !existingShell) {
        onDetectShell('local').then(setShell).catch(() => {})
      }
    }
  }, [pane, onDetectShell])

  useEffect(() => {
    if (!isOpen || !pane || isSaving) return
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        if (showDirectoryBrowser) {
          setShowDirectoryBrowser(false)
          return
        }
        onClose()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, isSaving, onClose, pane, showDirectoryBrowser])

  if (!isOpen || !pane) return null

  const needsConnection = type === 'ssh' || type === 'ssh_tmux'
  const needsTmux = type === 'tmux' || type === 'ssh_tmux'
  const needsShell = type === 'local' || type === 'ssh' || type === 'ssh_tmux'
  const canBrowseDirectories = !needsConnection || connection !== ''

  function resetDirectoryBrowser() {
    setShowDirectoryBrowser(false)
    setDirectoryResponses({})
    setExpandedDirectories({})
    setBrowserPath('')
    setBrowserInputPath('')
    setBrowserSelection('')
    setBrowseError(null)
    setIsBrowsingDirectories(false)
    setShowHiddenDirectories(false)
  }

  const handleDetectShell = async () => {
    setIsDetecting(true)
    try {
      const detected = await onDetectShell(type, connection || undefined)
      setShell(detected)
    } catch {
      // ignore: user can type manually
    } finally {
      setIsDetecting(false)
    }
  }

  const browseDirectory = async (path: string) => {
    setIsBrowsingDirectories(true)
    setBrowseError(null)
    try {
      const response = await onBrowseDirectories(
        type,
        needsConnection ? connection || undefined : undefined,
        path,
        showHiddenDirectories,
      )
      setDirectoryResponses({ [response.path]: response })
      setExpandedDirectories({ [response.path]: true })
      setBrowserPath(response.path)
      setBrowserInputPath(response.path)
      setBrowserSelection(response.path)
      return response
    } catch (err) {
      setBrowseError(err instanceof Error ? err.message : 'Failed to browse directories')
      return null
    } finally {
      setIsBrowsingDirectories(false)
    }
  }

  const openDirectoryBrowser = async () => {
    setShowDirectoryBrowser(true)
    await browseDirectory(cwd)
  }

  const toggleDirectory = async (path: string, hasChildren: boolean) => {
    if (!hasChildren) return
    if (!expandedDirectories[path]) {
      setIsBrowsingDirectories(true)
      setBrowseError(null)
      try {
        const response = await onBrowseDirectories(
          type,
          needsConnection ? connection || undefined : undefined,
          path,
          showHiddenDirectories,
        )
        setDirectoryResponses((current) => ({ ...current, [response.path]: response }))
      } catch (err) {
        setBrowseError(err instanceof Error ? err.message : 'Failed to browse directories')
        setIsBrowsingDirectories(false)
        return
      } finally {
        setIsBrowsingDirectories(false)
      }
    }
    setExpandedDirectories((current) => ({ ...current, [path]: !current[path] }))
  }

  const handleToggleHiddenDirectories = async (nextValue: boolean) => {
    setShowHiddenDirectories(nextValue)
    if (!showDirectoryBrowser) return
    setIsBrowsingDirectories(true)
    setBrowseError(null)
    try {
      const response = await onBrowseDirectories(
        type,
        needsConnection ? connection || undefined : undefined,
        browserPath,
        nextValue,
      )
      setDirectoryResponses({ [response.path]: response })
      setExpandedDirectories({ [response.path]: true })
      setBrowserPath(response.path)
      setBrowserInputPath(response.path)
      if (browserSelection === '' || browserSelection === browserPath) {
        setBrowserSelection(response.path)
      }
    } catch (err) {
      setBrowseError(err instanceof Error ? err.message : 'Failed to browse directories')
    } finally {
      setIsBrowsingDirectories(false)
    }
  }

  const applyDirectorySelection = () => {
    setCwd(browserSelection)
    setShowDirectoryBrowser(false)
  }

  const handleGoToParent = async () => {
    const parent = parentDirectory(browserPath)
    if (parent === browserPath) return
    await browseDirectory(parent)
  }

  const renderDirectoryEntries = (path: string, depth = 0): React.ReactNode => {
    const response = directoryResponses[path]
    if (!response) return null

    return response.entries.map((entry) => {
      const isSelected = browserSelection === entry.path
      return (
        <div key={entry.path}>
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              paddingLeft: `${depth * 14}px`,
              borderRadius: '4px',
              backgroundColor: isSelected ? '#04395e' : 'transparent',
            }}
          >
            <button
              type="button"
              aria-label={`toggle ${entry.path}`}
              onClick={(event) => {
                event.stopPropagation()
                void toggleDirectory(entry.path, entry.has_children)
              }}
              disabled={!entry.has_children}
              style={{
                ...explorerButtonStyle,
                width: '24px',
                height: '24px',
                fontSize: '15px',
                lineHeight: '24px',
                color: entry.has_children ? '#e5e7eb' : '#666666',
                cursor: entry.has_children ? 'pointer' : 'default',
              }}
            >
              {entry.has_children ? (expandedDirectories[entry.path] ? '▾' : '▸') : ''}
            </button>
            <button
              type="button"
              aria-label={entry.path}
              onClick={() => {
                setBrowserSelection(entry.path)
                if (entry.has_children) {
                  void toggleDirectory(entry.path, entry.has_children)
                }
              }}
              onDoubleClick={() => {
                setBrowserSelection(entry.path)
                setCwd(entry.path)
                setShowDirectoryBrowser(false)
              }}
              style={{
                ...explorerButtonStyle,
                flex: 1,
                minHeight: '28px',
                padding: '4px 6px',
                textAlign: 'left',
                color: isSelected ? '#ffffff' : '#d4d4d4',
              }}
            >
              <span
                aria-hidden="true"
                style={{
                  display: 'inline-block',
                  width: '16px',
                  marginRight: '6px',
                  color: '#d7ba7d',
                  fontSize: '14px',
                  textAlign: 'center',
                }}
              >
                ●
              </span>
              {entry.name}
            </button>
          </div>
          {expandedDirectories[entry.path] ? renderDirectoryEntries(entry.path, depth + 1) : null}
        </div>
      )
    })
  }

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
          width: '360px',
          maxWidth: 'calc(100vw - 32px)',
          maxHeight: 'calc(100vh - 32px)',
          overflowY: 'auto',
          boxSizing: 'border-box',
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
            onChange={(e) => {
              const newType = e.target.value as PaneConfig['type']
              setType(newType)
              setValidationError(null)
              resetDirectoryBrowser()
              if (newType === 'local' && !shell) {
                onDetectShell(newType).then(setShell).catch(() => {})
              }
            }}
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
            <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
              <input
                type="text"
                value={shell}
                onChange={(e) => setShell(e.target.value)}
                placeholder="/bin/bash"
                style={{ ...inputStyle, flex: 1 }}
              />
              <button
                type="button"
                onClick={handleDetectShell}
                disabled={isDetecting || (needsConnection && !connection)}
                style={actionButtonStyle(isDetecting || (needsConnection && !connection))}
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
              <select
                value={connection}
                onChange={(e) => {
                  setConnection(e.target.value)
                  setValidationError(null)
                  resetDirectoryBrowser()
                }}
                style={{ ...inputStyle, flex: 1 }}
              >
                <option value="">— select connection —</option>
                {sshConnectionNames.map((name) => (
                  <option key={name} value={name}>{name}</option>
                ))}
              </select>
              <button
                type="button"
                onClick={onAddSSHHost}
                style={actionButtonStyle(false)}
              >
                + Add
              </button>
            </div>
            <div style={{ fontSize: '11px', color: '#555', marginTop: '4px' }}>
              Edit connections in ~/.ssh/config
            </div>
          </div>
        )}

        {needsTmux && (
          <div style={fieldStyle}>
            <label style={labelStyle}>Tmux Session</label>
            <input
              type="text"
              value={tmuxSession}
              onChange={(e) => {
                setTmuxSession(e.target.value)
                setValidationError(null)
              }}
              placeholder="session-name"
              style={inputStyle}
            />
          </div>
        )}

        <div style={fieldStyle}>
          <label htmlFor="pane-working-directory" style={labelStyle}>Working Directory</label>
          <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
            <input
              id="pane-working-directory"
              aria-label="Working Directory"
              type="text"
              value={cwd}
              onChange={(e) => setCwd(e.target.value)}
              placeholder="~/projects/myapp"
              style={{ ...inputStyle, flex: 1 }}
            />
            <button
              type="button"
              onClick={() => {
                void openDirectoryBrowser()
              }}
              disabled={!canBrowseDirectories || isBrowsingDirectories}
              style={actionButtonStyle(!canBrowseDirectories || isBrowsingDirectories)}
            >
              Browse
            </button>
          </div>
          {needsConnection && !connection && (
            <div style={{ fontSize: '11px', color: '#888', marginTop: '4px' }}>
              Select an SSH connection to browse remote directories.
            </div>
          )}
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
            type="button"
            onClick={onClose}
            disabled={isSaving}
            style={secondaryButtonStyle}
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => {
              void handleSave()
            }}
            disabled={isSaving || (needsConnection && !connection)}
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

      {showDirectoryBrowser && (
        <div
          role="dialog"
          aria-modal="true"
          aria-label="Directory browser"
          style={{
            position: 'fixed',
            inset: 0,
            backgroundColor: 'rgba(0, 0, 0, 0.45)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 1100,
          }}
          onClick={(e) => {
            if (e.target === e.currentTarget) setShowDirectoryBrowser(false)
          }}
        >
          <div
            style={{
              width: '560px',
              maxWidth: 'calc(100vw - 32px)',
              maxHeight: 'calc(100vh - 48px)',
              display: 'flex',
              flexDirection: 'column',
              backgroundColor: '#1e1e1e',
              border: '1px solid #3f3f46',
              borderRadius: '8px',
              boxShadow: '0 20px 60px rgba(0, 0, 0, 0.45)',
              overflow: 'hidden',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div style={{ padding: '14px 16px', borderBottom: '1px solid #333', fontSize: '13px', fontWeight: 600 }}>
              Select Working Directory
            </div>

            <div style={{ padding: '12px 16px', borderBottom: '1px solid #333' }}>
              <div style={{ display: 'flex', gap: '8px', alignItems: 'center', marginBottom: '10px' }}>
                <button
                  type="button"
                  aria-label="Go to parent directory"
                  onClick={() => {
                    void handleGoToParent()
                  }}
                  disabled={isBrowsingDirectories || parentDirectory(browserPath) === browserPath}
                  style={secondaryButtonStyle}
                >
                  ↑ Up
                </button>
                <button
                  type="button"
                  aria-label="Reload current directory"
                  onClick={() => {
                    void browseDirectory(browserPath)
                  }}
                  disabled={isBrowsingDirectories}
                  style={secondaryButtonStyle}
                >
                  Reload
                </button>
                <label
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '6px',
                    marginLeft: 'auto',
                    fontSize: '11px',
                    color: '#aaaaaa',
                  }}
                >
                  <input
                    aria-label="Show hidden directories"
                    type="checkbox"
                    checked={showHiddenDirectories}
                    onChange={(e) => {
                      void handleToggleHiddenDirectories(e.target.checked)
                    }}
                  />
                  Show hidden directories
                </label>
              </div>
              <div style={{ display: 'flex', gap: '8px' }}>
                <input
                  aria-label="Directory browser path"
                  type="text"
                  value={browserInputPath}
                  onChange={(e) => setBrowserInputPath(e.target.value)}
                  style={{ ...inputStyle, flex: 1 }}
                />
                <button
                  type="button"
                  onClick={() => {
                    void browseDirectory(browserInputPath)
                  }}
                  disabled={isBrowsingDirectories}
                  style={secondaryButtonStyle}
                >
                  Go
                </button>
              </div>
            </div>

            <div style={{ padding: '10px 16px', fontSize: '11px', color: '#9ca3af', borderBottom: '1px solid #333' }}>
              Current folder: {browserPath}
            </div>

            <div style={{ flex: 1, overflowY: 'auto', padding: '8px 12px 12px' }}>
              {isBrowsingDirectories && (
                <div style={{ fontSize: '12px', color: '#888', padding: '8px 4px' }}>Loading directories…</div>
              )}
              {browseError && (
                <div style={{ fontSize: '12px', color: '#f44747', padding: '8px 4px' }}>{browseError}</div>
              )}
              {!isBrowsingDirectories && !browseError && directoryResponses[browserPath]?.entries.length === 0 && (
                <div style={{ fontSize: '12px', color: '#888', padding: '8px 4px' }}>No directories found.</div>
              )}
              {renderDirectoryEntries(browserPath)}
            </div>

            <div style={{ padding: '12px 16px', borderTop: '1px solid #333', backgroundColor: '#181818' }}>
              <div style={{ fontSize: '11px', color: '#9ca3af', marginBottom: '10px' }}>
                Selected: {browserSelection}
              </div>
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
                <button
                  type="button"
                  onClick={() => setShowDirectoryBrowser(false)}
                  style={secondaryButtonStyle}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  onClick={applyDirectorySelection}
                  disabled={!browserSelection}
                  style={{
                    padding: '5px 14px',
                    backgroundColor: '#0e639c',
                    color: '#fff',
                    border: 'none',
                    borderRadius: '3px',
                    fontFamily: TERMINAL_FONT_FAMILY,
                    fontSize: '13px',
                    cursor: 'pointer',
                    opacity: browserSelection ? 1 : 0.5,
                  }}
                >
                  Choose Directory
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function parentDirectory(path: string): string {
  if (!path || path === '/') return '/'
  const normalized = path.endsWith('/') && path !== '/' ? path.slice(0, -1) : path
  const lastSlash = normalized.lastIndexOf('/')
  if (lastSlash <= 0) return '/'
  return normalized.slice(0, lastSlash)
}

function actionButtonStyle(disabled: boolean): React.CSSProperties {
  return {
    padding: '5px 10px',
    backgroundColor: 'transparent',
    color: '#888',
    border: '1px solid #555',
    borderRadius: '3px',
    fontFamily: TERMINAL_FONT_FAMILY,
    fontSize: '13px',
    cursor: 'pointer',
    whiteSpace: 'nowrap',
    opacity: disabled ? 0.5 : 1,
  }
}

const secondaryButtonStyle: React.CSSProperties = {
  padding: '5px 14px',
  backgroundColor: 'transparent',
  color: '#c5c5c5',
  border: '1px solid #555',
  borderRadius: '3px',
  fontFamily: TERMINAL_FONT_FAMILY,
  fontSize: '13px',
  cursor: 'pointer',
}
