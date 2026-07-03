import { fireEvent, render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

class ResizeObserverMock {
  observe() {}
  disconnect() {}
  unobserve() {}
}

vi.stubGlobal('ResizeObserver', ResizeObserverMock)

const { mockUseTerminal } = vi.hoisted(() => ({
  mockUseTerminal: vi.fn(),
}))
const { mockUseGitInfo } = vi.hoisted(() => ({
  mockUseGitInfo: vi.fn(),
}))

vi.mock('../hooks/useTerminal', () => ({
  useTerminal: mockUseTerminal,
}))
vi.mock('../hooks/useGitInfo', () => ({
  useGitInfo: mockUseGitInfo,
}))
import { dropZoneStyle, PANE_DROP_ZONE_RATIO, resolvePaneDropEdge, TerminalPane } from './TerminalPane'
import { LayoutActionsContext, type LayoutActionsContextValue } from './SplitContainer'

describe('TerminalPane drop zones', () => {
  beforeEach(() => {
    mockUseTerminal.mockReturnValue({
      handleResize: vi.fn(),
      connected: true,
      dims: null,
      sessionState: 'running',
      reconnectFailed: false,
      restartSession: vi.fn(),
      scrollLines: vi.fn(),
    })
    mockUseGitInfo.mockReturnValue({
      gitInfo: { is_git: false },
      refreshIfStale: vi.fn(),
      refreshNow: vi.fn(),
    })
  })

  it('uses a top or bottom half-pane preview for horizontal edge drop zones', () => {
    expect(dropZoneStyle('top', false)).toMatchObject({ height: `${PANE_DROP_ZONE_RATIO * 100}%` })
    expect(dropZoneStyle('bottom', true)).toMatchObject({ height: `${PANE_DROP_ZONE_RATIO * 100}%` })
  })

  it('uses a left or right half-pane preview for vertical edge drop zones', () => {
    expect(dropZoneStyle('left', false)).toMatchObject({ width: `${PANE_DROP_ZONE_RATIO * 100}%` })
    expect(dropZoneStyle('right', true)).toMatchObject({ width: `${PANE_DROP_ZONE_RATIO * 100}%` })
  })

  it('resolves the nearest pane edge from the pointer position', () => {
    const rect = { left: 100, top: 200, width: 400, height: 200 }

    expect(resolvePaneDropEdge(rect, 120, 240)).toBe('left')
    expect(resolvePaneDropEdge(rect, 460, 240)).toBe('right')
    expect(resolvePaneDropEdge(rect, 280, 210)).toBe('top')
    expect(resolvePaneDropEdge(rect, 280, 390)).toBe('bottom')
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
    const { container } = render(
      <LayoutActionsContext.Provider value={ctx}>
        <TerminalPane pane={{ id: 'pane-target', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )

    const dropZone = container.querySelector('[data-pane-id="pane-target"] > div:nth-child(2)')
    expect(dropZone).not.toBeNull()
    vi.spyOn(dropZone as HTMLDivElement, 'getBoundingClientRect').mockReturnValue({
      left: 0,
      top: 0,
      width: 200,
      height: 100,
      right: 200,
      bottom: 100,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    })

    fireEvent.mouseMove(dropZone!, { buttons: 1, clientX: 5, clientY: 50 })
    fireEvent.mouseUp(dropZone!, { clientX: 5, clientY: 50 })

    expect(ctx.onMovePaneBeside).toHaveBeenCalledWith('pane-source', 'pane-target', 'left')
    expect(ctx.setDragSourcePaneId).toHaveBeenCalledWith(null)
  })

  it('shows restart button only when the session exited', () => {
    mockUseTerminal.mockReturnValue({
      handleResize: vi.fn(),
      connected: false,
      dims: null,
      sessionState: 'exited',
      reconnectFailed: false,
      restartSession: vi.fn(),
    })

    const { getByRole } = render(
      <LayoutActionsContext.Provider value={makeCtx()}>
        <TerminalPane pane={{ id: 'pane-1', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )

    expect(getByRole('button', { name: 'Restart Session' })).toBeInTheDocument()
  })

  it('shows reconnect button after disconnected auto-recovery fails', () => {
    mockUseTerminal.mockReturnValue({
      handleResize: vi.fn(),
      connected: false,
      dims: null,
      sessionState: 'disconnected',
      reconnectFailed: true,
      restartSession: vi.fn(),
    })

    const { getByRole, queryByRole } = render(
      <LayoutActionsContext.Provider value={makeCtx()}>
        <TerminalPane pane={{ id: 'pane-1', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )

    expect(getByRole('button', { name: 'Reconnect Session' })).toBeInTheDocument()
    expect(queryByRole('button', { name: 'Restart Session' })).toBeNull()
  })

  it('disables git polling for panes hidden behind maximize', () => {
    render(
      <LayoutActionsContext.Provider value={makeCtx({ maximizedPaneId: 'other-pane' })}>
        <TerminalPane pane={{ id: 'pane-1', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )

    expect(mockUseGitInfo).toHaveBeenCalledWith('pane-1', false)
  })

  it('keeps git polling enabled for the maximized pane', () => {
    render(
      <LayoutActionsContext.Provider value={makeCtx({ maximizedPaneId: 'pane-1' })}>
        <TerminalPane pane={{ id: 'pane-1', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )

    expect(mockUseGitInfo).toHaveBeenCalledWith('pane-1', true)
  })
})

describe('TerminalPane Shift+wheel handler', () => {
  const scrollLinesMock = vi.fn()

  beforeEach(() => {
    scrollLinesMock.mockReset()
    mockUseTerminal.mockReturnValue({
      handleResize: vi.fn(),
      connected: true,
      dims: null,
      sessionState: 'running',
      reconnectFailed: false,
      restartSession: vi.fn(),
      scrollLines: scrollLinesMock,
    })
    mockUseGitInfo.mockReturnValue({
      gitInfo: { is_git: false },
      refreshIfStale: vi.fn(),
      refreshNow: vi.fn(),
    })
  })

  function renderPane() {
    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx()}>
        <TerminalPane pane={{ id: 'pane-1', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )
    return container.querySelector('[data-pane-id="pane-1"] > div:nth-child(2)') as HTMLElement
  }

  it('DOM_DELTA_PIXEL: scrolls lines from pixel delta', () => {
    const el = renderPane()
    fireEvent.wheel(el, { shiftKey: true, deltaY: 120, deltaMode: 0 })
    expect(scrollLinesMock).toHaveBeenCalledWith(3)
  })

  it('DOM_DELTA_PIXEL: falls back to 1 when pixel delta rounds to zero', () => {
    const el = renderPane()
    fireEvent.wheel(el, { shiftKey: true, deltaY: 10, deltaMode: 0 })
    expect(scrollLinesMock).toHaveBeenCalledWith(1)
  })

  it('DOM_DELTA_LINE: uses deltaY directly as line count', () => {
    const el = renderPane()
    fireEvent.wheel(el, { shiftKey: true, deltaY: 3, deltaMode: 1 })
    expect(scrollLinesMock).toHaveBeenCalledWith(3)
  })

  it('DOM_DELTA_LINE: scrolls in reverse for negative delta', () => {
    const el = renderPane()
    fireEvent.wheel(el, { shiftKey: true, deltaY: -3, deltaMode: 1 })
    expect(scrollLinesMock).toHaveBeenCalledWith(-3)
  })

  it('DOM_DELTA_PAGE: converts pages to lines by multiplying by 20', () => {
    const el = renderPane()
    fireEvent.wheel(el, { shiftKey: true, deltaY: 1, deltaMode: 2 })
    expect(scrollLinesMock).toHaveBeenCalledWith(20)
  })

  it('does not scroll when Shift is not held', () => {
    const el = renderPane()
    fireEvent.wheel(el, { shiftKey: false, deltaY: 120, deltaMode: 0 })
    expect(scrollLinesMock).not.toHaveBeenCalled()
  })
})

function makeCtx(overrides: Partial<LayoutActionsContextValue> = {}): LayoutActionsContextValue {
  return {
    onSplit: vi.fn(),
    onCreatePaneBeside: vi.fn(),
    onClose: vi.fn(),
    onMaximize: vi.fn(),
    onSettings: vi.fn(),
    onSwapPanes: vi.fn(),
    onMovePaneToWorkspaceEdge: vi.fn(),
    onMovePaneBeside: vi.fn(),
    maximizedPaneId: null,
    dragSourcePaneId: null,
    setDragSourcePaneId: vi.fn(),
    isResizing: false,
    setIsResizing: vi.fn(),
    displayConfig: { show_header: true, show_status_bar: false },
    onPaneAttention: vi.fn(),
    clearPaneAttention: vi.fn(),
    hasPaneAttention: vi.fn(() => false),
    activePaneId: null,
    setActivePaneId: vi.fn(),
    ...overrides,
  }
}
