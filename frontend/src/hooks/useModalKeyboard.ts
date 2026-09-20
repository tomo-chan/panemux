import { useEffect } from 'react'
import type { RefObject } from 'react'

// What a browser would move focus between inside the dialog. Elements taken
// out of the tab order (`tabindex="-1"`) and disabled controls are excluded
// for the same reason the browser skips them.
//
// Visibility is deliberately not filtered: jsdom reports no layout, so a
// `offsetParent`-style check would silently exclude everything under test
// while behaving differently in a browser. A dialog that renders a hidden
// focusable control is the case this would miss, and none does today.
const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

interface UseModalKeyboardOptions {
  isOpen: boolean
  /** The element carrying `role="dialog"`, not the panel inside it. */
  dialogRef: RefObject<HTMLElement | null>
  /**
   * Called on Escape. Omit it to refuse dismissal — a dialog mid-save passes
   * `undefined` — which leaves the focus trap in place either way.
   */
  onEscape?: () => void
}

/**
 * useModalKeyboard gives an `aria-modal` dialog the two keyboard behaviours
 * that attribute promises but does not provide.
 *
 * **Focus stays inside.** The background behind a modal stays mounted and
 * interactive, so a dialog that only moves focus once, on open, lets the next
 * Tab reach the very controls the dialog is about. Tab and Shift+Tab cycle
 * within the dialog, and a Tab arriving from outside is pulled back in —
 * forwards to the first focusable element, backwards to the last, the order a
 * browser would have used had the background been inert.
 *
 * **Escape is heard.** The listener is registered on the capture phase because
 * a focused xterm terminal stops keydown propagation, so a bubble-phase window
 * listener never sees the key. That is not an edge case: it is exactly the
 * state the trap above exists to prevent, and the one an already-open dialog
 * can be left in by a shortcut that opened it while the terminal had focus.
 *
 * A nested modal wins. `PaneSettingsDialog` renders its directory browser
 * inside itself, and while that is open the trap follows it, so the form
 * behind stays out of reach.
 */
export function useModalKeyboard({ isOpen, dialogRef, onEscape }: UseModalKeyboardOptions): void {
  useEffect(() => {
    if (!isOpen) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        onEscape?.()
        return
      }
      if (event.key !== 'Tab') return

      const dialog = dialogRef.current
      if (!dialog) return
      const scope = dialog.querySelector<HTMLElement>('[role="dialog"][aria-modal="true"]') ?? dialog

      const focusable = Array.from(scope.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
      if (focusable.length === 0) {
        // Nothing to move to, so the only options are "stay" and "leave the
        // modal". Escape is still the way out.
        event.preventDefault()
        return
      }

      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      const active = document.activeElement as HTMLElement | null

      if (!active || !scope.contains(active)) {
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
  }, [isOpen, dialogRef, onEscape])
}
