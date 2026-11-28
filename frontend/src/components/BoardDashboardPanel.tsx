import React from 'react'
import { useBoardStatus } from '../hooks/useBoardStatus'
import { useModalKeyboard } from '../hooks/useModalKeyboard'
import { useRestoreFocusOnClose } from '../hooks/useRestoreFocusOnClose'
import type { BoardIdentity, BoardParticipation, BoardMessage, BoardStatusEntry } from '../schemas'
import { boardText } from '../utils/boardText'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'
import { colorForBoardState, formatRelativeTime, isStaleUpdatedAt } from '../utils/boardStatusColors'

// BoardPaneRef is a pane the config put on the board. The title is the
// operator's own name for it and is what makes a column of them readable;
// the ID is the agmsg address every from/to value uses, so it stays visible
// even when a title exists.
export interface BoardPaneRef {
  id: string
  title?: string
}

interface BoardDashboardPanelProps {
  isOpen: boolean
  token: string
  // All configured panes supply titles; participation determines which are shown.
  boardPanes: readonly BoardPaneRef[]
  onClose: () => void
}

// BoardDashboardPanel is the read-only status/history view for Agent Board
// — see docs/backend/agent-board/relay.md and docs/ui/agent-board.md's
// Dashboard section. Structure and styling deliberately mirror
// CommandHistoryPanel.tsx (same right-docked overlay pattern), not a new
// visual language. Sending lives in the command center's Message mode;
// this panel reads status/history and copies exact qualified addresses.
export const BoardDashboardPanel: React.FC<BoardDashboardPanelProps> = ({ isOpen, token, boardPanes, onClose }) => {
  const { identities, statuses, participation, messages, error } = useBoardStatus({ enabled: isOpen, token })
  useRestoreFocusOnClose(isOpen)
  const dialogRef = React.useRef<HTMLDivElement>(null)

  // Escape on the capture phase, plus a focus trap, both from the shared hook.
  // The capture phase is for the same reason App.tsx registers this panel's
  // own Cmd/Ctrl+Shift+B shortcut there: a focused xterm terminal stops
  // keydown propagation. That is not an edge case here — opening the panel
  // with the keyboard shortcut leaves focus exactly where it was, on the
  // terminal, which is the most common way the panel gets opened.
  useModalKeyboard({ isOpen, dialogRef, onEscape: onClose })

  if (!isOpen) return null

  // A detected agent or report makes a pane visible; configuration supplies titles.
  const titles = new Map(boardPanes.map((pane) => [pane.id, pane.title]))
  const representedPanes = new Set(Object.values(identities).flatMap(identity => identity.pane_id ? [identity.pane_id] : []))
  const paneIds = Array.from(new Set([...Object.keys(participation).filter(id => participation[id].agent_type !== '' || participation[id].last_report !== undefined), ...Object.keys(statuses)])).filter(id => !representedPanes.has(id)).sort()
  const messagesNewestFirst = [...messages].reverse()

  return (
    <div
      ref={dialogRef}
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

          <div style={sectionTitleStyle}>Names</div>
          <div style={{ ...notJoinedDetailStyle, marginBottom: 8 }}>Observed names do not guarantee registration, liveness or delivery.</div>
          {Object.entries(identities).sort(([a], [b]) => a.localeCompare(b)).map(([address, identity]) => (
            <IdentityCard key={address} address={address} identity={identity} title={identity.pane_id ? titles.get(identity.pane_id) : undefined} />
          ))}
          {paneIds.length === 0 && Object.keys(identities).length === 0 && <div style={emptyStyle}>No agent detected or status reported yet.</div>}
          {paneIds.length > 0 && <div style={{ ...sectionTitleStyle, marginTop: 12 }}>Detected panes</div>}
          {paneIds.map((paneId) => (
            <PaneStatusCard key={paneId} paneId={paneId} title={titles.get(paneId)} participation={participation[paneId]} status={statuses[paneId]} />
          ))}

          <div style={{ ...sectionTitleStyle, marginTop: '16px' }}>Messages</div>
          {messagesNewestFirst.length === 0 && <div style={emptyStyle}>No messages yet.</div>}
          {messagesNewestFirst.map((message, i) => (
            <MessageRow key={i} message={message} />
          ))}
        </div>
      </div>
    </div>
  )
}

const IdentityCard: React.FC<{ address: string; identity: BoardIdentity; title?: string }> = ({ address, identity, title }) => {
  const [copyResult, setCopyResult] = React.useState('')
  const report = identity.self_report
  const task = identity.source === 'task'
  const stale = !task && report !== null && isStaleUpdatedAt(report.updated_at)
  const copy = async () => {
    try { await navigator.clipboard.writeText(address); setCopyResult('Address copied') }
    catch { setCopyResult('Could not copy address') }
  }
  return <article style={{ ...cardStyle, opacity: stale ? 0.6 : 1 }}>
    <strong style={{ overflowWrap: 'anywhere' }}>{boardText(address)}</strong>
    <div style={{ color: '#8f98a8', fontSize: 11, overflowWrap: 'anywhere' }}>{boardText(identity.host)} · {identity.pane_id ? `Pane: ${boardText(identity.pane_id)}` : 'No pane'}</div>
    {title && <div style={paneTitleStyle}>{title}</div>}
    <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}>
      <span style={statusDotStyle(identity.state ?? undefined, identity.source)} />
      <span>{task ? 'Task observed' : identity.source === 'self_report' ? 'Self-report' : 'Unreported'}</span>
      {identity.state && <span style={{ overflowWrap: 'anywhere', minWidth: 0 }}>{boardText(identity.state)}</span>}
      {stale && <span>stale</span>}
    </div>
    {task && <>
      {identity.waiting_for && <div style={notJoinedDetailStyle}>Waiting for: {boardText(identity.waiting_for)}</div>}
      {identity.status_since && <div style={timestampStyle}>State since {formatRelativeTime(identity.status_since)}</div>}
      {identity.task_observed_at && <div style={timestampStyle}>Observed {formatRelativeTime(identity.task_observed_at)}</div>}
    </>}
    {report && <div>
      {task && <div style={mutedDetailStyle}>Self-report{isStaleUpdatedAt(report.updated_at) ? ' · stale' : ''}{report.state ? ` · ${boardText(report.state)}` : ''}</div>}
      {report.summary && <ReportSummary summary={report.summary} />}
      {report.last_tool && <div style={mutedDetailStyle}>tool: {boardText(report.last_tool)}</div>}
      <div style={timestampStyle}>{formatRelativeTime(report.updated_at)}</div>
    </div>}
    {identity.last_message_at && <div style={timestampStyle}>Last message {formatRelativeTime(identity.last_message_at)}</div>}
    <div><button type="button" onClick={() => void copy()} style={textButtonStyle}>Copy address</button></div>
    {copyResult && <div role="status">{copyResult}</div>}
  </article>
}

const ReportSummary: React.FC<{ summary: string }> = ({ summary }) => {
  const [expanded, setExpanded] = React.useState(false)
  return <>
    <div style={{ ...summaryStyle, whiteSpace: 'pre-wrap', ...(expanded ? { display: 'block', WebkitLineClamp: 'unset', overflow: 'visible' } : {}) }}>{boardText(summary)}</div>
    <button type="button" style={textButtonStyle} aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>{expanded ? 'Show less' : 'Show more'}</button>
  </>
}

const textButtonStyle: React.CSSProperties = { background: 'transparent', border: '1px solid #555', color: '#b7d9f7', borderRadius: 3, font: 'inherit', padding: '3px 6px', cursor: 'pointer' }

// PaneStatusCard answers two questions and deliberately no others: is this
// pane actually on the board, and what is it doing now.
//
// It used to also show repo, branch and the PR link. Those are computed
// authoritatively elsewhere — panemux runs git itself for the pane header
// and the workspace bar — while the board's copies are self-reported and go
// stale silently, so the same pane could show two different branches in two
// places. Dropping them also removed the only <a> in this component tree,
// and with it the agent-controlled href that needed scheme validation.
const PaneStatusCard: React.FC<{ paneId: string; title?: string; status?: BoardStatusEntry; participation?: BoardParticipation }> = ({
  paneId,
  title,
  status,
  participation,
}) => {
  const stale = status !== undefined && isStaleUpdatedAt(status.updated_at)

  return (
    <div style={{ ...cardStyle, opacity: stale ? 0.6 : 1 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }}>
        <span style={statusDotStyle(status?.state)} />
        <span style={paneIdStyle}>{paneId}</span>
        {title && <span style={paneTitleStyle}>{title}</span>}
        <span style={pillGroupStyle}>
          {!status && <span style={pillStyle('#3a2a2a', '#f08b8b')}>{participation?.instructed ? 'instruction sent' : 'agent detected'}</span>}
          {status?.state && <span style={{ ...pillStyle('#2d253f', '#cbb3ff'), maxWidth: 140, overflow: 'hidden', textOverflow: 'ellipsis' }}>{boardText(status.state)}</span>}
          {stale && <span style={pillStyle('#5a4311', '#f4bf4f')}>stale</span>}
        </span>
      </div>
      {!status && (
        <div style={notJoinedDetailStyle}>
          {boardText(participation?.agent_type ?? '')} on {boardText(participation?.host ?? 'unknown host')}. No status report yet.
        </div>
      )}
      {status && <div style={mutedDetailStyle}>Self-report · {boardText(participation?.host ?? 'host unavailable')}</div>}
      {status?.summary && <ReportSummary summary={status.summary} />}
      {status?.last_tool && <div style={mutedDetailStyle}>tool: {boardText(status.last_tool)}</div>}
      {status && <div style={timestampStyle}>{formatRelativeTime(status.updated_at)}</div>}
    </div>
  )
}

const MessageRow: React.FC<{ message: BoardMessage }> = ({ message }) => (
  <div style={messageRowStyle}>
    <div style={timestampStyle}>
      {formatRelativeTime(message.at)}
      {` · ${boardText(message.host)}`}
    </div>
    <div style={messageFromToStyle}>{boardText(message.from === '_system' || message.from.includes('@') ? message.from : `${message.from}@${message.host}`)} → {boardText(message.to)}</div>
    <div style={messageBodyStyle}>{boardText(message.body)}</div>
  </div>
)

function statusDotStyle(state: string | undefined, source?: BoardIdentity['source']): React.CSSProperties {
  const color = colorForBoardState(state, source)
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
  flexShrink: 0,
}

// The summary is the one field a person actually reads down the column, so
// it wraps instead of being clipped to one 420px line. It is still bounded:
// four lines, because an agent that reports a paragraph must not push every
// other pane below the fold. -webkit-line-clamp is the only widely supported
// way to bound by lines rather than pixels, and it is honored by every
// browser panemux supports (see docs/backend/workspace/sessions.md's Operational assumptions and distribution).
const summaryStyle: React.CSSProperties = {
  color: '#ccc',
  fontSize: 11,
  display: '-webkit-box',
  WebkitBoxOrient: 'vertical',
  WebkitLineClamp: 4,
  overflow: 'hidden',
  wordBreak: 'break-word',
}

// marginLeft: auto rather than letting the title flex-grow, so the pills
// stay right-aligned whether or not a pane has a title.
const pillGroupStyle: React.CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  gap: 6,
  marginLeft: 'auto',
  flexShrink: 0,
}

const paneTitleStyle: React.CSSProperties = {
  color: '#8f98a8',
  fontSize: 11,
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
  minWidth: 0,
  flexShrink: 1,
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
  overflowWrap: 'anywhere',
}

const timestampStyle: React.CSSProperties = { color: '#666', fontSize: '11px', overflowWrap: 'anywhere' }

const messageRowStyle: React.CSSProperties = { marginBottom: '10px' }

const messageFromToStyle: React.CSSProperties = { color: '#4ec9b0', fontSize: '11px', overflowWrap: 'anywhere' }

const messageBodyStyle: React.CSSProperties = { color: '#ccc', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }
