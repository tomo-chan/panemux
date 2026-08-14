import React, { useEffect, useState } from 'react'
import { BoardCommandHistoryResponseSchema } from '../schemas'
import type { BoardCommandHistoryEntry } from '../schemas'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'
import { useRestoreFocusOnClose } from '../hooks/useRestoreFocusOnClose'

interface CommandHistoryPanelProps {
  isOpen: boolean
  token: string
  onClose: () => void
}

// CommandHistoryPanel is the persistently accessible history view for the
// Agent Board command center — the same captured turn-by-turn record the
// palette shows inline, but reachable outside the quick-palette flow for
// scrolling back further. See docs/agent-board.md's Command center section
// and docs/ui-design.md's Agent Board UI section (existing overlay-panel
// pattern reused here, not a new visual language).
export const CommandHistoryPanel: React.FC<CommandHistoryPanelProps> = ({ isOpen, token, onClose }) => {
  const [entries, setEntries] = useState<BoardCommandHistoryEntry[]>([])
  const [error, setError] = useState<string | null>(null)

  useRestoreFocusOnClose(isOpen)

  useEffect(() => {
    if (!isOpen) return
    let cancelled = false
    setError(null)
    void (async () => {
      try {
        const res = await fetch('/api/board/command/history', {
          headers: { Authorization: `Bearer ${token}` },
        })
        if (!res.ok) {
          if (!cancelled) setError(`Failed to load history (${res.status}).`)
          return
        }
        const data = BoardCommandHistoryResponseSchema.parse(await res.json())
        if (!cancelled) setEntries(data.entries)
      } catch {
        if (!cancelled) setError('Failed to load history.')
      }
    })()
    return () => {
      cancelled = true
    }
  }, [isOpen, token])

  useEffect(() => {
    if (!isOpen) return
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  if (!isOpen) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Command center history"
      style={overlayStyle}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div style={panelStyle}>
        <div style={headerRowStyle}>
          <span style={titleStyle}>Command Center History</span>
          <button onClick={onClose} style={closeButtonStyle} aria-label="Close history panel">×</button>
        </div>

        <div style={bodyStyle}>
          {error && <div style={errorStyle}>{error}</div>}
          {!error && entries.length === 0 && <div style={emptyStyle}>No history yet.</div>}
          {entries.map((entry, i) => (
            <div key={i} style={entryStyle}>
              <div style={timestampStyle}>{formatTimestamp(entry.at)}</div>
              <div style={rawStyle}>{JSON.stringify(entry.raw)}</div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

function formatTimestamp(at: string): string {
  const date = new Date(at)
  return Number.isNaN(date.getTime()) ? at : date.toLocaleString()
}

const overlayStyle: React.CSSProperties = {
  position: 'fixed',
  inset: 0,
  backgroundColor: 'rgba(0, 0, 0, 0.4)',
  display: 'flex',
  justifyContent: 'flex-end',
  zIndex: 1150,
}

const panelStyle: React.CSSProperties = {
  backgroundColor: '#252526',
  borderLeft: '1px solid #444',
  width: '420px',
  maxWidth: 'calc(100vw - 32px)',
  height: '100%',
  display: 'flex',
  flexDirection: 'column',
  boxSizing: 'border-box',
  fontFamily: TERMINAL_FONT_FAMILY,
  color: '#d4d4d4',
}

const headerRowStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  padding: '12px 16px',
  borderBottom: '1px solid #333',
}

const titleStyle: React.CSSProperties = { fontSize: '13px', fontWeight: 600, color: '#e0e0e0' }

const closeButtonStyle: React.CSSProperties = {
  background: 'transparent',
  border: 'none',
  color: '#888',
  fontSize: '18px',
  lineHeight: 1,
  cursor: 'pointer',
}

const bodyStyle: React.CSSProperties = {
  flex: 1,
  overflowY: 'auto',
  padding: '12px 16px',
  fontSize: '12px',
}

const emptyStyle: React.CSSProperties = { color: '#666' }

const errorStyle: React.CSSProperties = { color: '#f44747' }

const entryStyle: React.CSSProperties = { marginBottom: '10px' }

const timestampStyle: React.CSSProperties = { color: '#666', fontSize: '11px', marginBottom: '2px' }

const rawStyle: React.CSSProperties = { color: '#ccc', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }
