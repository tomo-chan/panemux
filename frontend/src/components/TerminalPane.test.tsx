import { act, fireEvent, render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

// Records every observer the component creates so a test can fire the callback
// the real browser would fire on a layout change, and can assert the cleanup.
const resizeObservers: { callback: () => void, observed: unknown[], disconnected: boolean }[] = []

class ResizeObserverMock {
  private readonly record: { callback: () => void, observed: unknown[], disconnected: boolean }

  constructor(callback: () => void) {
    this.record = { callback, observed: [], disconnected: false }
    resizeObservers.push(this.record)
  }

  observe(target: unknown) { this.record.observed.push(target) }
  disconnect() { this.record.disconnected = true }
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
    displayConfig: { show_header: true, show_status_bar: false },
    onPaneAttention: vi.fn(),
    clearPaneAttention: vi.fn(),
    hasPaneAttention: vi.fn(() => false),
    activePaneId: null,
    setActivePaneId: vi.fn(),
    ...overrides,
  }
}

describe('TerminalPane URL opening', () => {
  beforeEach(() => {
    mockUseTerminal.mockReturnValue({
      handleResize: vi.fn(),
      connected: true,
      dims: null,
      sessionState: 'running',
      reconnectFailed: false,
      restartSession: vi.fn(),
    })
    mockUseGitInfo.mockReturnValue({
      gitInfo: { is_git: false },
      refreshIfStale: vi.fn(),
      refreshNow: vi.fn(),
    })
  })

  function terminalOptions() {
    return mockUseTerminal.mock.calls[mockUseTerminal.mock.calls.length - 1][0] as {
      onLinkActivate: (url: string) => void
      onBrowserOpenRequest: (url: string) => void
    }
  }

  it('gives the terminal both URL-opening handlers', () => {
    render(<TerminalPane pane={{ id: 'p1', type: 'ssh' }} />)

    const options = terminalOptions()
    expect(typeof options.onLinkActivate).toBe('function')
    expect(typeof options.onBrowserOpenRequest).toBe('function')
  })

  it('asks for approval when the pane requests a browser open, then opens it', async () => {
    const openSpy = vi.fn()
    window.open = openSpy as unknown as typeof window.open
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ url: 'https://example.com/device', forwarded: true, port: 8085 }),
    } as Response)
    const { findByText, getByRole } = render(<TerminalPane pane={{ id: 'p1', type: 'ssh' }} />)

    act(() => terminalOptions().onBrowserOpenRequest('https://example.com/device'))

    expect(await findByText('https://example.com/device')).toBeInTheDocument()
    expect(openSpy).not.toHaveBeenCalled()

    fireEvent.click(getByRole('button', { name: 'Open' }))

    expect(openSpy).toHaveBeenCalledWith('https://example.com/device', '_blank', 'noopener,noreferrer')
  })
})

describe('TerminalPane lifecycle', () => {
  let handleResize: ReturnType<typeof vi.fn>
  let refreshIfStale: ReturnType<typeof vi.fn>
  let refreshNow: ReturnType<typeof vi.fn>

  beforeEach(() => {
    resizeObservers.length = 0
    handleResize = vi.fn()
    refreshIfStale = vi.fn()
    refreshNow = vi.fn()
    mockUseTerminal.mockReturnValue({
      handleResize,
      connected: true,
      dims: null,
      sessionState: 'running',
      reconnectFailed: false,
      restartSession: vi.fn(),
    })
    mockUseGitInfo.mockReturnValue({ gitInfo: { is_git: false }, refreshIfStale, refreshNow })
  })

  function renderPane(overrides: Partial<LayoutActionsContextValue> = {}, paneId = 'pane-1') {
    const ctx = makeCtx(overrides)
    const rendered = render(
      <LayoutActionsContext.Provider value={ctx}>
        <TerminalPane pane={{ id: paneId, type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )
    return { ctx, ...rendered }
  }

  it('re-fits the terminal when the pane element changes size', () => {
    renderPane()

    expect(resizeObservers).toHaveLength(1)
    expect(resizeObservers[0].observed).toHaveLength(1)
    expect(handleResize).not.toHaveBeenCalled()

    act(() => resizeObservers[0].callback())

    expect(handleResize).toHaveBeenCalledTimes(1)
  })

  it('stops observing when the pane goes away', () => {
    const { unmount } = renderPane()

    expect(resizeObservers[0].disconnected).toBe(false)
    unmount()

    expect(resizeObservers[0].disconnected).toBe(true)
  })

  it('refreshes git metadata and asks the server to open the pane in VSCode', () => {
    const fetchSpy = vi.fn().mockResolvedValue({ ok: true })
    window.fetch = fetchSpy as unknown as typeof window.fetch
    const { getByTitle } = renderPane()

    fireEvent.click(getByTitle('Open in VSCode'))

    expect(refreshNow).toHaveBeenCalled()
    expect(fetchSpy).toHaveBeenCalledWith('/api/sessions/pane-1/open-vscode', { method: 'POST' })
  })

  it('logs rather than throws when opening VSCode fails', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    window.fetch = vi.fn().mockRejectedValue(new Error('no editor')) as unknown as typeof window.fetch
    const { getByTitle } = renderPane()

    fireEvent.click(getByTitle('Open in VSCode'))
    await act(async () => { await Promise.resolve() })

    expect(consoleError).toHaveBeenCalledWith('open-vscode failed:', expect.any(Error))
    consoleError.mockRestore()
  })

  it('claims the pane as active when it is clicked', () => {
    const { ctx, container } = renderPane()

    fireEvent.mouseDown(container.querySelector('[data-pane-id="pane-1"]')!)

    expect(ctx.clearPaneAttention).toHaveBeenCalledWith('pane-1')
    expect(ctx.setActivePaneId).toHaveBeenCalledWith('pane-1')
    expect(refreshIfStale).toHaveBeenCalled()
  })

  it('claims the pane as active when focus reaches it without a click', () => {
    // Keyboard navigation and xterm's own focus handling both land here rather
    // than on mousedown.
    const { ctx, container } = renderPane()

    fireEvent.focus(container.querySelector('[data-pane-id="pane-1"]')!)

    expect(ctx.clearPaneAttention).toHaveBeenCalledWith('pane-1')
    expect(ctx.setActivePaneId).toHaveBeenCalledWith('pane-1')
    expect(refreshIfStale).toHaveBeenCalled()
  })
})

describe('TerminalPane header actions', () => {
  beforeEach(() => {
    mockUseTerminal.mockReturnValue({
      handleResize: vi.fn(),
      connected: true,
      dims: null,
      sessionState: 'running',
      reconnectFailed: false,
      restartSession: vi.fn(),
    })
    mockUseGitInfo.mockReturnValue({
      gitInfo: { is_git: false },
      refreshIfStale: vi.fn(),
      refreshNow: vi.fn(),
    })
  })

  function renderPane(overrides: Partial<LayoutActionsContextValue> = {}) {
    const ctx = makeCtx(overrides)
    const rendered = render(
      <LayoutActionsContext.Provider value={ctx}>
        <TerminalPane pane={{ id: 'pane-1', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )
    return { ctx, ...rendered }
  }

  it('splits this pane on the axis the header button names', () => {
    const { ctx, getByTitle } = renderPane()

    fireEvent.click(getByTitle('Split horizontal'))
    fireEvent.click(getByTitle('Split vertical'))

    expect(ctx.onSplit).toHaveBeenNthCalledWith(1, 'pane-1', 'horizontal')
    expect(ctx.onSplit).toHaveBeenNthCalledWith(2, 'pane-1', 'vertical')
  })

  it('creates a new pane on the edge the header button names', () => {
    const { ctx, getByTitle } = renderPane()

    fireEvent.click(getByTitle('Add new pane to the right'))
    fireEvent.click(getByTitle('Add new pane below'))

    expect(ctx.onCreatePaneBeside).toHaveBeenNthCalledWith(1, 'pane-1', 'right')
    expect(ctx.onCreatePaneBeside).toHaveBeenNthCalledWith(2, 'pane-1', 'bottom')
  })

  it('closes and opens the settings for this pane', () => {
    const { ctx, getByTitle } = renderPane()

    fireEvent.click(getByTitle('Pane settings'))
    fireEvent.click(getByTitle('Close pane'))

    expect(ctx.onSettings).toHaveBeenCalledWith('pane-1')
    expect(ctx.onClose).toHaveBeenCalledWith('pane-1')
  })

  it('maximizes this pane when nothing is maximized', () => {
    const { ctx, getByTitle } = renderPane()

    fireEvent.click(getByTitle('Maximize'))

    expect(ctx.onMaximize).toHaveBeenCalledWith('pane-1')
  })

  it('restores instead when this pane is the maximized one', () => {
    const { ctx, getByTitle } = renderPane({ maximizedPaneId: 'pane-1' })

    fireEvent.click(getByTitle('Restore'))

    expect(ctx.onMaximize).toHaveBeenCalledWith(null)
  })

  it('ends the drag when the move handle is dropped', () => {
    const { ctx, getByTitle } = renderPane()

    fireEvent.dragEnd(getByTitle('Drag to move pane'))

    expect(ctx.setDragSourcePaneId).toHaveBeenCalledWith(null)
  })

  it('starts a drag when the move handle is pressed with the primary button', () => {
    const { ctx, getByTitle } = renderPane()

    fireEvent.mouseDown(getByTitle('Drag to move pane'), { button: 0 })

    expect(ctx.setDragSourcePaneId).toHaveBeenCalledWith('pane-1')
  })

  it('ignores a press of any other mouse button on the move handle', () => {
    // A right-click opens the context menu; it must not begin a pane move that
    // only a mouseup can end.
    const { ctx, getByTitle } = renderPane()

    fireEvent.mouseDown(getByTitle('Drag to move pane'), { button: 2 })

    expect(ctx.setDragSourcePaneId).not.toHaveBeenCalled()
  })
})

describe('TerminalPane while it is the drag source', () => {
  beforeEach(() => {
    mockUseTerminal.mockReturnValue({
      handleResize: vi.fn(),
      connected: true,
      dims: null,
      sessionState: 'running',
      reconnectFailed: false,
      restartSession: vi.fn(),
    })
    mockUseGitInfo.mockReturnValue({
      gitInfo: { is_git: false },
      refreshIfStale: vi.fn(),
      refreshNow: vi.fn(),
    })
    document.body.style.cursor = ''
  })

  function renderDragSource() {
    const ctx = makeCtx({ dragSourcePaneId: 'pane-1' })
    const rendered = render(
      <LayoutActionsContext.Provider value={ctx}>
        <TerminalPane pane={{ id: 'pane-1', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )
    return { ctx, ...rendered }
  }

  it('takes over the page cursor for the whole drag', () => {
    document.body.style.cursor = 'text'

    const { unmount } = renderDragSource()
    expect(document.body.style.cursor).toBe('grabbing')

    unmount()
    expect(document.body.style.cursor).toBe('text')
  })

  it('ends the drag on a mouse-up anywhere on the page', () => {
    // The pointer is very often outside the pane by the time the button comes
    // back up, so the listener is on the window rather than the pane.
    const { ctx } = renderDragSource()

    fireEvent.mouseUp(window)

    expect(ctx.setDragSourcePaneId).toHaveBeenCalledWith(null)
  })

  it('stops listening for that mouse-up once the drag is over', () => {
    const { ctx, unmount } = renderDragSource()
    unmount()

    fireEvent.mouseUp(window)

    expect(ctx.setDragSourcePaneId).not.toHaveBeenCalled()
  })

  it('does not touch the page cursor for a pane that is not the drag source', () => {
    document.body.style.cursor = 'text'

    render(
      <LayoutActionsContext.Provider value={makeCtx({ dragSourcePaneId: 'another-pane' })}>
        <TerminalPane pane={{ id: 'pane-1', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )

    expect(document.body.style.cursor).toBe('text')
  })

  it('fades the pane being dragged', () => {
    const { container } = renderDragSource()

    const pane = container.querySelector('[data-pane-id="pane-1"]') as HTMLElement
    expect(Number(pane.style.opacity)).toBeLessThan(1)
    expect(pane.style.transform).toBe('scale(0.985)')
  })
})

/**
 * jsdom's DragEvent does not derive from MouseEvent, so
 * `fireEvent.dragOver(el, { clientX })` silently drops the coordinates: the
 * handler reads `undefined`, every distance becomes NaN, and the edge
 * calculation falls through to its last branch. A test written that way passes
 * against any pointer position, which is worth nothing. Dispatching a real
 * MouseEvent under the drag event's name is what actually carries the pointer
 * position into React's handler.
 */
function fireDragEvent(
  type: 'dragover' | 'drop',
  target: HTMLElement,
  options: { clientX: number, clientY: number, dataTransfer?: { dropEffect: string } },
) {
  const event = new MouseEvent(type, {
    bubbles: true,
    cancelable: true,
    clientX: options.clientX,
    clientY: options.clientY,
  })
  Object.defineProperty(event, 'dataTransfer', { value: options.dataTransfer ?? { dropEffect: 'none' } })
  fireEvent(target, event)
}

describe('TerminalPane drop target behavior', () => {
  const rect = {
    left: 0, top: 0, width: 200, height: 100, right: 200, bottom: 100, x: 0, y: 0,
    toJSON: () => ({}),
  }

  beforeEach(() => {
    mockUseTerminal.mockReturnValue({
      handleResize: vi.fn(),
      connected: true,
      dims: null,
      sessionState: 'running',
      reconnectFailed: false,
      restartSession: vi.fn(),
    })
    mockUseGitInfo.mockReturnValue({
      gitInfo: { is_git: false },
      refreshIfStale: vi.fn(),
      refreshNow: vi.fn(),
    })
  })

  function renderTarget(dragSourcePaneId: string | null) {
    const ctx = makeCtx({ dragSourcePaneId })
    const rendered = render(
      <LayoutActionsContext.Provider value={ctx}>
        <TerminalPane pane={{ id: 'pane-target', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )
    const area = rendered.container.querySelector('[data-pane-id="pane-target"] > div:nth-child(2)') as HTMLElement
    vi.spyOn(area, 'getBoundingClientRect').mockReturnValue(rect as DOMRect)
    const preview = () => rendered.container.querySelector('[data-pane-drop-preview]') as HTMLElement | null
    return { ctx, area, preview, ...rendered }
  }

  it('previews the edge the pointer is nearest while dragging over it', () => {
    const { area, preview } = renderTarget('pane-source')

    expect(preview()).toBeNull()

    fireDragEvent('dragover', area, { clientX: 5, clientY: 50 })
    expect(preview()?.dataset.paneDropPreview).toBe('left')

    // Follows the pointer rather than being decided once.
    fireDragEvent('dragover', area, { clientX: 195, clientY: 50 })
    expect(preview()?.dataset.paneDropPreview).toBe('right')

    fireDragEvent('dragover', area, { clientX: 100, clientY: 2 })
    expect(preview()?.dataset.paneDropPreview).toBe('top')
  })

  it('reports a move rather than a copy to the drag layer', () => {
    const { area } = renderTarget('pane-source')
    const dataTransfer = { dropEffect: 'none' }

    fireDragEvent('dragover', area, { clientX: 5, clientY: 50, dataTransfer })

    expect(dataTransfer.dropEffect).toBe('move')
  })

  it('clears the preview when the pointer leaves', () => {
    const { area, preview } = renderTarget('pane-source')

    fireDragEvent('dragover', area, { clientX: 5, clientY: 50 })
    expect(preview()).not.toBeNull()

    fireEvent.dragLeave(area)

    expect(preview()).toBeNull()
  })

  it('clears the preview when the mouse leaves during a mouse drag', () => {
    const { area, preview } = renderTarget('pane-source')

    fireEvent.mouseMove(area, { buttons: 1, clientX: 5, clientY: 50 })
    expect(preview()).not.toBeNull()

    fireEvent.mouseLeave(area)
    expect(preview()).toBeNull()
  })

  it('moves the pane on drop, taking the edge from the pointer position', () => {
    const { ctx, area } = renderTarget('pane-source')

    fireDragEvent('drop', area, { clientX: 195, clientY: 50 })

    expect(ctx.onMovePaneBeside).toHaveBeenCalledWith('pane-source', 'pane-target', 'right')
    expect(ctx.setDragSourcePaneId).toHaveBeenCalledWith(null)
  })

  it('takes the drop edge from where the pointer was released, not from the last preview', () => {
    // The preview is a hint; the drop is decided by the release position, which
    // can differ when the pointer moves between the last dragover and the drop.
    const { ctx, area } = renderTarget('pane-source')

    fireDragEvent('dragover', area, { clientX: 5, clientY: 50 })
    fireDragEvent('drop', area, { clientX: 100, clientY: 98 })

    expect(ctx.onMovePaneBeside).toHaveBeenCalledWith('pane-source', 'pane-target', 'bottom')
  })

  it('ignores a drag-over while nothing is being dragged', () => {
    const { area, preview } = renderTarget(null)
    const dataTransfer = { dropEffect: 'none' }

    fireDragEvent('dragover', area, { clientX: 5, clientY: 50, dataTransfer })

    expect(dataTransfer.dropEffect).toBe('none')
    expect(preview()).toBeNull()
  })

  it('ignores a pane being dragged onto itself', () => {
    // Dropping a pane on its own body is a no-op, not a move next to itself.
    const { ctx, area, preview } = renderTarget('pane-target')

    fireDragEvent('dragover', area, { clientX: 5, clientY: 50 })
    fireDragEvent('drop', area, { clientX: 5, clientY: 50 })
    fireEvent.mouseUp(area, { clientX: 5, clientY: 50 })

    expect(preview()).toBeNull()
    expect(ctx.onMovePaneBeside).not.toHaveBeenCalled()
  })

  it('ignores a mouse move with no button held down', () => {
    // Without the button check, simply moving the pointer across a pane after a
    // drag had ended elsewhere would keep drawing drop previews.
    const { area, preview } = renderTarget('pane-source')

    fireEvent.mouseMove(area, { buttons: 0, clientX: 5, clientY: 50 })

    expect(preview()).toBeNull()
  })

  it('ignores a mouse-up while nothing is being dragged', () => {
    const { ctx, area } = renderTarget(null)

    fireEvent.mouseUp(area, { clientX: 5, clientY: 50 })

    expect(ctx.onMovePaneBeside).not.toHaveBeenCalled()
  })
})

describe('TerminalPane attention and focus styling', () => {
  beforeEach(() => {
    mockUseTerminal.mockReturnValue({
      handleResize: vi.fn(),
      connected: true,
      dims: null,
      sessionState: 'running',
      reconnectFailed: false,
      restartSession: vi.fn(),
    })
    mockUseGitInfo.mockReturnValue({
      gitInfo: { is_git: false },
      refreshIfStale: vi.fn(),
      refreshNow: vi.fn(),
    })
  })

  function paneElement(overrides: Partial<LayoutActionsContextValue>) {
    const { container } = render(
      <LayoutActionsContext.Provider value={makeCtx(overrides)}>
        <TerminalPane pane={{ id: 'pane-1', type: 'local', title: 'Pane 1' }} />
      </LayoutActionsContext.Provider>,
    )
    return container.querySelector('[data-pane-id="pane-1"]') as HTMLElement
  }

  it('marks a pane that needs attention', () => {
    const pane = paneElement({ hasPaneAttention: vi.fn(() => true) })

    expect(pane.dataset.attention).toBe('true')
    expect(pane.className).toBe('panemux-pane-attention')
    expect(pane.style.outline).toContain('rgba(244, 191, 79, 0.95)')
  })

  it('marks the active pane differently', () => {
    const pane = paneElement({ activePaneId: 'pane-1' })

    expect(pane.dataset.activePane).toBe('true')
    expect(pane.dataset.attention).toBeUndefined()
    expect(pane.style.outline).toContain('rgba(137, 196, 244, 0.92)')
  })

  it('prefers the attention outline when the active pane is also the one asking', () => {
    const pane = paneElement({ activePaneId: 'pane-1', hasPaneAttention: vi.fn(() => true) })

    expect(pane.style.outline).toContain('rgba(244, 191, 79, 0.95)')
  })

  it('leaves an ordinary pane unmarked', () => {
    const pane = paneElement({})

    expect(pane.dataset.attention).toBeUndefined()
    expect(pane.dataset.activePane).toBeUndefined()
    expect(pane.style.outline).toBe('none')
  })
})
