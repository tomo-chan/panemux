import { useEffect, useRef } from 'react'
import type { RefObject } from 'react'
import { isTerminalCloseShortcut, isTerminalMaximizeShortcut } from '../utils/taskBoard'

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

interface TerminalPopupKeyboardOptions {
  dialogRef: RefObject<HTMLElement | null>
  /** The terminal's area: every key typed inside it is the terminal's own. */
  bodyRef: RefObject<HTMLElement | null>
  /** The popup's header, the one place Escape closes from. */
  headSelector: string
  /** Whether the terminal takes input. */
  connected: boolean
  /** A full sheet, which has no maximize. */
  sheet: boolean
  onClose: () => void
  onToggleMaximize: () => void
}

/**
 * The keyboard rules of the task dashboard's terminal popups — a task's Type
 * in pane (issue #284) and a host's (issue #314). Focus starts on the popup
 * and is caught there when the terminal stops taking input; Cmd/Ctrl+Shift+Esc
 * closes and Cmd/Ctrl+Shift+Enter maximizes from anywhere, before a focused
 * terminal can see them; Escape closes from the header only; Tab cycles
 * within the popup, skipping an inert terminal.
 */
export function useTerminalPopupKeyboard({
  dialogRef,
  bodyRef,
  headSelector,
  connected,
  sheet,
  onClose,
  onToggleMaximize,
}: TerminalPopupKeyboardOptions): void {
  const handlers = useRef({ onClose, onToggleMaximize, sheet, headSelector })
  handlers.current = { onClose, onToggleMaximize, sheet, headSelector }

  // Focus starts on the popup itself; the terminal takes it once connected.
  useEffect(() => {
    dialogRef.current?.focus({ preventScroll: true })
  }, [])

  // A terminal that stops taking input becomes inert, which drops its focus
  // on the floor; catch it here so keys still land inside the popup.
  useEffect(() => {
    const dialog = dialogRef.current
    if (!dialog || connected) return
    if (!dialog.contains(document.activeElement)) dialog.focus({ preventScroll: true })
  }, [connected])

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
        if (!(target instanceof Element && target.closest(handlers.current.headSelector))) return
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
}
