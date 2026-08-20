import React from 'react'
import { useBoardStatus } from '../hooks/useBoardStatus'
import { useRestoreFocusOnClose } from '../hooks/useRestoreFocusOnClose'
import type { BoardMessage, BoardStatusEntry } from '../schemas'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'
import { colorForBoardState, formatRelativeTime, isStaleUpdatedAt } from '../utils/boardStatusColors'

interface BoardDashboardPanelProps {
  isOpen: boolean
  token: string
  // Every pane configured with agent_board.enabled, reported or not. Listing
  // only panes that already reported made "configured but never joined"
  // indistinguishable from "not configured at all" — the first question this
  // panel exists to answer.
  boardPaneIds: readonly string[]
  onClose: () => void
}

// BoardDashboardPanel is the read-only status/history view for Agent Board
// — see docs/agent-board.md's Architecture section and docs/ui-design.md's
// Agent Board UI section. Structure and styling deliberately mirror
// CommandHistoryPanel.tsx (same right-docked overlay pattern), not a new
// visual language. Broadcast sending is out of scope by design (see the
// implementation plan's "Scope外" section) — this panel only ever reads
// /api/board/status and /api/board/messages.
export const BoardDashboardPanel: React.FC<BoardDashboardPanelProps> = ({ isOpen, token, boardPaneIds, onClose }) => {
  const { statuses, messages, error } = useBoardStatus({ enabled: isOpen, token })
  useRestoreFocusOnClose(isOpen)

  // Registered on the capture phase for the same reason App.tsx registers
  // this panel's own Cmd/Ctrl+Shift+B shortcut there: a focused xterm
  // terminal stops keydown propagation, so a bubble-phase window listener
  // never sees the key at all. That is not an edge case here — opening the
  // panel with the keyboard shortcut leaves focus exactly where it was, on
  // the terminal, so a bubble-registered Escape handler would do nothing in
  // the most common way the panel gets opened.
  React.useEffect(() => {
    if (!isOpen) return
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeyDown, true)
    return () => window.removeEventListener('keydown', handleKeyDown, true)
  }, [isOpen, onClose])

  if (!isOpen) return null

  // Union, so a pane that dropped out of the config but is still reporting
  // does not silently vanish from the board.
  const paneIds = Array.from(new Set([...boardPaneIds, ...Object.keys(statuses)])).sort()
  const messagesNewestFirst = [...messages].reverse()
  const hosts = new Set(messages.map((m) => m.host))
  const showHost = hosts.size > 1

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Agent board"
      style={overlayStyle}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div style={panelStyle}>
        <div style={headerRowStyle}>
          <span style={titleStyle}>Agent Board</span>
          <button onClick={onClose} style={closeButtonStyle} aria-label="Close agent board panel">×</button>
        </div>

        <div style={bodyStyle}>
          {error && <div style={errorStyle}>{error}</div>}

          <div style={sectionTitleStyle}>Panes</div>
          {paneIds.length === 0 && <div style={emptyStyle}>No pane has agent board enabled yet.</div>}
          {paneIds.map((paneId) => (
            <PaneStatusCard key={paneId} paneId={paneId} status={statuses[paneId]} />
          ))}

          <div style={{ ...sectionTitleStyle, marginTop: '16px' }}>Messages</div>
          {messagesNewestFirst.length === 0 && <div style={emptyStyle}>No messages yet.</div>}
          {messagesNewestFirst.map((message, i) => (
            <MessageRow key={i} message={message} showHost={showHost} />
          ))}
        </div>
      </div>
    </div>
  )
}

// PaneStatusCard answers two questions and deliberately no others: is this
// pane actually on the board, and what is it doing now.
//
// It used to also show repo, branch and the PR link. Those are computed
// authoritatively elsewhere — panemux runs git itself for the pane header
// and the workspace bar — while the board's copies are self-reported and go
// stale silently, so the same pane could show two different branches in two
// places. Dropping them also removed the only <a> in this component tree,
// and with it the agent-controlled href that needed scheme validation.
const PaneStatusCard: React.FC<{ paneId: string; status?: BoardStatusEntry }> = ({ paneId, status }) => {
  const stale = status !== undefined && isStaleUpdatedAt(status.updated_at)

  return (
    <div style={{ ...cardStyle, opacity: stale ? 0.6 : 1 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }}>
        <span style={statusDotStyle(status?.state)} />
        <span style={paneIdStyle}>{paneId}</span>
        {!status && <span style={pillStyle('#3a2a2a', '#f08b8b')}>not joined</span>}
        {status?.state && <span style={pillStyle('#2d253f', '#cbb3ff')}>{status.state}</span>}
        {stale && <span style={pillStyle('#5a4311', '#f4bf4f')}>stale</span>}
      </div>
      {!status && (
        <div style={notJoinedDetailStyle}>
          On the board in config, but no status has arrived. It reports once its agent joins.
        </div>
      )}
      {status?.summary && <div style={detailStyle}>{status.summary}</div>}
      {status?.last_tool && <div style={mutedDetailStyle}>tool: {status.last_tool}</div>}
      {status && <div style={timestampStyle}>{formatRelativeTime(status.updated_at)}</div>}
    </div>
  )
}

const MessageRow: React.FC<{ message: BoardMessage; showHost: boolean }> = ({ message, showHost }) => (
  <div style={messageRowStyle}>
    <div style={timestampStyle}>
      {formatRelativeTime(message.at)}
      {showHost && ` · ${message.host}`}
    </div>
    <div style={messageFromToStyle}>{message.from} → {message.to}</div>
    <div style={messageBodyStyle}>{message.body}</div>
  </div>
)

function statusDotStyle(state: string | undefined): React.CSSProperties {
  const color = colorForBoardState(state)
  return {
    width: 8,
    height: 8,
    borderRadius: '50%',
    backgroundColor: color,
    boxShadow: `0 0 0 1px ${color}33`,
    flexShrink: 0,
  }
}

function pillStyle(backgroundColor: string, color: string): React.CSSProperties {
  return {
    display: 'inline-flex',
    alignItems: 'center',
    padding: '1px 5px',
    borderRadius: 999,
    backgroundColor,
    color,
    fontSize: 9,
    fontWeight: 700,
    letterSpacing: '0.02em',
    whiteSpace: 'nowrap',
    flexShrink: 0,
  }
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

const sectionTitleStyle: React.CSSProperties = {
  fontSize: '11px',
  fontWeight: 700,
  color: '#8f98a8',
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
  marginBottom: '8px',
}

const emptyStyle: React.CSSProperties = { color: '#666', marginBottom: '8px' }

const errorStyle: React.CSSProperties = { color: '#f44747', marginBottom: '10px' }

const cardStyle: React.CSSProperties = {
  border: '1px solid #3a3a3a',
  borderRadius: '4px',
  padding: '8px 10px',
  marginBottom: '8px',
  display: 'flex',
  flexDirection: 'column',
  gap: '4px',
}

const paneIdStyle: React.CSSProperties = {
  fontSize: '12px',
  fontWeight: 700,
  color: '#e0e0e0',
  minWidth: 0,
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
  flex: '1 1 auto',
}

const detailStyle: React.CSSProperties = {
  color: '#ccc',
  fontSize: 11,
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
}

const mutedDetailStyle: React.CSSProperties = {
  ...detailStyle,
  color: '#8f98a8',
}

// The one line in a card that is a sentence rather than a value, so it wraps
// instead of being ellipsised: a truncated explanation of why a pane has no
// status is worse than no explanation.
const notJoinedDetailStyle: React.CSSProperties = {
  color: '#8f98a8',
  fontSize: 11,
  whiteSpace: 'normal',
}

const timestampStyle: React.CSSProperties = { color: '#666', fontSize: '11px' }

const messageRowStyle: React.CSSProperties = { marginBottom: '10px' }

const messageFromToStyle: React.CSSProperties = { color: '#4ec9b0', fontSize: '11px' }

const messageBodyStyle: React.CSSProperties = { color: '#ccc', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }
