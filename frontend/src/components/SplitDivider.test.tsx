import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { SplitDivider } from './SplitDivider'

afterEach(() => {
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
})

function renderDivider(direction: 'horizontal' | 'vertical', onDrag = vi.fn()) {
  render(
    <div data-testid="wrap">
      <SplitDivider direction={direction} onDrag={onDrag} />
    </div>,
  )
  const divider = screen.getByTestId('wrap').firstElementChild as HTMLElement
  return { divider, onDrag }
}

describe('SplitDivider', () => {
  // efficacy:exempt pins pre-existing behavior — SplitDivider.tsx is
  // unchanged on this branch; #178's G5 asked for the missing tests.
  it.each([
    ['horizontal' as const, 'col-resize', '4px', '100%'],
    ['vertical' as const, 'row-resize', '100%', '4px'],
  ])('lays out and hints the cursor for a %s split', (direction, cursor, width, height) => {
    const { divider } = renderDivider(direction)

    expect(divider.style.cursor).toBe(cursor)
    expect(divider.style.width).toBe(width)
    expect(divider.style.height).toBe(height)
  })

  // efficacy:exempt pins pre-existing behavior — SplitDivider.tsx is
  // unchanged on this branch; #178's G5 asked for the missing tests.
  it('reports each horizontal move as a delta from the previous position', () => {
    const { divider, onDrag } = renderDivider('horizontal')

    fireEvent.mouseDown(divider, { clientX: 100, clientY: 0 })
    fireEvent.mouseMove(document, { clientX: 130, clientY: 0 })
    fireEvent.mouseMove(document, { clientX: 120, clientY: 0 })

    expect(onDrag.mock.calls).toEqual([[30], [-10]])
  })

  // efficacy:exempt pins pre-existing behavior — SplitDivider.tsx is
  // unchanged on this branch; #178's G5 asked for the missing tests.
  it('follows the other axis for a vertical split', () => {
    const { divider, onDrag } = renderDivider('vertical')

    fireEvent.mouseDown(divider, { clientX: 0, clientY: 40 })
    fireEvent.mouseMove(document, { clientX: 999, clientY: 55 })

    expect(onDrag.mock.calls).toEqual([[15]])
  })

  // efficacy:exempt pins pre-existing behavior — SplitDivider.tsx is
  // unchanged on this branch; #178's G5 asked for the missing tests.
  it('reports to the latest callback, so a re-rendered parent is not dragged against stale state', () => {
    const first = vi.fn()
    const latest = vi.fn()
    const { container } = render(<SplitDivider direction="horizontal" onDrag={first} />)
    const divider = container.firstElementChild as HTMLElement

    fireEvent.mouseDown(divider, { clientX: 0 })
    render(<SplitDivider direction="horizontal" onDrag={latest} />, { container })
    fireEvent.mouseMove(document, { clientX: 10 })

    expect(latest).toHaveBeenCalledWith(10)
    expect(first).not.toHaveBeenCalled()
  })

  // efficacy:exempt pins pre-existing behavior — SplitDivider.tsx is
  // unchanged on this branch; #178's G5 asked for the missing tests.
  it('takes over the page cursor while dragging and gives it back on release', () => {
    const { divider } = renderDivider('horizontal')

    fireEvent.mouseDown(divider, { clientX: 0 })
    expect(document.body.style.cursor).toBe('col-resize')
    expect(document.body.style.userSelect).toBe('none')

    fireEvent.mouseUp(document)
    expect(document.body.style.cursor).toBe('')
    expect(document.body.style.userSelect).toBe('')
  })

  // efficacy:exempt pins pre-existing behavior — SplitDivider.tsx is
  // unchanged on this branch; #178's G5 asked for the missing tests.
  it('stops reporting after the button is released', () => {
    const { divider, onDrag } = renderDivider('horizontal')

    fireEvent.mouseDown(divider, { clientX: 0 })
    fireEvent.mouseUp(document)
    fireEvent.mouseMove(document, { clientX: 500 })

    expect(onDrag).not.toHaveBeenCalled()
  })

  // efficacy:exempt pins pre-existing behavior — SplitDivider.tsx is
  // unchanged on this branch; #178's G5 asked for the missing tests.
  it('highlights on hover and returns to the chrome colour when the pointer leaves', () => {
    const { divider } = renderDivider('horizontal')

    fireEvent.mouseEnter(divider)
    expect(divider.style.backgroundColor).toBe('rgb(86, 156, 214)')

    fireEvent.mouseLeave(divider)
    expect(divider.style.backgroundColor).toBe('rgb(51, 51, 51)')
  })
})
