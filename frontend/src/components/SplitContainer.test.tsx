import { useState } from 'react'
import { render, act, fireEvent } from '@testing-library/react'
import { beforeEach, describe, it, expect, vi } from 'vitest'
import { LayoutRenderer, LayoutActionsContext, LayoutActionsContextValue, dividerHitAreaStyle, dividerOverlayStyle, DIVIDER_DROP_ZONE_THICKNESS, SplitContainer, workspaceDropZoneStyle, WORKSPACE_DROP_ZONE_THICKNESS } from './SplitContainer'
import { LayoutChild, LayoutNode } from '../types'

// Stub TerminalPane and SplitDivider so we don't need xterm.js or drag logic.
//
// The divider stub keeps `data-divider` — the attribute the visibility tests
// below query — and additionally records the `onDrag` callback it is handed.
// That callback is the only way into LayoutRenderer's resize arithmetic: it is
// not reachable through a DOM event, because the real SplitDivider owns the
// pointer handling that produces the delta (covered in SplitDivider.test.tsx).
vi.mock('./TerminalPane', () => ({
  TerminalPane: ({ pane }: { pane: { id: string } }) => (
    <div data-pane-id={pane.id} />
  ),
}))
const { dividerDrags } = vi.hoisted(() => ({
  dividerDrags: [] as { direction: 'horizontal' | 'vertical', onDrag: (delta: number) => void }[],
}))
vi.mock('./SplitDivider', () => ({
  SplitDivider: ({ direction, onDrag }: { direction: 'horizontal' | 'vertical', onDrag: (delta: number) => void }) => {
    dividerDrags.push({ direction, onDrag })
    return <div data-divider data-divider-direction={direction} />
  },
}))

beforeEach(() => {
  dividerDrags.length = 0
})

/** The most recently rendered divider's resize callback. */
function latestDividerDrag(): (delta: number) => void {
  const drag = dividerDrags[dividerDrags.length - 1]
  if (!drag) throw new Error('no divider was rendered')
  return drag.onDrag
}

/**
 * The resize callback of the first divider on the given axis. Nested layouts
 * render an inner divider before the outer one that contains it, so "latest" is
 * the wrong end of the list when a test means the inner split.
 */
function dividerDragOn(direction: 'horizontal' | 'vertical'): (delta: number) => void {
  const drag = dividerDrags.find((d) => d.direction === direction)
  if (!drag) throw new Error(`no ${direction} divider was rendered`)
  return drag.onDrag
}

/**
 * jsdom reports every element as 0x0, and handleDrag divides by the container's
 * own size, so a resize test has to say how big the container is or it only
 * ever exercises the `usableSize <= 0` guard.
 */
function stubFlexContainerSize(container: HTMLElement, size: { width?: number, height?: number }): HTMLElement {
  const flex = container.querySelector('[style*="display: flex"]') as HTMLElement
  if (size.width !== undefined) {
    Object.defineProperty(flex, 'offsetWidth', { configurable: true, value: size.width })
  }
  if (size.height !== undefined) {
    Object.defineProperty(flex, 'offsetHeight', { configurable: true, value: size.height })
  }
  return flex
}

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

describe('LayoutRenderer resizing', () => {
  /** A container 404px wide leaves exactly 400px of usable size once the 4px divider is taken off. */
  const USABLE_CONTAINER_WIDTH = 404

  function renderResizable(direction: 'horizontal' | 'vertical', layoutChildren: LayoutChild[] = children) {
    const onChildrenChange = vi.fn()
    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx(null)}>
        <LayoutRenderer direction={direction} children={layoutChildren} onChildrenChange={onChildrenChange} />
      </LayoutActionsContext.Provider>,
    )
    stubFlexContainerSize(
      container,
      direction === 'horizontal' ? { width: USABLE_CONTAINER_WIDTH } : { height: USABLE_CONTAINER_WIDTH },
    )
    return { container, onChildrenChange }
  }

  it('moves the dragged percentage from one neighbour to the other', () => {
    const { onChildrenChange } = renderResizable('horizontal')

    latestDividerDrag()(40) // 40px of 400 usable = 10%

    expect(onChildrenChange).toHaveBeenCalledWith([
      expect.objectContaining({ size: 60 }),
      expect.objectContaining({ size: 40 }),
    ])
  })

  it('moves it the other way for a negative delta', () => {
    const { onChildrenChange } = renderResizable('horizontal')

    latestDividerDrag()(-40)

    expect(onChildrenChange).toHaveBeenCalledWith([
      expect.objectContaining({ size: 40 }),
      expect.objectContaining({ size: 60 }),
    ])
  })

  it('measures a vertical split against the container height rather than its width', () => {
    const { onChildrenChange } = renderResizable('vertical')

    latestDividerDrag()(40)

    expect(onChildrenChange).toHaveBeenCalledWith([
      expect.objectContaining({ size: 60 }),
      expect.objectContaining({ size: 40 }),
    ])
  })

  it('keeps every other field of the children it resizes', () => {
    const { onChildrenChange } = renderResizable('horizontal')

    latestDividerDrag()(40)

    expect(onChildrenChange).toHaveBeenCalledWith([
      { size: 60, pane: { id: 'p1', type: 'local' } },
      { size: 40, pane: { id: 'p2', type: 'local' } },
    ])
  })

  it('does not mutate the children it was handed', () => {
    const { onChildrenChange } = renderResizable('horizontal')

    latestDividerDrag()(40)

    // The caller owns this array: resizing has to hand back new objects, not
    // edit the ones React is still rendering from.
    expect(children[0].size).toBe(50)
    expect(children[1].size).toBe(50)
    expect(onChildrenChange.mock.calls[0][0][0]).not.toBe(children[0])
  })

  it('refuses a drag that would shrink the pane before the divider below 5%', () => {
    const { onChildrenChange } = renderResizable('horizontal')

    latestDividerDrag()(-185) // 50% - 46.25% = 3.75%

    expect(onChildrenChange).not.toHaveBeenCalled()
  })

  it('refuses a drag that would shrink the pane after the divider below 5%', () => {
    const { onChildrenChange } = renderResizable('horizontal')

    latestDividerDrag()(185)

    expect(onChildrenChange).not.toHaveBeenCalled()
  })

  it('allows a drag that lands exactly on the 5% floor', () => {
    const { onChildrenChange } = renderResizable('horizontal')

    latestDividerDrag()(180) // 50% + 45% = 95% / 5%

    expect(onChildrenChange).toHaveBeenCalledWith([
      expect.objectContaining({ size: 95 }),
      expect.objectContaining({ size: 5 }),
    ])
  })

  it('does nothing while the container is too small to hold its dividers', () => {
    // A container that has not been laid out yet reports 0, which leaves a
    // negative usable size once the divider is subtracted. Dividing by it
    // inverts the drag: the delta is deliberately small and negative, because
    // that is the case the `usableSize <= 0` guard is the only thing stopping.
    // A large delta would be refused by the 5% floor further down whether the
    // guard was there or not, which asserts nothing about this branch.
    const onChildrenChange = vi.fn()
    render(
      <LayoutActionsContext.Provider value={makeCtx(null)}>
        <LayoutRenderer direction="horizontal" children={children} onChildrenChange={onChildrenChange} />
      </LayoutActionsContext.Provider>,
    )

    latestDividerDrag()(-1)

    expect(onChildrenChange).not.toHaveBeenCalled()
  })

  it('resizes the right pair when a layout has more than one divider', () => {
    const three: LayoutChild[] = [
      { size: 40, pane: { id: 'a', type: 'local' } },
      { size: 30, pane: { id: 'b', type: 'local' } },
      { size: 30, pane: { id: 'c', type: 'local' } },
    ]
    const onChildrenChange = vi.fn()
    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx(null)}>
        <LayoutRenderer direction="horizontal" children={three} onChildrenChange={onChildrenChange} />
      </LayoutActionsContext.Provider>,
    )
    // Two dividers now, so 8px of the container is divider rather than pane.
    stubFlexContainerSize(container, { width: 408 })

    latestDividerDrag()(40) // the second divider, between b and c

    expect(onChildrenChange).toHaveBeenCalledWith([
      expect.objectContaining({ size: 40 }),
      expect.objectContaining({ size: 40 }),
      expect.objectContaining({ size: 20 }),
    ])
  })
})

describe('LayoutRenderer nested layouts', () => {
  const nestedChild: LayoutChild = {
    size: 50,
    direction: 'vertical',
    children: [
      { size: 50, pane: { id: 'nested-a', type: 'local' } },
      { size: 50, pane: { id: 'nested-b', type: 'local' } },
    ],
  }

  it('renders a nested split rather than a single pane', () => {
    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx(null)}>
        <LayoutRenderer direction="horizontal" children={[nestedChild, pane2]} onChildrenChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )

    expect(container.querySelector('[data-pane-id="nested-a"]')).not.toBeNull()
    expect(container.querySelector('[data-pane-id="nested-b"]')).not.toBeNull()
    expect(container.querySelector('[data-pane-id="p2"]')).not.toBeNull()
  })

  it('reports a nested resize as a change to the outer child that holds it', () => {
    const onChildrenChange = vi.fn()
    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx(null)}>
        <LayoutRenderer direction="horizontal" children={[nestedChild, pane2]} onChildrenChange={onChildrenChange} />
      </LayoutActionsContext.Provider>,
    )
    // The inner vertical renderer is the second flex container in the tree.
    const inner = container.querySelectorAll('[style*="display: flex"]')[1] as HTMLElement
    Object.defineProperty(inner, 'offsetHeight', { configurable: true, value: 404 })

    dividerDragOn('vertical')(40)

    expect(onChildrenChange).toHaveBeenCalledWith([
      {
        size: 50,
        direction: 'vertical',
        children: [
          expect.objectContaining({ size: 60 }),
          expect.objectContaining({ size: 40 }),
        ],
      },
      pane2,
    ])
  })

  it('renders the nested split when a child carries both a pane and children', () => {
    const both: LayoutChild = { ...nestedChild, pane: { id: 'root-pane', type: 'local' } }

    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx(null)}>
        <LayoutRenderer direction="horizontal" children={[both, pane2]} onChildrenChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )

    expect(container.querySelector('[data-pane-id="nested-a"]')).not.toBeNull()
    expect(container.querySelector('[data-pane-id="root-pane"]')).toBeNull()
  })

  it('renders nothing for a child that holds neither a pane nor children', () => {
    const empty: LayoutChild = { size: 50, direction: 'horizontal', children: [] }

    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx(null)}>
        <LayoutRenderer direction="horizontal" children={[empty, pane2]} onChildrenChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )

    const [wrapper] = Array.from(
      (container.querySelector('[style*="display: flex"]') as HTMLElement).children,
    ) as HTMLElement[]
    expect(wrapper.innerHTML).toBe('')
  })
})

describe('SplitContainer layout changes', () => {
  it('reports a resize as a change to the whole layout node', () => {
    const onLayoutChange = vi.fn()
    const layout: LayoutNode = { direction: 'horizontal', children }

    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx(null)}>
        <SplitContainer layout={layout} onLayoutChange={onLayoutChange} />
      </LayoutActionsContext.Provider>,
    )
    stubFlexContainerSize(container, { width: 404 })

    latestDividerDrag()(40)

    expect(onLayoutChange).toHaveBeenCalledWith({
      direction: 'horizontal',
      children: [
        expect.objectContaining({ size: 60 }),
        expect.objectContaining({ size: 40 }),
      ],
    })
  })

  it('renders no workspace drop zones while no pane is being dragged', () => {
    const layout: LayoutNode = { direction: 'horizontal', children }

    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx(null)}>
        <SplitContainer layout={layout} onLayoutChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )

    expect(container.querySelectorAll('[data-workspace-drop-edge]')).toHaveLength(0)
  })

  it('offers all four workspace edges once a pane is being dragged', () => {
    const ctx = makeCtx(null)
    ctx.dragSourcePaneId = 'pane-source'
    const layout: LayoutNode = { direction: 'horizontal', children }

    const { container } = render(
      <LayoutActionsContext.Provider value={ctx}>
        <SplitContainer layout={layout} onLayoutChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )

    expect(
      Array.from(container.querySelectorAll('[data-workspace-drop-edge]')).map(
        (el) => (el as HTMLElement).dataset.workspaceDropEdge,
      ),
    ).toEqual(['top', 'bottom', 'left', 'right'])
  })
})

describe('SplitContainer workspace edges under a mouse drag', () => {
  function renderDragging() {
    const ctx = makeCtx(null)
    ctx.dragSourcePaneId = 'pane-source'
    const layout: LayoutNode = { direction: 'horizontal', children }
    const rendered = render(
      <LayoutActionsContext.Provider value={ctx}>
        <SplitContainer layout={layout} onLayoutChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )
    const zone = (edge: string) =>
      rendered.container.querySelector(`[data-workspace-drop-edge="${edge}"]`) as HTMLElement
    return { ctx, zone }
  }

  it('highlights the edge the pointer is over', () => {
    const { zone } = renderDragging()

    expect(zone('left').style.backgroundColor).toBe('transparent')

    fireEvent.mouseEnter(zone('left'))

    expect(zone('left').style.backgroundColor).toBe('rgba(86, 156, 214, 0.2)')
    expect(zone('right').style.backgroundColor).toBe('transparent')
  })

  it('clears the highlight when the pointer leaves that same edge', () => {
    const { zone } = renderDragging()

    fireEvent.mouseEnter(zone('left'))
    fireEvent.mouseLeave(zone('left'))

    expect(zone('left').style.backgroundColor).toBe('transparent')
  })

  it('keeps the highlight when the pointer leaves a different edge', () => {
    // The leave handler only clears the edge it belongs to. Without that check,
    // the browser's enter-then-leave ordering between adjacent zones would
    // clear the highlight the enter had just set.
    const { zone } = renderDragging()

    fireEvent.mouseEnter(zone('left'))
    fireEvent.mouseLeave(zone('right'))

    expect(zone('left').style.backgroundColor).toBe('rgba(86, 156, 214, 0.2)')
  })

  it('moves the pane and ends the drag when the button is released over an edge', () => {
    const { ctx, zone } = renderDragging()

    fireEvent.mouseEnter(zone('bottom'))
    fireEvent.mouseUp(zone('bottom'))

    expect(ctx.onMovePaneToWorkspaceEdge).toHaveBeenCalledWith('pane-source', 'bottom')
    expect(ctx.setDragSourcePaneId).toHaveBeenCalledWith(null)
  })

  it('sets the drop effect so the cursor reports a move rather than a copy', () => {
    const { zone } = renderDragging()
    const dataTransfer = { dropEffect: 'none' }

    fireEvent.dragOver(zone('right'), { dataTransfer })

    expect(dataTransfer.dropEffect).toBe('move')
  })
})

describe('DividerDropZone', () => {
  function renderZone(layoutChildren: LayoutChild[], dragSourcePaneId: string | null) {
    const ctx = makeCtx(null)
    ctx.dragSourcePaneId = dragSourcePaneId
    const rendered = render(
      <LayoutActionsContext.Provider value={ctx}>
        <LayoutRenderer direction="horizontal" children={layoutChildren} onChildrenChange={vi.fn()} />
      </LayoutActionsContext.Provider>,
    )
    const zone = rendered.container.querySelector('[data-divider-drop-zone="horizontal"]') as HTMLElement
    // The zone's wrapper holds three things: the hit area, the divider itself
    // and — only while a pane is being dragged — the highlight overlay. The
    // overlay is the one carrying neither marker attribute.
    const overlay = () => {
      const siblings = Array.from(zone.parentElement?.children ?? []) as HTMLElement[]
      return siblings.find((el) => !el.dataset.dividerDropZone && el.dataset.divider === undefined) ?? null
    }
    return { ctx, zone, overlay, ...rendered }
  }

  it('ignores a drag-over while no pane is being dragged', () => {
    const { zone } = renderZone(children, null)
    const dataTransfer = { dropEffect: 'none' }

    fireEvent.dragOver(zone, { dataTransfer })

    expect(dataTransfer.dropEffect).toBe('none')
  })

  it('shows no overlay at all while no pane is being dragged', () => {
    const { overlay } = renderZone(children, null)

    expect(overlay()).toBeNull()
  })

  it('highlights the overlay on drag-over and clears it on drag-leave', () => {
    const { zone, overlay } = renderZone(children, 'pane-source')

    fireEvent.dragOver(zone, { dataTransfer: { dropEffect: 'none' } })
    expect(overlay()?.style.backgroundColor).toBe('rgba(86, 156, 214, 0.35)')

    fireEvent.dragLeave(zone)
    expect(overlay()?.style.backgroundColor).toBe('transparent')
  })

  it('highlights the overlay on mouse-enter and clears it on mouse-leave', () => {
    const { zone, overlay } = renderZone(children, 'pane-source')

    fireEvent.mouseEnter(zone)
    expect(overlay()?.style.backgroundColor).toBe('rgba(86, 156, 214, 0.35)')

    fireEvent.mouseLeave(zone)
    expect(overlay()?.style.backgroundColor).toBe('transparent')
  })

  it('does not highlight on mouse-enter while no pane is being dragged', () => {
    const { zone, overlay } = renderZone(children, null)

    fireEvent.mouseEnter(zone)

    expect(overlay()).toBeNull()
  })

  it('moves the pane when the button is released over the divider', () => {
    const { ctx, zone } = renderZone(children, 'pane-source')

    fireEvent.mouseUp(zone)

    expect(ctx.onMovePaneBeside).toHaveBeenCalledWith('pane-source', 'p1', 'right')
    expect(ctx.setDragSourcePaneId).toHaveBeenCalledWith(null)
  })

  it('does nothing when the button is released with no pane being dragged', () => {
    const { ctx, zone } = renderZone(children, null)

    fireEvent.mouseUp(zone)

    expect(ctx.onMovePaneBeside).not.toHaveBeenCalled()
  })

  it('does nothing on drop with no pane being dragged', () => {
    const { ctx, zone } = renderZone(children, null)

    fireEvent.drop(zone, { dataTransfer: { dropEffect: 'none' } })

    expect(ctx.onMovePaneBeside).not.toHaveBeenCalled()
  })

  it('uses the last pane of the split before the divider as the target', () => {
    // The divider sits between two nodes, not two panes. Which pane a drop
    // lands beside is the deepest one against that boundary.
    const nested: LayoutChild = {
      size: 50,
      direction: 'vertical',
      children: [
        { size: 50, pane: { id: 'upper', type: 'local' } },
        { size: 50, pane: { id: 'lower', type: 'local' } },
      ],
    }
    const { ctx } = renderZone([nested, pane2], 'pane-source')
    const outerZone = document.querySelectorAll('[data-divider-drop-zone="horizontal"]')[0] as HTMLElement

    fireEvent.drop(outerZone, { dataTransfer: { dropEffect: 'none' } })

    expect(ctx.onMovePaneBeside).toHaveBeenCalledWith('pane-source', 'lower', 'right')
  })

  it('falls back to the first pane after the divider when the split before it holds none', () => {
    const empty: LayoutChild = { size: 50, direction: 'vertical', children: [] }
    const { ctx, zone } = renderZone([empty, pane2], 'pane-source')

    fireEvent.drop(zone, { dataTransfer: { dropEffect: 'none' } })

    expect(ctx.onMovePaneBeside).toHaveBeenCalledWith('pane-source', 'p2', 'right')
  })

  it('reaches into the split after the divider for its first pane', () => {
    const empty: LayoutChild = { size: 50, direction: 'vertical', children: [] }
    const nestedAfter: LayoutChild = {
      size: 50,
      direction: 'vertical',
      children: [
        { size: 50, pane: { id: 'first-below', type: 'local' } },
        { size: 50, pane: { id: 'second-below', type: 'local' } },
      ],
    }
    const { ctx, zone } = renderZone([empty, nestedAfter], 'pane-source')

    fireEvent.drop(zone, { dataTransfer: { dropEffect: 'none' } })

    expect(ctx.onMovePaneBeside).toHaveBeenCalledWith('pane-source', 'first-below', 'right')
  })

  it('does nothing when neither side of the divider holds a pane', () => {
    const empty = (): LayoutChild => ({ size: 50, direction: 'vertical', children: [] })
    const { ctx, zone } = renderZone([empty(), empty()], 'pane-source')

    fireEvent.drop(zone, { dataTransfer: { dropEffect: 'none' } })
    fireEvent.mouseUp(zone)

    expect(ctx.onMovePaneBeside).not.toHaveBeenCalled()
    expect(ctx.setDragSourcePaneId).not.toHaveBeenCalled()
  })
})

describe('divider overlay styling', () => {
  it('is transparent until the divider is the drop target, on both axes', () => {
    expect(dividerOverlayStyle('horizontal', false).backgroundColor).toBe('transparent')
    expect(dividerOverlayStyle('vertical', false).backgroundColor).toBe('transparent')
    expect(dividerOverlayStyle('horizontal', true).backgroundColor).toBe('rgba(86, 156, 214, 0.35)')
    expect(dividerOverlayStyle('vertical', true).backgroundColor).toBe('rgba(86, 156, 214, 0.35)')
  })
})
