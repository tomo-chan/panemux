import { renderHook, act } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { __resetTerminalEntriesForTests, useTerminal } from './useTerminal'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

// ── xterm.js mocks ───────────────────────────────────────────────────────────
const { mockWrite, mockTerm, mockFitAddon, mockTerminalCtor } = vi.hoisted(() => {
  const mockWrite = vi.fn()
  const mockTerm = {
    attachCustomKeyEventHandler: vi.fn(),
    element: undefined as HTMLElement | undefined,
    hasSelection: vi.fn(() => false),
    getSelection: vi.fn(() => ''),
    loadAddon: vi.fn(),
    open: vi.fn(),
    onData: vi.fn(),
    onBinary: vi.fn(),
    dispose: vi.fn(),
    cols: 80,
    rows: 24,
    refresh: vi.fn(),
    write: mockWrite,
  }
  const mockFitAddon = { fit: vi.fn() }
  const mockTerminalCtor = vi.fn(function () { return mockTerm })
  return { mockWrite, mockTerm, mockFitAddon, mockTerminalCtor }
})

vi.mock('@xterm/xterm', () => ({ Terminal: mockTerminalCtor }))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: vi.fn(function () { return mockFitAddon }) }))
vi.mock('@xterm/addon-web-links', () => ({ WebLinksAddon: vi.fn(function () { return {} }) }))

// ── WebSocket mock ────────────────────────────────────────────────────────────
class MockWebSocket {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3
  static instances: MockWebSocket[] = []

  onopen: (() => void) | null = null
  onmessage: ((e: { data: unknown }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  readyState = MockWebSocket.OPEN
  binaryType = 'blob'
  url: string
  sent: unknown[] = []

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }
  send(data: unknown) { this.sent.push(data) }
  close() { this.readyState = MockWebSocket.CLOSED; this.onclose?.() }
  simulateOpen() { this.readyState = MockWebSocket.OPEN; this.onopen?.() }
  simulateMessage(data: string | ArrayBuffer) { this.onmessage?.({ data }) }
}

// ── helpers ───────────────────────────────────────────────────────────────────
function makeContainer() {
  return document.createElement('div')
}

// ── tests ─────────────────────────────────────────────────────────────────────
describe('useTerminal', () => {
  let originalWebSocket: typeof WebSocket
  let originalClipboard: Clipboard | undefined
  let originalExecCommand: typeof document.execCommand | undefined
  let originalRequestAnimationFrame: typeof window.requestAnimationFrame

  beforeEach(() => {
    originalWebSocket = window.WebSocket
    originalClipboard = navigator.clipboard
    originalExecCommand = document.execCommand
    originalRequestAnimationFrame = window.requestAnimationFrame
    MockWebSocket.instances = []
    window.WebSocket = MockWebSocket as unknown as typeof WebSocket
    window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      cb(0)
      return 0
    }) as typeof window.requestAnimationFrame
    mockTerm.element = undefined
    mockTerm.open.mockImplementation((container: HTMLElement) => {
      const el = document.createElement('div')
      mockTerm.element = el
      container.appendChild(el)
    })
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
    })
    document.execCommand = vi.fn(() => true)
    vi.clearAllMocks()
  })

  afterEach(() => {
    __resetTerminalEntriesForTests()
    window.WebSocket = originalWebSocket
    window.requestAnimationFrame = originalRequestAnimationFrame
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: originalClipboard,
    })
    document.execCommand = originalExecCommand as typeof document.execCommand
    vi.restoreAllMocks()
  })

  it('does not initialise terminal when container is null', () => {
    renderHook(() => useTerminal({ sessionId: 's1', container: null }))
    expect(mockTerm.open).not.toHaveBeenCalled()
  })

  it('initialises terminal when container is provided', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    expect(mockTerm.open).toHaveBeenCalledWith(container)
    expect(mockFitAddon.fit).toHaveBeenCalled()
  })

  it('configures a Unicode-capable terminal font stack', () => {
    const container = makeContainer()

    renderHook(() => useTerminal({ sessionId: 's1', container }))

    expect(mockTerminalCtor).toHaveBeenCalledWith(expect.objectContaining({
      customGlyphs: true,
      fontFamily: TERMINAL_FONT_FAMILY,
    }))
    expect(TERMINAL_FONT_FAMILY).toContain('Terminal Powerline')
    expect(TERMINAL_FONT_FAMILY).toContain('Meslo LG M for Powerline')
    expect(TERMINAL_FONT_FAMILY).toContain('Symbol Neu for Powerline')
    expect(TERMINAL_FONT_FAMILY).toContain('Noto Sans Mono CJK JP')
    expect(TERMINAL_FONT_FAMILY).toContain('Hiragino Sans')
  })

  it('loads all addons on init', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    expect(mockTerm.loadAddon).toHaveBeenCalledTimes(2)
  })

  it('registers onData and onBinary handlers', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    expect(mockTerm.onData).toHaveBeenCalled()
    expect(mockTerm.onBinary).toHaveBeenCalled()
  })

  it('registers a custom key handler for copy shortcuts', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    expect(mockTerm.attachCustomKeyEventHandler).toHaveBeenCalledTimes(1)
  })

  it('copies selected text and suppresses terminal input on copy shortcut', () => {
    const container = makeContainer()
    mockTerm.hasSelection.mockReturnValue(true)
    mockTerm.getSelection.mockReturnValue('copied text')
    renderHook(() => useTerminal({ sessionId: 's1', container }))

    const handler = mockTerm.attachCustomKeyEventHandler.mock.calls[0][0] as (event: KeyboardEvent) => boolean
    const preventDefault = vi.fn()
    const allowed = handler({
      key: 'c',
      ctrlKey: true,
      metaKey: false,
      preventDefault,
    } as unknown as KeyboardEvent)

    expect(allowed).toBe(false)
    expect(preventDefault).toHaveBeenCalled()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('copied text')
  })

  it('keeps Ctrl+C available to the terminal when nothing is selected', () => {
    const container = makeContainer()
    mockTerm.hasSelection.mockReturnValue(false)
    renderHook(() => useTerminal({ sessionId: 's1', container }))

    const handler = mockTerm.attachCustomKeyEventHandler.mock.calls[0][0] as (event: KeyboardEvent) => boolean
    const allowed = handler({
      key: 'c',
      ctrlKey: true,
      metaKey: false,
      preventDefault: vi.fn(),
    } as unknown as KeyboardEvent)

    expect(allowed).toBe(true)
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled()
  })

  it('connects a WebSocket for the given sessionId', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 'mysession', container }))
    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0].url).toContain('mysession')
  })

  it('sends resize on WebSocket open when terminal is ready', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())
    const sentResizes = MockWebSocket.instances[0].sent.filter(
      (d) => typeof d === 'string' && (d as string).includes('resize')
    )
    expect(sentResizes.length).toBeGreaterThan(0)
  })

  it('writes binary data directly to terminal', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    const buf = new ArrayBuffer(4)
    act(() => MockWebSocket.instances[0].simulateMessage(buf))
    expect(mockWrite).toHaveBeenCalledWith(expect.any(Uint8Array))
  })

  it('writes error message to terminal on error control frame', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'error', message: 'something broke' })
      )
    )
    expect(mockWrite).toHaveBeenCalledWith(
      expect.stringContaining('something broke')
    )
  })

  it('does not write to terminal on status control frame', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'status', state: 'connected' })
      )
    )
    // write may have been called once for the initial onOpen resize, but not for status
    const writeCalls = mockWrite.mock.calls
    const statusWrites = writeCalls.filter((args) =>
      typeof args[0] === 'string' && args[0].includes('connected')
    )
    expect(statusWrites).toHaveLength(0)
  })

  it('handleResize fits and sends resize when connected', () => {
    const container = makeContainer()
    const { result } = renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    const sentBefore = MockWebSocket.instances[0].sent.length
    act(() => result.current.handleResize())

    const newSent = MockWebSocket.instances[0].sent.slice(sentBefore)
    const resizeSent = newSent.some(
      (d) => typeof d === 'string' && (d as string).includes('resize')
    )
    expect(mockFitAddon.fit).toHaveBeenCalled()
    expect(resizeSent).toBe(true)
  })

  it('handleResize does not send when not connected', () => {
    const container = makeContainer()
    const { result } = renderHook(() => useTerminal({ sessionId: 's1', container }))
    // Do NOT simulate open — connected stays false

    const sentBefore = MockWebSocket.instances[0].sent.length
    act(() => result.current.handleResize())

    expect(MockWebSocket.instances[0].sent.length).toBe(sentBefore)
  })

  it('disposes terminal on unmount', () => {
    vi.useFakeTimers()
    const container = makeContainer()
    const { unmount } = renderHook(() => useTerminal({ sessionId: 's1', container }))
    unmount()
    vi.runAllTimers()
    expect(mockTerm.dispose).toHaveBeenCalled()
    vi.useRealTimers()
  })

  it('returns connected state', () => {
    const container = makeContainer()
    const { result } = renderHook(() => useTerminal({ sessionId: 's1', container }))
    // Before open, connected should be false initially
    act(() => MockWebSocket.instances[0].simulateOpen())
    expect(result.current.connected).toBe(true)
  })

  it('onData callback encodes user input and sends via WebSocket', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    const ws = MockWebSocket.instances[0]
    const sentBefore = ws.sent.length
    const onDataCallback = mockTerm.onData.mock.calls[0][0] as (data: string) => void
    act(() => onDataCallback('hello'))
    expect(ws.sent.length).toBeGreaterThan(sentBefore)
  })

  it('onBinary callback encodes binary string and sends via WebSocket', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    const onBinaryCallback = mockTerm.onBinary.mock.calls[0][0] as (data: string) => void
    act(() => onBinaryCallback('ABC'))

    const sent = MockWebSocket.instances[0].sent
    const binaryFrames = sent.filter((d) => d instanceof Uint8Array)
    expect(binaryFrames.length).toBeGreaterThan(0)
    const frame = binaryFrames[binaryFrames.length - 1] as Uint8Array
    expect(frame[0]).toBe(0x41) // 'A'
    expect(frame[1]).toBe(0x42) // 'B'
    expect(frame[2]).toBe(0x43) // 'C'
  })

  it('copies selected text via fallbackCopy when clipboard API is unavailable', () => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
    const container = makeContainer()
    mockTerm.hasSelection.mockReturnValue(true)
    mockTerm.getSelection.mockReturnValue('fallback text')
    renderHook(() => useTerminal({ sessionId: 's1', container }))

    const handler = mockTerm.attachCustomKeyEventHandler.mock.calls[0][0] as (event: KeyboardEvent) => boolean
    handler({ key: 'c', ctrlKey: true, metaKey: false, preventDefault: vi.fn() } as unknown as KeyboardEvent)

    expect(document.execCommand).toHaveBeenCalledWith('copy')
  })

  it('reuses the same terminal instance across remounts for the same session', () => {
    const firstContainer = makeContainer()
    const secondContainer = makeContainer()

    const first = renderHook(() => useTerminal({ sessionId: 's1', container: firstContainer }))
    first.unmount()

    renderHook(() => useTerminal({ sessionId: 's1', container: secondContainer }))

    expect(mockTerminalCtor).toHaveBeenCalledTimes(1)
    expect(mockTerm.dispose).not.toHaveBeenCalled()
    expect(secondContainer.contains(mockTerm.element as HTMLElement)).toBe(true)
  })
})
