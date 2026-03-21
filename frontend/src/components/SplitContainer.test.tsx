import { useState } from 'react'
import { render, act } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { LayoutRenderer, LayoutActionsContext, LayoutActionsContextValue } from './SplitContainer'
import { LayoutChild } from '../types'

// Stub TerminalPane and SplitDivider so we don't need xterm.js or drag logic
vi.mock('./TerminalPane', () => ({
  TerminalPane: ({ pane }: { pane: { id: string } }) => (
    <div data-pane-id={pane.id} />
  ),
}))
vi.mock('./SplitDivider', () => ({
  SplitDivider: () => <div data-divider />,
}))

const pane1: LayoutChild = { size: 50, pane: { id: 'p1', type: 'local' } }
const pane2: LayoutChild = { size: 50, pane: { id: 'p2', type: 'local' } }
const children = [pane1, pane2]

function makeCtx(maximizedPaneId: string | null): LayoutActionsContextValue {
  return {
    onSplit: vi.fn(),
    onClose: vi.fn(),
    onMaximize: vi.fn(),
    maximizedPaneId,
    displayConfig: { show_header: false, show_status_bar: false },
    editMode: false,
  }
}

/** Returns the direct child wrapper divs of the LayoutRenderer flex container. */
function getWrapperDivs(container: HTMLElement): HTMLElement[] {
  // LayoutRenderer renders one flex div; find it regardless of surrounding elements
  const flex = container.querySelector('[style*="display: flex"]') as HTMLElement
  return Array.from(flex.children).filter(
    (el) => !(el as HTMLElement).dataset.divider,
  ) as HTMLElement[]
}

describe('LayoutRenderer divider visibility', () => {
  it('hides the divider when a pane is maximized', () => {
    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx('p1')}>
        <LayoutRenderer direction="horizontal" children={children} onChildrenChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )
    expect(container.querySelector('[data-divider]')).toBeNull()
  })

  it('shows the divider when no pane is maximized', () => {
    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx(null)}>
        <LayoutRenderer direction="horizontal" children={children} onChildrenChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )
    expect(container.querySelector('[data-divider]')).not.toBeNull()
  })
})

describe('LayoutRenderer maximize CSS', () => {
  it('applies absolute positioning to the maximized child wrapper only', () => {
    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx('p1')}>
        <LayoutRenderer direction="horizontal" children={children} onChildrenChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )

    const [wrapper1, wrapper2] = getWrapperDivs(container)
    expect(wrapper1.style.position).toBe('absolute')
    expect(wrapper1.style.inset).toBeTruthy()
    expect(Number(wrapper1.style.zIndex)).toBeGreaterThan(0)
    expect(wrapper2.style.position).not.toBe('absolute')
  })

  it('does not apply absolute positioning when no pane is maximized', () => {
    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx(null)}>
        <LayoutRenderer direction="horizontal" children={children} onChildrenChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )

    const [wrapper1, wrapper2] = getWrapperDivs(container)
    expect(wrapper1.style.position).not.toBe('absolute')
    expect(wrapper2.style.position).not.toBe('absolute')
  })

  it('toggles maximize CSS when maximizedPaneId changes', () => {
    function Wrapper() {
      const [maxId, setMaxId] = useState<string | null>(null)
      return (
        <LayoutActionsContext.Provider value={{ ...makeCtx(maxId), onMaximize: setMaxId }}>
          <button onClick={() => setMaxId(maxId ? null : 'p1')}>toggle</button>
          <LayoutRenderer direction="horizontal" children={children} onChildrenChange={vi.fn()} />
        </LayoutActionsContext.Provider>
      )
    }

    const { container, getByRole } = render(<Wrapper />)

    // Initially not maximized
    const [w1a] = getWrapperDivs(container)
    expect(w1a.style.position).not.toBe('absolute')

    // Maximize p1
    act(() => { getByRole('button').click() })
    const [w1b, w2b] = getWrapperDivs(container)
    expect(w1b.style.position).toBe('absolute')
    expect(w2b.style.position).not.toBe('absolute')

    // Restore
    act(() => { getByRole('button').click() })
    const [w1c] = getWrapperDivs(container)
    expect(w1c.style.position).not.toBe('absolute')
  })
})
