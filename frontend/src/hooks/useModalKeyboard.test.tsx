import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { useRef } from 'react'
import { useModalKeyboard } from './useModalKeyboard'

afterEach(() => {
  document.body.innerHTML = ''
})

interface HarnessProps {
  isOpen: boolean
  onEscape?: () => void
  children?: React.ReactNode
}

// The harness mirrors how a dialog uses the hook: a ref on the element that
// carries role="dialog", and whatever it renders inside it.
function Harness({ isOpen, onEscape, children }: HarnessProps) {
  const dialogRef = useRef<HTMLDivElement>(null)
  useModalKeyboard({ isOpen, dialogRef, onEscape })

  return (
    <div>
      <button type="button">Behind</button>
      {isOpen && (
        <div ref={dialogRef} role="dialog" aria-modal="true" aria-label="Test dialog">
          {children ?? (
            <>
              <button type="button">First</button>
              <input aria-label="Middle" />
              <button type="button">Last</button>
            </>
          )}
        </div>
      )}
    </div>
  )
}

const press = (element: Element, key: string, shiftKey = false) =>
  fireEvent.keyDown(element, { key, shiftKey })

describe('useModalKeyboard', () => {
  describe('escape', () => {
    it('calls onEscape on the capture phase, so an element that stops propagation cannot swallow it', () => {
      const onEscape = vi.fn()
      render(
        <Harness isOpen onEscape={onEscape}>
          <button type="button" onKeyDown={(event) => event.stopPropagation()}>
            Swallows keys
          </button>
        </Harness>,
      )

      press(screen.getByRole('button', { name: 'Swallows keys' }), 'Escape')

      expect(onEscape).toHaveBeenCalledTimes(1)
    })

    it('does nothing while closed', () => {
      const onEscape = vi.fn()
      render(<Harness isOpen={false} onEscape={onEscape} />)

      press(screen.getByRole('button', { name: 'Behind' }), 'Escape')

      expect(onEscape).not.toHaveBeenCalled()
    })

    it('stops listening once it closes', () => {
      const onEscape = vi.fn()
      const { rerender } = render(<Harness isOpen onEscape={onEscape} />)

      rerender(<Harness isOpen={false} onEscape={onEscape} />)
      press(screen.getByRole('button', { name: 'Behind' }), 'Escape')

      expect(onEscape).not.toHaveBeenCalled()
    })

    it('leaves Escape alone when no handler is given, which is how a dialog refuses to be dismissed mid-save', () => {
      render(<Harness isOpen />)
      const first = screen.getByRole('button', { name: 'First' })
      first.focus()

      // No throw, and the trap still works.
      press(first, 'Escape')
      press(first, 'Tab', true)

      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Last' }))
    })
  })

  describe('focus trap', () => {
    it('wraps Tab from the last focusable element to the first', () => {
      render(<Harness isOpen />)
      const last = screen.getByRole('button', { name: 'Last' })
      last.focus()

      press(last, 'Tab')

      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'First' }))
    })

    it('wraps Shift+Tab from the first focusable element to the last', () => {
      render(<Harness isOpen />)
      const first = screen.getByRole('button', { name: 'First' })
      first.focus()

      press(first, 'Tab', true)

      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Last' }))
    })

    it('leaves a Tab in the middle of the dialog to the browser', () => {
      render(<Harness isOpen />)
      const middle = screen.getByLabelText('Middle')
      middle.focus()

      const event = fireEvent.keyDown(middle, { key: 'Tab' })

      // fireEvent returns false when preventDefault was called.
      expect(event).toBe(true)
      expect(document.activeElement).toBe(middle)
    })

    it.each([
      ['Tab', false, 'First'],
      ['Shift+Tab', true, 'Last'],
    ])('pulls focus back in on %s when it is outside the dialog', (_name, shiftKey, landsOn) => {
      render(<Harness isOpen />)
      const outside = screen.getByRole('button', { name: 'Behind' })
      outside.focus()

      press(outside, 'Tab', shiftKey)

      expect(document.activeElement).toBe(screen.getByRole('button', { name: landsOn }))
    })

    it('skips a disabled control', () => {
      render(
        <Harness isOpen>
          <button type="button">First</button>
          <button type="button" disabled>
            Saving…
          </button>
        </Harness>,
      )
      const first = screen.getByRole('button', { name: 'First' })
      first.focus()

      press(first, 'Tab', true)

      expect(document.activeElement).toBe(first)
    })

    it('skips an element explicitly taken out of the tab order', () => {
      render(
        <Harness isOpen>
          <button type="button">First</button>
          <div tabIndex={-1}>Programmatically focusable only</div>
          <button type="button">Last</button>
        </Harness>,
      )
      const last = screen.getByRole('button', { name: 'Last' })
      last.focus()

      press(last, 'Tab')

      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'First' }))
    })

    it('blocks Tab rather than letting it escape when the dialog holds nothing focusable', () => {
      render(
        <Harness isOpen>
          <p>Nothing to focus here.</p>
        </Harness>,
      )
      const outside = screen.getByRole('button', { name: 'Behind' })
      outside.focus()

      const event = fireEvent.keyDown(outside, { key: 'Tab' })

      expect(event).toBe(false)
      expect(document.activeElement).toBe(outside)
    })

    it('traps inside the innermost dialog when one is nested in another', () => {
      render(
        <Harness isOpen>
          <button type="button">Outer</button>
          <div role="dialog" aria-modal="true" aria-label="Inner">
            <button type="button">Inner first</button>
            <button type="button">Inner last</button>
          </div>
        </Harness>,
      )
      const innerLast = screen.getByRole('button', { name: 'Inner last' })
      innerLast.focus()

      press(innerLast, 'Tab')

      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Inner first' }))
    })

    it('leaves keys other than Tab and Escape alone', () => {
      render(<Harness isOpen />)
      const last = screen.getByRole('button', { name: 'Last' })
      last.focus()

      press(last, 'ArrowDown')

      expect(document.activeElement).toBe(last)
    })

    it('does nothing before the ref is attached', () => {
      const onEscape = vi.fn()
      function NoRef() {
        const dialogRef = useRef<HTMLDivElement>(null)
        useModalKeyboard({ isOpen: true, dialogRef, onEscape })
        return <button type="button">Only me</button>
      }
      render(<NoRef />)
      const only = screen.getByRole('button', { name: 'Only me' })
      only.focus()

      const event = fireEvent.keyDown(only, { key: 'Tab' })

      expect(event).toBe(true)
      expect(document.activeElement).toBe(only)
    })
  })
})
