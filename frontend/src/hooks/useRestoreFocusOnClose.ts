import { useEffect, useRef } from 'react'

// useRestoreFocusOnClose returns focus to whatever element had it right
// before an overlay panel opened, once that panel closes (or unmounts while
// still open). Shared by CommandPalette, CommandHistoryPanel, and
// BoardDashboardPanel — all three are dismissable overlays that otherwise
// leave focus stranded on whatever was inside them (or on nothing at all,
// if the closing click/keypress target itself gets removed from the DOM).
export function useRestoreFocusOnClose(isOpen: boolean): void {
  const previouslyFocusedRef = useRef<HTMLElement | null>(null)
  const wasOpenRef = useRef(false)

  useEffect(() => {
    if (isOpen && !wasOpenRef.current) {
      previouslyFocusedRef.current =
        document.activeElement instanceof HTMLElement ? document.activeElement : null
    } else if (!isOpen && wasOpenRef.current) {
      restoreFocus(previouslyFocusedRef.current)
      previouslyFocusedRef.current = null
    }
    wasOpenRef.current = isOpen
  }, [isOpen])

  // Deliberately empty deps: this effect's cleanup only needs to run once,
  // on unmount, to restore focus if the panel was still open at that point.
  // Depending on isOpen here would run this cleanup on every open/close
  // transition too, fighting the effect above.
  useEffect(() => {
    return () => {
      if (wasOpenRef.current) restoreFocus(previouslyFocusedRef.current)
    }
  }, [])
}

function restoreFocus(element: HTMLElement | null): void {
  if (element && document.contains(element)) element.focus()
}
