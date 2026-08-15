import React, { useEffect, useRef, useState } from 'react'
import { useBoardCommand } from '../hooks/useBoardCommand'
import { BoardCommandHistoryResponseSchema } from '../schemas'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'
import { summarizeStreamLines } from '../utils/streamJson'
import type { StreamSummaryLine } from '../utils/streamJson'
import { useRestoreFocusOnClose } from '../hooks/useRestoreFocusOnClose'

interface CommandPaletteProps {
  isOpen: boolean
  token: string
  onClose: () => void
}

const RECENT_HISTORY_LIMIT = 20

// CommandPalette is the Spotlight-style modal for the Agent Board command
// center, per docs/agent-board.md's Command center section and
// docs/ui-design.md's Agent Board UI section (Modal Dialogs pattern reused
// for the palette itself). It owns its own WS connection, open only while
// the palette itself is open, and seeds itself with recent history on open
// so a reopened palette isn't blank.
export const CommandPalette: React.FC<CommandPaletteProps> = ({ isOpen, token, onClose }) => {
  const { connected, turns, pending, sendPrompt } = useBoardCommand({ enabled: isOpen, token })
  const [prompt, setPrompt] = useState('')
  const [recentHistory, setRecentHistory] = useState<StreamSummaryLine[]>([])
  const inputRef = useRef<HTMLInputElement>(null)
  const transcriptRef = useRef<HTMLDivElement>(null)

  useRestoreFocusOnClose(isOpen)

  useEffect(() => {
    if (!isOpen) {
      setPrompt('')
      setRecentHistory([])
      return
    }
    inputRef.current?.focus()

    let cancelled = false
    void (async () => {
      try {
        const res = await fetch('/api/board/command/history', {
          headers: { Authorization: `Bearer ${token}` },
        })
        if (!res.ok) return
        const data = BoardCommandHistoryResponseSchema.parse(await res.json())
        if (!cancelled) {
          // Summarize before slicing: the last N raw lines are almost always
          // all bookkeeping frames, which would summarize to nothing.
          setRecentHistory(summarizeStreamLines(data.entries.map((entry) => entry.raw)).slice(-RECENT_HISTORY_LIMIT))
        }
      } catch {
        // Best-effort inline history — the palette still works without it.
      }
    })()
    return () => {
      cancelled = true
    }
  }, [isOpen, token])

  // Pin the transcript to its newest line. Without this the one line worth
  // reading — the assistant's answer — lands below the fold on every query
  // long enough to overflow, and the palette looks like it produced nothing.
  useEffect(() => {
    const transcript = transcriptRef.current
    if (!transcript) return
    transcript.scrollTop = transcript.scrollHeight
  }, [turns, recentHistory])

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

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault()
    const trimmed = prompt.trim()
    if (!trimmed || pending || !connected) return
    sendPrompt(trimmed)
    setPrompt('')
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Command center"
      style={overlayStyle}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div style={paletteStyle}>
        <div style={headerStyle}>Command Center{connected ? '' : ' (connecting…)'}</div>

        <div style={historyStyle} ref={transcriptRef} data-testid="command-palette-transcript">
          {recentHistory.length === 0 && turns.length === 0 && (
            <div style={emptyStyle}>No recent activity.</div>
          )}
          {recentHistory.map((line, i) => (
            <SummaryLine key={`recent-${i}`} line={line} />
          ))}
          {turns.map((turn) => (
            <div key={turn.id} style={turnStyle}>
              <div style={promptLineStyle}>&gt; {turn.prompt}</div>
              {summarizeStreamLines(turn.lines).map((line, i) => (
                <SummaryLine key={i} line={line} />
              ))}
              {turn.error && <div style={errorLineStyle}>{turn.error}</div>}
              {turn.busy && <div style={errorLineStyle}>Command center is busy — try again shortly.</div>}
              {!turn.done && <div style={lineStyle}>…</div>}
            </div>
          ))}
        </div>

        <form onSubmit={handleSubmit} style={{ display: 'flex', gap: '8px' }}>
          <input
            ref={inputRef}
            type="text"
            value={prompt}
            onChange={(event) => setPrompt(event.target.value)}
            placeholder="Ask the command center…"
            style={inputStyle}
            aria-label="Command center prompt"
          />
          <button type="submit" style={submitStyle} disabled={pending || !connected}>
            {pending ? 'Sending…' : 'Send'}
          </button>
        </form>
      </div>
    </div>
  )
}

// SummaryLine renders one readable line of a command center turn. Tool calls
// are marked rather than spelled out — knowing that board_broadcast ran is the
// useful part; its arguments belong in the history panel, not the palette.
const SummaryLine: React.FC<{ line: StreamSummaryLine }> = ({ line }) =>
  line.kind === 'tool' ? (
    <div style={toolLineStyle}>{`→ ${line.text}`}</div>
  ) : (
    <div style={lineStyle}>{line.text}</div>
  )

const overlayStyle: React.CSSProperties = {
  position: 'fixed',
  inset: 0,
  backgroundColor: 'rgba(0, 0, 0, 0.6)',
  display: 'flex',
  alignItems: 'flex-start',
  justifyContent: 'center',
  paddingTop: '10vh',
  zIndex: 1200,
}

const paletteStyle: React.CSSProperties = {
  backgroundColor: '#252526',
  border: '1px solid #444',
  borderRadius: '6px',
  padding: '16px 20px',
  width: '560px',
  maxWidth: 'calc(100vw - 32px)',
  maxHeight: 'calc(80vh)',
  display: 'flex',
  flexDirection: 'column',
  boxSizing: 'border-box',
  fontFamily: TERMINAL_FONT_FAMILY,
  color: '#d4d4d4',
}

const headerStyle: React.CSSProperties = {
  fontSize: '13px',
  fontWeight: 600,
  color: '#e0e0e0',
  marginBottom: '10px',
}

const historyStyle: React.CSSProperties = {
  flex: 1,
  overflowY: 'auto',
  marginBottom: '12px',
  fontSize: '12px',
  lineHeight: 1.5,
  minHeight: '120px',
  maxHeight: '50vh',
}

const emptyStyle: React.CSSProperties = { color: '#666' }

const turnStyle: React.CSSProperties = { marginBottom: '10px' }

const promptLineStyle: React.CSSProperties = { color: '#4ec9b0' }

const toolLineStyle: React.CSSProperties = {
  color: '#8f98a8',
  fontSize: '12px',
  whiteSpace: 'pre-wrap',
  wordBreak: 'break-word',
}

const lineStyle: React.CSSProperties = { color: '#ccc', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }

const errorLineStyle: React.CSSProperties = { color: '#f44747' }

const inputStyle: React.CSSProperties = {
  flex: 1,
  padding: '6px 10px',
  backgroundColor: '#3c3c3c',
  color: '#d4d4d4',
  border: '1px solid #555',
  borderRadius: '3px',
  fontFamily: TERMINAL_FONT_FAMILY,
  fontSize: '13px',
  boxSizing: 'border-box',
}

const submitStyle: React.CSSProperties = {
  padding: '6px 14px',
  backgroundColor: '#0e639c',
  color: '#fff',
  border: 'none',
  borderRadius: '3px',
  fontFamily: TERMINAL_FONT_FAMILY,
  fontSize: '13px',
  cursor: 'pointer',
}
