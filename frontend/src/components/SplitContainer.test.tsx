import { useState } from 'react'
import { render, act, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { LayoutRenderer, LayoutActionsContext, LayoutActionsContextValue, dividerHitAreaStyle, dividerOverlayStyle, DIVIDER_DROP_ZONE_THICKNESS, SplitContainer, workspaceDropZoneStyle, WORKSPACE_DROP_ZONE_THICKNESS } from './SplitContainer'
import { LayoutChild, LayoutNode } from '../types'

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
    onCreatePaneBeside: vi.fn(),
    onClose: vi.fn(),
    onMaximize: vi.fn(),
    onSettings: vi.fn(),
    onSwapPanes: vi.fn(),
    onMovePaneToWorkspaceEdge: vi.fn(),
    onMovePaneBeside: vi.fn(),
    maximizedPaneId,
    dragSourcePaneId: null,
    setDragSourcePaneId: vi.fn(),
    displayConfig: { show_header: false, show_status_bar: false },
    onPaneAttention: vi.fn(),
    clearPaneAttention: vi.fn(),
    hasPaneAttention: vi.fn(() => false),
    activePaneId: null,
    setActivePaneId: vi.fn(),
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

describe('SplitContainer drop zones', () => {
  it('uses the expanded thickness for workspace edge drop zones', () => {
    expect(workspaceDropZoneStyle('top')).toMatchObject({ height: WORKSPACE_DROP_ZONE_THICKNESS })
    expect(workspaceDropZoneStyle('bottom')).toMatchObject({ height: WORKSPACE_DROP_ZONE_THICKNESS })
    expect(workspaceDropZoneStyle('left')).toMatchObject({ width: WORKSPACE_DROP_ZONE_THICKNESS })
    expect(workspaceDropZoneStyle('right')).toMatchObject({ width: WORKSPACE_DROP_ZONE_THICKNESS })
    expect(workspaceDropZoneStyle('top', true)).toMatchObject({ backgroundColor: 'rgba(86, 156, 214, 0.2)' })
  })

  it('uses the expanded thickness for divider drop overlays', () => {
    expect(dividerOverlayStyle('horizontal', false)).toMatchObject({ width: DIVIDER_DROP_ZONE_THICKNESS, marginLeft: -10 })
    expect(dividerOverlayStyle('vertical', true)).toMatchObject({ height: DIVIDER_DROP_ZONE_THICKNESS, marginTop: -10 })
  })

  it('uses the expanded thickness for divider hit areas without changing resize thickness', () => {
    expect(dividerHitAreaStyle('horizontal', true)).toMatchObject({ width: DIVIDER_DROP_ZONE_THICKNESS, left: -10, pointerEvents: 'auto' })
    expect(dividerHitAreaStyle('vertical', false)).toMatchObject({ height: DIVIDER_DROP_ZONE_THICKNESS, top: -10, pointerEvents: 'none' })
  })

  it('moves a dragged pane to the workspace edge on drop', () => {
    const ctx = makeCtx(null)
    ctx.dragSourcePaneId = 'pane-source'
    const layout: LayoutNode = { direction: 'horizontal', children }

    const { container } = render(
      <LayoutActionsContext.Provider value={ctx}>
        <SplitContainer layout={layout} onLayoutChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )

    const dropZone = container.querySelector('[data-workspace-drop-edge="top"]')
    expect(dropZone).not.toBeNull()

    fireEvent.dragOver(dropZone!, { dataTransfer: { dropEffect: 'none' } })
    fireEvent.drop(dropZone!, { dataTransfer: { dropEffect: 'none' } })

    expect(ctx.onMovePaneToWorkspaceEdge).toHaveBeenCalledWith('pane-source', 'top')
    expect(ctx.setDragSourcePaneId).toHaveBeenCalledWith(null)
  })

  it('moves a dragged pane beside the divider boundary pane on drop', () => {
    const ctx = makeCtx(null)
    ctx.dragSourcePaneId = 'pane-source'

    const { container } = render(
      <LayoutActionsContext.Provider value={ctx}>
        <LayoutRenderer direction="horizontal" children={children} onChildrenChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )

    const dropZone = container.querySelector('[data-divider-drop-zone="horizontal"]')
    expect(dropZone).not.toBeNull()

    fireEvent.dragOver(dropZone!, { dataTransfer: { dropEffect: 'none' } })
    fireEvent.drop(dropZone!, { dataTransfer: { dropEffect: 'none' } })

    expect(ctx.onMovePaneBeside).toHaveBeenCalledWith('pane-source', 'p1', 'right')
    expect(ctx.setDragSourcePaneId).toHaveBeenCalledWith(null)
  })
})
