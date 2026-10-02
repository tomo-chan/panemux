import React, { useEffect, useRef } from 'react'
import type { Task } from '../schemas'
import {
  TASK_STATE_LABELS,
  hostLabel,
  isTerminalCloseShortcut,
  isTerminalMaximizeShortcut,
  taskOpenAction,
  taskTitle,
} from '../utils/taskBoard'
import type { TaskPaneRef } from '../utils/taskBoard'
import { TaskTerminal } from './TaskTerminal'
import type { TaskTerminalStatus } from './TaskTerminal'

// The task dashboard's Type in pane popup (issue #284): a real terminal on a
// task's tmux session over the board's temporary attach (issue #283), so a
// waiting task can be answered and an idle one given its next instruction
// without leaving the board. TaskDashboard owns the attach's lifecycle; this
// draws it and owns the popup's keyboard rules.

/** Where the board's attach request stands. */
export type TaskAttachPhase =
  | { phase: 'connecting' }
  | { phase: 'ready'; sessionId: string; tmuxSession: string }
  | { phase: 'failed'; error: string }

export type TaskInputChip = 'connecting' | 'connected' | 'disconnected' | 'failed'

const CHIP_LABELS: Record<TaskInputChip, string> = {
  connecting: 'Connecting',
  connected: 'Connected',
  disconnected: 'Disconnected',
  failed: 'Failed',
}

/** The connection state the header shows, from the attach request and then its terminal. */
export function taskInputChip(attach: TaskAttachPhase, terminal: TaskTerminalStatus): TaskInputChip {
  if (attach.phase === 'connecting') return 'connecting'
  if (attach.phase === 'failed') return 'failed'
  switch (terminal) {
    case 'connected':
      return 'connected'
    case 'failed':
      return 'failed'
    case 'disconnected':
    case 'ended':
      return 'disconnected'
    default:
      return 'connecting'
  }
}

// What a browser moves focus between, minus anything inert: a terminal that
// takes no input (connecting, disconnected) is skipped as the browser would.
const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

export interface TaskInputPopupProps {
  /** The task as last listed, or as it was when the popup opened if it is listed no more. */
  task: Task
  listed: boolean
  /** The column the task has moved to since the popup opened, by title, or null. */
  movedTo: string | null
  /** The workspace pane already showing the task, if any. */
  pane: TaskPaneRef | null
  attach: TaskAttachPhase
  /** Bumped by each retry, so a reconnect to the same attach opens a new WebSocket. */
  attempt: number
  terminal: TaskTerminalStatus
  onTerminalStatus: (status: TaskTerminalStatus) => void
  maximized: boolean
  /** A narrow screen: the popup is a full sheet from the start and has no maximize. */
  sheet: boolean
  /** Distance from the dashboard's top to the bottom of its top bar, which a maximized popup leaves visible. */
  topOffset: number
  onToggleMaximize: () => void
  onClose: () => void
  onRetry: () => void
  onOpenTask: (task: Task) => void
  /** The state pill's colors, as the dashboard's cards use them. */
  stateStyle: React.CSSProperties
}

export const TaskInputPopup: React.FC<TaskInputPopupProps> = ({
  task,
  listed,
  movedTo,
  pane,
  attach,
  attempt,
  terminal,
  onTerminalStatus,
  maximized,
  sheet,
  topOffset,
  onToggleMaximize,
  onClose,
  onRetry,
  onOpenTask,
  stateStyle,
}) => {
  const dialogRef = useRef<HTMLDivElement>(null)
  const bodyRef = useRef<HTMLDivElement>(null)
  const chip = taskInputChip(attach, terminal)
  const title = taskTitle(task)
  const tmuxSession = attach.phase === 'ready' ? attach.tmuxSession : task.location.tmux_session ?? ''
  const openAction = taskOpenAction(task, pane)
  const isMaximized = maximized && !sheet

  const handlers = useRef({ onClose, onToggleMaximize, sheet })
  handlers.current = { onClose, onToggleMaximize, sheet }

  // Focus starts on the popup itself; the terminal takes it once connected.
  useEffect(() => {
    dialogRef.current?.focus({ preventScroll: true })
  }, [])

  // A terminal that stops taking input becomes inert, which drops its focus
  // on the floor; catch it here so keys still land inside the popup.
  useEffect(() => {
    const dialog = dialogRef.current
    if (!dialog || chip === 'connected') return
    if (!dialog.contains(document.activeElement)) dialog.focus({ preventScroll: true })
  }, [chip])

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const dialog = dialogRef.current
      if (!dialog) return
      const target = event.target instanceof Node ? event.target : null
      // Captured on the window, so a focused terminal never sees these keys.
      if (isTerminalCloseShortcut(event)) {
        event.preventDefault()
        event.stopPropagation()
        handlers.current.onClose()
        return
      }
      if (isTerminalMaximizeShortcut(event)) {
        event.preventDefault()
        event.stopPropagation()
        if (!handlers.current.sheet) handlers.current.onToggleMaximize()
        return
      }
      // Escape, Tab and everything else typed into the terminal are its own.
      if (target && bodyRef.current?.contains(target)) return

      // Escape closes from the header only: anywhere else it may have been
      // meant for the terminal, which is not taking keys right now.
      if (event.key === 'Escape') {
        if (!(target instanceof Element && target.closest('.td-input-head'))) return
        event.preventDefault()
        handlers.current.onClose()
        return
      }
      if (event.key !== 'Tab') return
      const focusable = Array.from(dialog.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
        (element) => !element.closest('[inert]'),
      )
      if (focusable.length === 0) {
        event.preventDefault()
        return
      }
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      const active = document.activeElement
      if (!active || !dialog.contains(active) || active === dialog) {
        event.preventDefault()
        ;(event.shiftKey ? last : first).focus()
      } else if (event.shiftKey && active === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && active === last) {
        event.preventDefault()
        first.focus()
      }
    }
    window.addEventListener('keydown', handleKeyDown, true)
    return () => window.removeEventListener('keydown', handleKeyDown, true)
  }, [])

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
        return { text: 'The tmux client ended.', action: 'Reconnect' }
      default:
        return null
    }
  })()

  const notice = !listed
    ? `The board no longer lists this task. This terminal stays on ${title}.`
    : movedTo
      ? `Moved to ${movedTo} on the board. This terminal stays on ${title}.`
      : null

  return (
    <div className="td-input-layer" data-maximized={isMaximized} data-sheet={sheet}>
      <div className="td-input-backdrop" aria-hidden="true" />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label={`Type in pane: ${title}`}
        tabIndex={-1}
        className="td-input"
        data-maximized={isMaximized}
        data-sheet={sheet}
        style={{ '--td-input-top': `${topOffset}px` } as React.CSSProperties}
      >
        <header
          className="td-input-head"
          data-testid="task-input-header"
          onDoubleClick={(event) => {
            if (sheet || (event.target as HTMLElement).closest('button, a')) return
            onToggleMaximize()
          }}
        >
          <div className="td-input-titlebar">
            <span className="td-pill" style={stateStyle}>
              {TASK_STATE_LABELS[task.state]}
            </span>
            <h2 className="td-input-title">{title}</h2>
            <span className="td-input-chip" data-status={chip} data-testid="task-input-status" aria-live="polite">
              <span className="td-input-chip-dot" aria-hidden="true" />
              {CHIP_LABELS[chip]}
            </span>
            <span className="td-spacer" />
            {openAction.kind !== 'unavailable' && (
              <button
                type="button"
                className="td-btn td-btn-sm"
                aria-label={`${openAction.kind === 'goto' ? 'Go to pane' : 'Open'}: ${title}`}
                onClick={() => onOpenTask(task)}
              >
                {openAction.kind === 'goto' ? 'Go to pane' : 'Open'}
              </button>
            )}
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
          <div className="td-input-meta td-mono">
            {hostLabel(task.host)} · {task.agent} · tmux: {tmuxSession}
          </div>
        </header>

        {notice && (
          <div role="status" className="td-input-note">
            {notice}
          </div>
        )}
        {pane && (
          <p className="td-input-note">
            Pane {pane.paneTitle} shows this tmux session too. Input from either reaches the same session, and
            the window takes the size of the client that was used last.
          </p>
        )}
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
                  <strong>Connecting to tmux {tmuxSession}…</strong>
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
          {!sheet && ' · Cmd/Ctrl+Shift+Enter maximizes'} · Closing leaves the agent and its tmux session running
        </footer>
      </div>
    </div>
  )
}
