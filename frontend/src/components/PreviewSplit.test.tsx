import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { PreviewSplit } from './PreviewSplit'
import type { PreviewView } from '../hooks/usePreview'
import { loadPreviewSplit, paneSplitKey, savePreviewSplit } from '../utils/preview'

// jsdom has no PointerEvent, so fireEvent.pointer* would drop pointerId,
// pointerType and clientX.
beforeAll(() => {
  if (window.PointerEvent) return
  class TestPointerEvent extends MouseEvent {
    pointerId: number
    pointerType: string
    constructor(type: string, init: PointerEventInit = {}) {
      super(type, init)
      this.pointerId = init.pointerId ?? 0
      this.pointerType = init.pointerType ?? ''
    }
  }
  window.PointerEvent = TestPointerEvent as unknown as typeof PointerEvent
})

afterEach(() => {
  vi.restoreAllMocks()
  window.localStorage.clear()
})

const ready: PreviewView = {
  kind: 'ready', source: 'file', url: '/preview/ab/mockups/index.html', path: '/workspace/user/project/mockups/index.html', host: '',
  generation: 0,
}

function renderSplit(view: PreviewView | null, handlers: Partial<Record<'onChoose' | 'onReload' | 'onClose', () => void>> = {}) {
  const props = {
    splitKey: paneSplitKey('pane-1'),
    view,
    terminal: <div data-testid="terminal">terminal</div>,
    onChoose: vi.fn(),
    onReload: vi.fn(),
    onClose: vi.fn(),
    ...handlers,
  }
  const utils = render(<PreviewSplit {...props} />)
  return { ...utils, props }
}

function terminalSide(): HTMLElement {
  return screen.getByTestId('terminal').parentElement as HTMLElement
}

function mockWidth(container: HTMLElement, width: number) {
  const split = container.querySelector('.pm-preview-split') as HTMLElement
  vi.spyOn(split, 'getBoundingClientRect').mockReturnValue({
    left: 100, top: 0, width, height: 400, right: 100 + width, bottom: 400, x: 100, y: 0, toJSON: () => ({}),
  })
}

describe('PreviewSplit', () => {
  it('gives the whole body to the terminal while nothing is previewed', () => {
    renderSplit(null)
    expect(screen.queryByRole('separator')).toBeNull()
    expect(screen.queryByRole('region', { name: 'Preview' })).toBeNull()
    expect(terminalSide().style.flex).toBe('1 1 100%')
  })

  it('keeps the same terminal element when the preview opens and closes', () => {
    const { rerender, props } = renderSplit(null)
    const terminal = screen.getByTestId('terminal')

    rerender(<PreviewSplit {...props} view={ready} />)
    expect(screen.getByTestId('terminal')).toBe(terminal)
    rerender(<PreviewSplit {...props} view={null} />)
    expect(screen.getByTestId('terminal')).toBe(terminal)
  })

  it('splits 50:50 and shows the file in an isolated frame', () => {
    renderSplit(ready)

    expect(terminalSide().style.flex).toBe('0 1 50%')
    const frame = screen.getByTitle('Preview of /workspace/user/project/mockups/index.html') as HTMLIFrameElement
    expect(frame.getAttribute('src')).toBe('/preview/ab/mockups/index.html')
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts')
    const region = screen.getByRole('region', { name: 'Preview' })
    expect(region).toHaveTextContent('local')
    expect(region).toHaveTextContent('/workspace/user/project/mockups/index.html')
  })

  it('shows a relayed localhost URL in its own origin, naming its host (issue #387)', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    renderSplit({
      kind: 'ready', source: 'url', url: 'http://127.0.0.1:49152/app', path: 'http://localhost:5173/app', host: 'devbox',
      generation: 0,
    })

    const frame = screen.getByTitle('Preview of http://localhost:5173/app') as HTMLIFrameElement
    expect(frame.getAttribute('src')).toBe('http://127.0.0.1:49152/app')
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts allow-same-origin allow-forms')
    const region = screen.getByRole('region', { name: 'Preview' })
    expect(region).toHaveTextContent('devbox')
    expect(region).toHaveTextContent('http://localhost:5173/app')
    fireEvent.click(screen.getByRole('button', { name: 'Open in new tab' }))
    expect(open).toHaveBeenCalledWith('http://127.0.0.1:49152/app', '_blank', 'noopener,noreferrer')
  })

  it('starts from the ratio stored for this pane', () => {
    savePreviewSplit(paneSplitKey('pane-1'), 35)
    renderSplit(ready)
    expect(terminalSide().style.flex).toBe('0 1 35%')
  })

  it('keeps both sides at their minimum width', () => {
    renderSplit(ready)
    expect(terminalSide().style.minWidth).toBe('160px')
    expect(screen.getByRole('region', { name: 'Preview' }).style.minWidth).toBe('160px')
  })

  it('moves with the arrow keys within 20:80 and 80:20, and saves the ratio', () => {
    renderSplit(ready)
    const divider = screen.getByRole('separator', { name: 'Resize terminal and preview' })
    expect(divider).toHaveAttribute('aria-orientation', 'vertical')
    expect(divider).toHaveAttribute('aria-valuenow', '50')
    expect(divider).toHaveAttribute('aria-valuemin', '20')
    expect(divider).toHaveAttribute('aria-valuemax', '80')
    expect(divider).toHaveAttribute('tabindex', '0')

    fireEvent.keyDown(divider, { key: 'ArrowLeft' })
    expect(divider).toHaveAttribute('aria-valuenow', '45')
    expect(loadPreviewSplit(paneSplitKey('pane-1'))).toBe(45)

    for (let i = 0; i < 10; i++) fireEvent.keyDown(divider, { key: 'ArrowLeft' })
    expect(divider).toHaveAttribute('aria-valuenow', '20')
    for (let i = 0; i < 20; i++) fireEvent.keyDown(divider, { key: 'ArrowRight' })
    expect(divider).toHaveAttribute('aria-valuenow', '80')
    expect(terminalSide().style.flex).toBe('0 1 80%')
  })

  it('leaves other keys alone', () => {
    renderSplit(ready)
    const divider = screen.getByRole('separator')
    const event = fireEvent.keyDown(divider, { key: 'a' })
    expect(event).toBe(true)
    expect(divider).toHaveAttribute('aria-valuenow', '50')
  })

  it('returns to 50:50 on double-click', () => {
    savePreviewSplit(paneSplitKey('pane-1'), 30)
    renderSplit(ready)
    const divider = screen.getByRole('separator')

    fireEvent.doubleClick(divider)
    expect(divider).toHaveAttribute('aria-valuenow', '50')
    expect(loadPreviewSplit(paneSplitKey('pane-1'))).toBe(50)
  })

  it('follows a drag by mouse, pen or touch, clamped, and saves when it ends', () => {
    const { container } = renderSplit(ready)
    mockWidth(container, 1000)
    const divider = screen.getByRole('separator')
    divider.setPointerCapture = vi.fn()
    divider.releasePointerCapture = vi.fn()

    fireEvent.pointerDown(divider, { pointerId: 3, pointerType: 'touch', clientX: 600, button: 0 })
    expect(divider.setPointerCapture).toHaveBeenCalledWith(3)
    fireEvent.pointerMove(divider, { pointerId: 3, clientX: 400 })
    expect(divider).toHaveAttribute('aria-valuenow', '30')
    expect(container.querySelector('.pm-preview-split')).toHaveAttribute('data-dragging', 'true')
    expect(loadPreviewSplit(paneSplitKey('pane-1'))).toBe(50)

    fireEvent.pointerMove(divider, { pointerId: 3, clientX: 1090 })
    expect(divider).toHaveAttribute('aria-valuenow', '80')
    fireEvent.pointerUp(divider, { pointerId: 3, clientX: 1090 })
    expect(loadPreviewSplit(paneSplitKey('pane-1'))).toBe(80)
    expect(container.querySelector('.pm-preview-split')).not.toHaveAttribute('data-dragging')

    fireEvent.pointerMove(divider, { pointerId: 3, clientX: 300 })
    expect(divider).toHaveAttribute('aria-valuenow', '80')
  })

  it('ignores a secondary mouse button', () => {
    const { container } = renderSplit(ready)
    mockWidth(container, 1000)
    const divider = screen.getByRole('separator')
    divider.setPointerCapture = vi.fn()

    fireEvent.pointerDown(divider, { pointerId: 1, pointerType: 'mouse', clientX: 600, button: 2 })
    fireEvent.pointerMove(divider, { pointerId: 1, clientX: 400 })
    expect(divider).toHaveAttribute('aria-valuenow', '50')
  })

  it('reloads, opens a new tab and closes', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const { props } = renderSplit(ready)

    fireEvent.click(screen.getByRole('button', { name: 'Reload preview' }))
    expect(props.onReload).toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Open in new tab' }))
    expect(open).toHaveBeenCalledWith('/preview/ab/mockups/index.html', '_blank', 'noopener,noreferrer')
    fireEvent.click(screen.getByRole('button', { name: 'Close preview' }))
    expect(props.onClose).toHaveBeenCalled()
  })

  it('reloads the frame when the generation changes', () => {
    const { rerender, props } = renderSplit(ready)
    const before = screen.getByTitle(/^Preview of/)
    rerender(<PreviewSplit {...props} view={{ ...ready, generation: 1 }} />)
    expect(screen.getByTitle(/^Preview of/)).not.toBe(before)
  })

  it('says it is opening the file, with nothing to reload or open yet', () => {
    renderSplit({ kind: 'loading', request: './mockups/index.html' })
    expect(screen.getByRole('region', { name: 'Preview' })).toHaveTextContent('Opening ./mockups/index.html')
    expect(screen.getByRole('button', { name: 'Reload preview' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Open in new tab' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Close preview' })).toBeEnabled()
  })

  it('shows why a path cannot be opened', () => {
    renderSplit({ kind: 'error', request: 'x.md', message: 'file not found: x.md (looked in /w)' })
    expect(screen.getByRole('alert')).toHaveTextContent('file not found: x.md (looked in /w)')
    expect(screen.getByRole('region', { name: 'Preview' })).toHaveTextContent('x.md')
  })

  it('lists the files a path may name, to choose from', () => {
    const { props } = renderSplit({
      kind: 'choose', request: 'docs/design.md', host: '',
      candidates: ['/workspace/user/project/docs/design.md', '/workspace/user/project-wt/docs/design.md'],
    })

    expect(screen.getByRole('region', { name: 'Preview' })).toHaveTextContent('docs/design.md is in more than one place')
    fireEvent.click(screen.getByRole('button', { name: '/workspace/user/project-wt/docs/design.md' }))
    expect(props.onChoose).toHaveBeenCalledWith('/workspace/user/project-wt/docs/design.md')
  })
})

describe('PreviewSplit keys in the page', () => {
  // A key typed in the sandboxed page never reaches the dashboard's window,
  // so panemux's shortcuts (the popup's Ctrl/Cmd+Shift+Esc and Enter among
  // them) do nothing while the page has focus. The preview says so.
  const hint = /Keys go to the page/

  it('says so while the page has focus, and not once focus is back', () => {
    const { rerender, props } = renderSplit(ready)
    expect(screen.queryByText(hint)).toBeNull()

    const frame = screen.getByTitle(`Preview of ${ready.path}`)
    act(() => {
      frame.focus()
      window.dispatchEvent(new Event('blur'))
    })
    expect(screen.getByRole('status')).toHaveTextContent(hint)

    act(() => {
      screen.getByRole('button', { name: 'Close preview' }).focus()
      window.dispatchEvent(new Event('focus'))
    })
    expect(screen.queryByText(hint)).toBeNull()

    act(() => {
      frame.focus()
      window.dispatchEvent(new Event('blur'))
    })
    expect(screen.getByText(hint)).toBeInTheDocument()
    // A reload replaces the frame, which takes its focus with it.
    rerender(<PreviewSplit {...props} view={{ ...ready, generation: 1 }} />)
    expect(screen.queryByText(hint)).toBeNull()
  })

  it('says nothing when the window loses focus to something else', () => {
    renderSplit(ready)
    act(() => {
      window.dispatchEvent(new Event('blur'))
    })
    expect(screen.queryByText(hint)).toBeNull()
  })
})

describe('PreviewSplit in a narrow split', () => {
  // The split's width as a ResizeObserver reports it. Without one (jsdom's
  // default) the split cannot know its width and keeps the fixed 20-80 range.
  let resized: (() => void) | null = null
  beforeEach(() => {
    resized = null
    vi.stubGlobal('ResizeObserver', class {
      constructor(callback: () => void) { resized = callback }
      observe() {}
      unobserve() {}
      disconnect() {}
    })
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function narrow(container: HTMLElement, width: number) {
    mockWidth(container, width)
    act(() => resized?.())
  }

  it('shows, announces and moves within the ratios that leave both sides 160 px', () => {
    savePreviewSplit(paneSplitKey('pane-1'), 80)
    const { container } = renderSplit(ready)
    narrow(container, 450)

    const divider = screen.getByRole('separator')
    expect(divider).toHaveAttribute('aria-valuenow', '63')
    expect(divider).toHaveAttribute('aria-valuemin', '36')
    expect(divider).toHaveAttribute('aria-valuemax', '63')
    expect(terminalSide().style.flex).toBe('0 1 63%')
    expect(loadPreviewSplit(paneSplitKey('pane-1'))).toBe(80)

    fireEvent.keyDown(divider, { key: 'ArrowLeft' })
    expect(divider).toHaveAttribute('aria-valuenow', '58')
    expect(loadPreviewSplit(paneSplitKey('pane-1'))).toBe(58)

    for (let i = 0; i < 3; i++) fireEvent.keyDown(divider, { key: 'ArrowRight' })
    expect(divider).toHaveAttribute('aria-valuenow', '63')
    for (let i = 0; i < 8; i++) fireEvent.keyDown(divider, { key: 'ArrowLeft' })
    expect(divider).toHaveAttribute('aria-valuenow', '36')
    expect(loadPreviewSplit(paneSplitKey('pane-1'))).toBe(36)
  })

  it('drags within the same ratios', () => {
    const { container } = renderSplit(ready)
    narrow(container, 450)
    const divider = screen.getByRole('separator')
    divider.setPointerCapture = vi.fn()

    fireEvent.pointerDown(divider, { pointerId: 1, pointerType: 'mouse', button: 0, clientX: 325 })
    fireEvent.pointerMove(divider, { pointerId: 1, pointerType: 'mouse', clientX: 540 })
    expect(divider).toHaveAttribute('aria-valuenow', '63')
    fireEvent.pointerUp(divider, { pointerId: 1, pointerType: 'mouse', clientX: 540 })
    expect(loadPreviewSplit(paneSplitKey('pane-1'))).toBe(63)
  })

  it('returns to 50:50 on a double-click, and follows the split as it widens', () => {
    savePreviewSplit(paneSplitKey('pane-1'), 80)
    const { container } = renderSplit(ready)
    narrow(container, 450)
    const divider = screen.getByRole('separator')
    expect(divider).toHaveAttribute('aria-valuenow', '63')

    narrow(container, 1000)
    expect(divider).toHaveAttribute('aria-valuenow', '80')
    expect(divider).toHaveAttribute('aria-valuemax', '80')

    fireEvent.doubleClick(divider)
    expect(divider).toHaveAttribute('aria-valuenow', '50')
  })
})
