import { fireEvent, render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

class ResizeObserverMock {
  observe() {}
  disconnect() {}
  unobserve() {}
}

vi.stubGlobal('ResizeObserver', ResizeObserverMock)

vi.mock('../hooks/useTerminal', () => ({
  useTerminal: () => ({
    handleResize: vi.fn(),
    connected: true,
    dims: null,
    sessionExited: false,
    restartSession: vi.fn(),
  }),
}))
vi.mock('../hooks/useGitInfo', () => ({
  useGitInfo: () => ({ is_git: false }),
}))
import { dropZoneStyle, PANE_DROP_ZONE_THICKNESS, TerminalPane } from './TerminalPane'
import { LayoutActionsContext, type LayoutActionsContextValue } from './SplitContainer'

describe('TerminalPane drop zones', () => {
  it('uses the expanded thickness for horizontal edge drop zones', () => {
    expect(dropZoneStyle('top', false)).toMatchObject({ height: PANE_DROP_ZONE_THICKNESS })
    expect(dropZoneStyle('bottom', true)).toMatchObject({ height: PANE_DROP_ZONE_THICKNESS })
  })

  it('uses the expanded thickness for vertical edge drop zones', () => {
    expect(dropZoneStyle('left', false)).toMatchObject({ width: PANE_DROP_ZONE_THICKNESS })
    expect(dropZoneStyle('right', true)).toMatchObject({ width: PANE_DROP_ZONE_THICKNESS })
  })

  it('starts pane drag with transferable pane data', () => {
    const ctx = makeCtx()
    const dataTransfer = {
      effectAllowed: 'none',
      setData: vi.fn(),
    }

    const { getByTitle } = render(
      <LayoutActionsContext.Provider value={ctx}>
        <TerminalPane pane={{ id: 'pane-1', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )

    fireEvent.dragStart(getByTitle('Drag to move pane'), { dataTransfer })

    expect(dataTransfer.effectAllowed).toBe('move')
    expect(dataTransfer.setData).toHaveBeenCalledWith('text/plain', 'pane-1')
    expect(ctx.setDragSourcePaneId).toHaveBeenCalledWith('pane-1')
  })

  it('moves a dragged pane beside the target pane on edge drop', () => {
    const ctx = makeCtx({ dragSourcePaneId: 'pane-source' })
    const dataTransfer = { dropEffect: 'none' }

    const { container } = render(
      <LayoutActionsContext.Provider value={ctx}>
        <TerminalPane pane={{ id: 'pane-target', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )

    const dropZone = container.querySelector('[data-pane-drop-edge="left"]')
    expect(dropZone).not.toBeNull()

    fireEvent.dragOver(dropZone!, { dataTransfer })
    fireEvent.drop(dropZone!, { dataTransfer })

    expect(ctx.onMovePaneBeside).toHaveBeenCalledWith('pane-source', 'pane-target', 'left')
    expect(ctx.setDragSourcePaneId).toHaveBeenCalledWith(null)
  })
})

function makeCtx(overrides: Partial<LayoutActionsContextValue> = {}): LayoutActionsContextValue {
  return {
    onSplit: vi.fn(),
    onClose: vi.fn(),
    onMaximize: vi.fn(),
    onSettings: vi.fn(),
    onSwapPanes: vi.fn(),
    onMovePaneToWorkspaceEdge: vi.fn(),
    onMovePaneBeside: vi.fn(),
    maximizedPaneId: null,
    dragSourcePaneId: null,
    setDragSourcePaneId: vi.fn(),
    displayConfig: { show_header: true, show_status_bar: false },
    onPaneAttention: vi.fn(),
    clearPaneAttention: vi.fn(),
    hasPaneAttention: vi.fn(() => false),
    ...overrides,
  }
}
