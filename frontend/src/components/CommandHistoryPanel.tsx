import React, { useEffect, useState } from 'react'
import { BoardCommandHistoryResponseSchema } from '../schemas'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'
import { summarizeStreamLines } from '../utils/streamJson'
import type { StreamSummaryLine } from '../utils/streamJson'
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
  const [lines, setLines] = useState<StreamSummaryLine[]>([])
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
        if (!cancelled) setLines(summarizeStreamLines(data.entries.map((entry) => entry.raw)))
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
    // Capture phase: a focused xterm terminal stops keydown propagation, so
    // a bubble-phase window listener never sees Escape at all. The palette
    // opens over a terminal that usually still holds focus, which is exactly
    // the state where a bubble-registered handler would silently do nothing.
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeyDown, true)
    return () => window.removeEventListener('keydown', handleKeyDown, true)
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
          {!error && lines.length === 0 && <div style={emptyStyle}>No history yet.</div>}
          {lines.map((line, i) => {
            if (line.kind === 'prompt') {
              return <div key={i} style={promptLineStyle}>{`> ${line.text}`}</div>
            }
            if (line.kind === 'tool') {
              return <div key={i} style={toolLineStyle}>{`→ ${line.text}`}</div>
            }
            return <div key={i} style={rawStyle}>{line.text}</div>
          })}
        </div>
      </div>
    </div>
  )
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

// A prompt opens its turn, so it carries the same marker and colour the
// palette uses for the operator's own line.
const promptLineStyle: React.CSSProperties = {
  color: '#4ec9b0',
  marginTop: '14px',
  marginBottom: '4px',
  whiteSpace: 'pre-wrap',
  wordBreak: 'break-word',
}

const toolLineStyle: React.CSSProperties = {
  color: '#8f98a8',
  fontSize: '11px',
  marginBottom: '2px',
}

const rawStyle: React.CSSProperties = {
  color: '#ccc',
  marginBottom: '4px',
  whiteSpace: 'pre-wrap',
  wordBreak: 'break-word',
}
