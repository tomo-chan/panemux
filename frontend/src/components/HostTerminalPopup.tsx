import React, { useRef } from 'react'
import type { HostTerminalSession } from '../hooks/useHostTerminal'
import { useTerminalPopupKeyboard } from '../hooks/useTerminalPopupKeyboard'
import { TaskTerminal } from './TaskTerminal'
import type { TaskTerminalStatus } from './TaskTerminal'
import { TASK_INPUT_CHIP_LABELS, taskInputChip } from './TaskInputPopup'

// A host chip's Type in pane popup (issue #314): a real terminal on a host,
// over a host terminal the server opens for it (POST /api/hosts/terminal) and
// keeps out of the layout. It looks and behaves like a task's popup
// (TaskInputPopup); TaskDashboard owns the terminal's lifecycle.

export interface HostTerminalPopupProps {
  session: HostTerminalSession
  onTerminalStatus: (status: TaskTerminalStatus) => void
  maximized: boolean
  /** A narrow screen: the popup is a full sheet from the start and has no maximize. */
  sheet: boolean
  /** Distance from the dashboard's top to the bottom of its top bar, which a maximized popup leaves visible. */
  topOffset: number
  onToggleMaximize: () => void
  onClose: () => void
  /** Opens a new terminal in place of this one. */
  onRetry: () => void
}

export const HostTerminalPopup: React.FC<HostTerminalPopupProps> = ({
  session,
  onTerminalStatus,
  maximized,
  sheet,
  topOffset,
  onToggleMaximize,
  onClose,
  onRetry,
}) => {
  const dialogRef = useRef<HTMLDivElement>(null)
  const bodyRef = useRef<HTMLDivElement>(null)
  const { host, type, attach, attempt, terminal } = session
  const chip = taskInputChip(attach, terminal)
  const isMaximized = maximized && !sheet
  const tmuxSession = attach.phase === 'ready' ? attach.tmuxSession : session.tmuxSession ?? ''

  useTerminalPopupKeyboard({
    dialogRef,
    bodyRef,
    headSelector: '.td-input-head',
    connected: chip === 'connected',
    sheet,
    onClose,
    onToggleMaximize,
  })

  const notSent = (() => {
    if (attach.phase === 'failed') {
      return { text: `Could not connect: ${attach.error.replace(/\.$/, '')}.`, action: 'Retry' }
    }
    if (attach.phase !== 'ready') return null
    switch (terminal) {
      case 'failed':
        return { text: 'Could not connect to the terminal.', action: 'Retry' }
      case 'disconnected':
        return { text: 'Disconnected.', action: 'Reconnect' }
      case 'ended':
        return { text: 'The connection ended.', action: 'Reconnect' }
      default:
        return null
    }
  })()

  return (
    <div className="td-input-layer" data-maximized={isMaximized} data-sheet={sheet}>
      <div className="td-input-backdrop" aria-hidden="true" />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label={`Terminal on ${host}`}
        tabIndex={-1}
        className="td-input"
        data-maximized={isMaximized}
        data-sheet={sheet}
        style={{ '--td-input-top': `${topOffset}px` } as React.CSSProperties}
      >
        <header
          className="td-input-head"
          onDoubleClick={(event) => {
            if (sheet || (event.target as HTMLElement).closest('button, a')) return
            onToggleMaximize()
          }}
        >
          <div className="td-input-titlebar">
            <span className="td-pill">{type}</span>
            <h2 className="td-input-title">{host}</h2>
            <span className="td-input-chip" data-status={chip} data-testid="task-input-status" aria-live="polite">
              <span className="td-input-chip-dot" aria-hidden="true" />
              {TASK_INPUT_CHIP_LABELS[chip]}
            </span>
            <span className="td-spacer" />
            {!sheet && (
              <button
                type="button"
                className="td-btn td-btn-sm td-input-max"
                aria-pressed={isMaximized}
                aria-label={isMaximized ? 'Restore size' : 'Maximize'}
                aria-keyshortcuts="Meta+Shift+Enter Control+Shift+Enter"
                title={isMaximized ? 'Restore size (Cmd/Ctrl+Shift+Enter)' : 'Maximize (Cmd/Ctrl+Shift+Enter)'}
                onClick={onToggleMaximize}
              >
                <span aria-hidden="true">{isMaximized ? '⤡' : '⤢'}</span>
              </button>
            )}
            <button
              type="button"
              className="td-btn td-btn-sm"
              aria-keyshortcuts="Meta+Shift+Escape Control+Shift+Escape"
              onClick={onClose}
            >
              Close
            </button>
          </div>
          {type === 'ssh_tmux' && <div className="td-input-meta td-mono">tmux: {tmuxSession}</div>}
        </header>
        {notSent && (
          <div role="alert" className="td-input-note" data-tone="error">
            {notSent.text} Input is not being sent.
            <button type="button" className="td-btn td-btn-sm" onClick={onRetry}>
              {notSent.action}
            </button>
          </div>
        )}
        <div ref={bodyRef} className="td-input-body" data-input={chip === 'connected' ? 'on' : 'off'}>
          {attach.phase === 'ready' ? (
            <TaskTerminal key={`${attach.sessionId}:${attempt}`} sessionId={attach.sessionId} onStatus={onTerminalStatus} />
          ) : (
            <div className="td-input-blank">
              {attach.phase === 'connecting' ? (
                <>
                  <strong>Connecting to {host}…</strong>
                  <span>Keys are not sent until it connects.</span>
                </>
              ) : (
                <strong>Not connected.</strong>
              )}
            </div>
          )}
        </div>
        <footer className="td-input-foot">
          Esc goes to the terminal · Cmd/Ctrl+Shift+Esc closes
          {!sheet && ' · Cmd/Ctrl+Shift+Enter maximizes'} ·{' '}
          {type === 'ssh_tmux' ? 'Closing leaves the tmux session running' : `Closing logs out of ${host}`}
        </footer>
      </div>
    </div>
  )
}
