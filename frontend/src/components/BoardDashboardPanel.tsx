import React from 'react'
import { useBoardStatus } from '../hooks/useBoardStatus'
import { useRestoreFocusOnClose } from '../hooks/useRestoreFocusOnClose'
import type { BoardMessage, BoardStatusEntry } from '../schemas'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'
import { colorForBoardState, formatRelativeTime, isStaleUpdatedAt } from '../utils/boardStatusColors'

interface BoardDashboardPanelProps {
  isOpen: boolean
  token: string
  onClose: () => void
}

// BoardDashboardPanel is the read-only status/history view for Agent Board
// — see docs/agent-board.md's Architecture section and docs/ui-design.md's
// Agent Board UI section. Structure and styling deliberately mirror
// CommandHistoryPanel.tsx (same right-docked overlay pattern), not a new
// visual language. Broadcast sending is out of scope by design (see the
// implementation plan's "Scope外" section) — this panel only ever reads
// /api/board/status and /api/board/messages.
export const BoardDashboardPanel: React.FC<BoardDashboardPanelProps> = ({ isOpen, token, onClose }) => {
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

  const paneIds = Object.keys(statuses).sort()
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
          {paneIds.length === 0 && <div style={emptyStyle}>No pane has reported status yet.</div>}
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

const PaneStatusCard: React.FC<{ paneId: string; status: BoardStatusEntry }> = ({ paneId, status }) => {
  const stale = isStaleUpdatedAt(status.updated_at)
  const prHref = status.pr_url ? safeExternalURL(status.pr_url) : null

  return (
    <div style={{ ...cardStyle, opacity: stale ? 0.6 : 1 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }}>
        <span style={statusDotStyle(status.state)} />
        <span style={paneIdStyle}>{paneId}</span>
        {status.state && <span style={pillStyle('#2d253f', '#cbb3ff')}>{status.state}</span>}
        {stale && <span style={pillStyle('#5a4311', '#f4bf4f')}>stale</span>}
      </div>
      <div style={metaRowStyle}>
        {status.repo && <span style={repoStyle}>{status.repo}</span>}
        {status.branch && <span style={ellipsisStyle}>{status.branch}</span>}
        {status.pr_url &&
          (prHref ? (
            <a href={prHref} target="_blank" rel="noopener noreferrer" style={linkStyle}>
              {prLabel(status.pr_url)}
            </a>
          ) : (
            <span style={ellipsisStyle}>{status.pr_url}</span>
          ))}
      </div>
      {status.summary && <div style={detailStyle}>{status.summary}</div>}
      {status.last_tool && <div style={mutedDetailStyle}>tool: {status.last_tool}</div>}
      {status.cwd && <div style={mutedDetailStyle}>{status.cwd}</div>}
      <div style={timestampStyle}>{formatRelativeTime(status.updated_at)}</div>
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

// safeExternalURL returns url only when it is an ordinary web address, and
// null otherwise, so the caller can fall back to rendering it as text.
//
// pr_url reaches this component as free text an agent wrote about itself
// (internal/board's ParseStatus copies it through unvalidated, and the
// relay only checks who sent a row, never what is in it), possibly from a
// remote host. React 18 — the version this app pins — merely logs a warning
// for a javascript: href and renders it anyway, so without this check a
// status report would be enough to run script in the dashboard's origin,
// which holds the board bearer token. target="_blank" and rel="noopener
// noreferrer" do not help: they constrain the opened document, not whether
// a script-scheme URL executes.
function safeExternalURL(url: string): string | null {
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'https:' || parsed.protocol === 'http:' ? url : null
  } catch {
    // Not an absolute URL at all — nothing safe to link to.
    return null
  }
}

// prLabel extracts a "PR #N" label from a github.com pull request URL,
// matching WorkspaceTabs.tsx's own "PR #{n}" text convention. Falls back to
// the raw URL for any URL shape that doesn't match (a non-GitHub agmsg
// deployment could plausibly report something else here).
function prLabel(prUrl: string): string {
  const match = prUrl.match(/\/pull\/(\d+)/)
  return match ? `PR #${match[1]}` : prUrl
}

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

const metaRowStyle: React.CSSProperties = {
  display: 'flex',
  gap: 6,
  flexWrap: 'wrap',
  color: '#8f98a8',
  fontSize: 10,
}

const repoStyle: React.CSSProperties = {
  color: '#9fcbff',
  minWidth: 0,
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
}

const ellipsisStyle: React.CSSProperties = {
  minWidth: 0,
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
}

const linkStyle: React.CSSProperties = {
  color: '#7ea6e0',
  textDecoration: 'none',
  whiteSpace: 'nowrap',
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

const timestampStyle: React.CSSProperties = { color: '#666', fontSize: '11px' }

const messageRowStyle: React.CSSProperties = { marginBottom: '10px' }

const messageFromToStyle: React.CSSProperties = { color: '#4ec9b0', fontSize: '11px' }

const messageBodyStyle: React.CSSProperties = { color: '#ccc', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }
