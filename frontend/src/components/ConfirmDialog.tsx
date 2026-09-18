import React, { useEffect, useRef } from 'react'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface ConfirmDialogProps {
  isOpen: boolean
  title: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  /** Styles the confirm button as destructive. The action itself is the caller's. */
  isDestructive?: boolean
  onConfirm: () => void
  onCancel: () => void
}

const buttonStyle: React.CSSProperties = {
  padding: '5px 14px',
  border: '1px solid #555',
  borderRadius: '3px',
  backgroundColor: '#3c3c3c',
  color: '#d4d4d4',
  fontFamily: TERMINAL_FONT_FAMILY,
  fontSize: '13px',
  cursor: 'pointer',
}

/**
 * ConfirmDialog asks a yes/no question in the app's own chrome.
 *
 * It replaces `window.confirm` for workspace deletion (issue #70): the native
 * dialog blocks the main thread — every terminal in the page stops rendering
 * while it is up — and cannot be styled or driven by the rest of the UI.
 *
 * Cancelling is deliberately the easy path: Escape, the backdrop and the
 * cancel button all reach `onCancel`, and nothing but the confirm button
 * reaches `onConfirm`.
 *
 * Focus is trapped between its two buttons while it is open. The background is
 * still mounted and interactive, so a dialog that only moved focus once would
 * let the next Tab reach the very controls the question is about.
 */
export const ConfirmDialog: React.FC<ConfirmDialogProps> = ({
  isOpen,
  title,
  message,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  isDestructive = false,
  onConfirm,
  onCancel,
}) => {
  const confirmRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)

  // Capture phase, like the app's other overlays (see BoardDashboardPanel): a
  // focused xterm terminal stops keydown propagation, so a bubble-phase window
  // listener never sees the key at all.
  useEffect(() => {
    if (!isOpen) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        onCancel()
        return
      }
      if (event.key !== 'Tab') return

      // The background behind an aria-modal dialog stays mounted and
      // interactive, so without this a Tab walks out of the question and into
      // the workspace controls it is asking about — and once focus reaches a
      // terminal, the Escape above is the only way back and the terminal eats
      // the keystroke it travels on.
      const panel = panelRef.current
      if (!panel) return
      const focusable = Array.from(panel.querySelectorAll<HTMLElement>('button:not([disabled])'))
      if (focusable.length === 0) return

      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      const active = document.activeElement as HTMLElement | null

      if (!active || !panel.contains(active)) {
        event.preventDefault()
        ;(event.shiftKey ? last : first).focus()
        return
      }
      if (event.shiftKey && active === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && active === last) {
        event.preventDefault()
        first.focus()
      }
    }

    window.addEventListener('keydown', handleKeyDown, true)
    return () => window.removeEventListener('keydown', handleKeyDown, true)
  }, [isOpen, onCancel])

  // Focus the confirm button rather than the dialog itself: it puts both
  // answers one keystroke away (Enter confirms, Escape cancels) without the
  // focus having to be moved first.
  useEffect(() => {
    if (isOpen) confirmRef.current?.focus()
  }, [isOpen])

  if (!isOpen) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={title}
      data-testid="confirm-dialog-backdrop"
      style={{
        position: 'fixed',
        inset: 0,
        backgroundColor: 'rgba(0, 0, 0, 0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1100,
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget) onCancel()
      }}
    >
      <div
        ref={panelRef}
        style={{
          backgroundColor: '#252526',
          border: '1px solid #444',
          borderRadius: '6px',
          padding: '20px 24px',
          width: '360px',
          maxWidth: 'calc(100vw - 32px)',
          boxSizing: 'border-box',
          fontFamily: TERMINAL_FONT_FAMILY,
          color: '#d4d4d4',
        }}
      >
        <div style={{ fontSize: '14px', fontWeight: 600, marginBottom: '12px', color: '#e0e0e0' }}>
          {title}
        </div>
        <div style={{ fontSize: '13px', lineHeight: 1.5, marginBottom: '20px' }}>{message}</div>
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
          <button type="button" style={buttonStyle} onClick={onCancel}>
            {cancelLabel}
          </button>
          <button
            type="button"
            ref={confirmRef}
            data-tone={isDestructive ? 'danger' : 'default'}
            style={{
              ...buttonStyle,
              ...(isDestructive
                ? { backgroundColor: '#5a1d1d', borderColor: '#7f1d1d', color: '#fca5a5' }
                : { backgroundColor: '#0e639c', borderColor: '#0e639c', color: '#ffffff' }),
            }}
            onClick={onConfirm}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
