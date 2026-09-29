import { renderHook } from '@testing-library/react'
import { describe, it, expect, afterEach } from 'vitest'
import { useRestoreFocusOnClose } from './useRestoreFocusOnClose'

describe('useRestoreFocusOnClose', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('restores focus to the element that was focused when it opened, once it closes', () => {
    const trigger = document.createElement('button')
    trigger.textContent = 'Open'
    document.body.appendChild(trigger)
    trigger.focus()
    expect(document.activeElement).toBe(trigger)

    const { rerender } = renderHook(({ isOpen }) => useRestoreFocusOnClose(isOpen), {
      initialProps: { isOpen: false },
    })

    rerender({ isOpen: true })
    // Simulate the panel's own content stealing focus while open (e.g. an
    // input auto-focusing).
    const inner = document.createElement('input')
    document.body.appendChild(inner)
    inner.focus()
    expect(document.activeElement).toBe(inner)

    rerender({ isOpen: false })

    expect(document.activeElement).toBe(trigger)
  })

  it('is a no-op when nothing was focused before opening', () => {
    document.body.innerHTML = '<div></div>'
    ;(document.activeElement as HTMLElement | null)?.blur?.()

    const { rerender } = renderHook(({ isOpen }) => useRestoreFocusOnClose(isOpen), {
      initialProps: { isOpen: false },
    })

    rerender({ isOpen: true })
    rerender({ isOpen: false })

    // No throw, and no assertion possible on "what got focused" beyond the
    // absence of an error — document.body is the safe default when nothing
    // else claims focus.
    expect(document.activeElement).not.toBeNull()
  })

  it('does not throw when the previously focused element has since been removed from the DOM', () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    trigger.focus()

    const { rerender } = renderHook(({ isOpen }) => useRestoreFocusOnClose(isOpen), {
      initialProps: { isOpen: false },
    })
    rerender({ isOpen: true })

    trigger.remove()

    expect(() => rerender({ isOpen: false })).not.toThrow()
  })

  it('restores focus on unmount while still open', () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    trigger.focus()

    const { rerender, unmount } = renderHook(({ isOpen }) => useRestoreFocusOnClose(isOpen), {
      initialProps: { isOpen: false },
    })
    rerender({ isOpen: true })

    const inner = document.createElement('input')
    document.body.appendChild(inner)
    inner.focus()

    unmount()

    expect(document.activeElement).toBe(trigger)
  })

  it('does not throw on unmount when the previously focused element is gone', () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    trigger.focus()

    const { rerender, unmount } = renderHook(({ isOpen }) => useRestoreFocusOnClose(isOpen), {
      initialProps: { isOpen: false },
    })
    rerender({ isOpen: true })
    trigger.remove()

    expect(() => unmount()).not.toThrow()
  })
})
