import { renderHook, act } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  TERMINAL_URL_REGEX,
  __computePullRequestLinksForTests,
  __resetTerminalEntriesForTests,
  useTerminal,
} from './useTerminal'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

// ── xterm.js mocks ───────────────────────────────────────────────────────────
const { mockWrite, mockTerm, mockFitAddon, mockTerminalCtor } = vi.hoisted(() => {
  const mockWrite = vi.fn()
  const mockTerm = {
    options: { disableStdin: false },
    attachCustomKeyEventHandler: vi.fn(),
    element: undefined as HTMLElement | undefined,
    registerLinkProvider: vi.fn(),
    hasSelection: vi.fn(() => false),
    getSelection: vi.fn(() => ''),
    loadAddon: vi.fn(),
    open: vi.fn(),
    onData: vi.fn(),
    onBinary: vi.fn(),
    buffer: {
      active: {
        getLine: vi.fn(() => null),
      },
    },
    dispose: vi.fn(),
    cols: 80,
    rows: 24,
    refresh: vi.fn(),
    write: vi.fn((data: string | Uint8Array, callback?: () => void) => {
      mockWrite(data)
      callback?.()
    }),
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
    mockTerm.options.disableStdin = false
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

  it('enables forced selection for tmux mouse mode across platforms', () => {
    const container = makeContainer()

    renderHook(() => useTerminal({ sessionId: 's1', container }))

    expect(mockTerminalCtor).toHaveBeenCalledWith(expect.objectContaining({
      macOptionClickForcesSelection: true,
    }))
  })

  it('loads all addons on init', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    expect(mockTerm.loadAddon).toHaveBeenCalledTimes(2)
  })

  it('configures the web links addon with the CJK-aware url regex', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))

    expect(WebLinksAddon).toHaveBeenCalledTimes(1)
    expect(vi.mocked(WebLinksAddon).mock.calls[0][1]).toEqual({ urlRegex: TERMINAL_URL_REGEX })
  })

  it('registers a custom link provider for pull request numbers', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container, repoURL: 'https://github.com/example/panemux' }))

    expect(mockTerm.registerLinkProvider).toHaveBeenCalledTimes(1)
  })

  it('turns visible #123 references into pull request links when repo metadata is available', () => {
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(null)
    const term = {
      buffer: {
        active: {
          getLine: vi.fn(() => ({
            translateToString: vi.fn(() => 'Reviewing #123 now'),
          })),
        },
      },
    } as unknown as typeof mockTerm

    const links = __computePullRequestLinksForTests(term as never, 'https://github.com/example/panemux', 1)

    expect(links).toHaveLength(1)
    expect(links[0].range).toEqual({
      start: { x: 10, y: 0 },
      end: { x: 14, y: 0 },
    })
    links[0].activate()
    expect(openSpy).toHaveBeenCalledWith(
      'https://github.com/example/panemux/pull/123',
      '_blank',
      'noopener,noreferrer',
    )
  })

  it('skips pull request link generation when repo metadata is unavailable', () => {
    const term = {
      buffer: {
        active: {
          getLine: vi.fn(() => ({
            translateToString: vi.fn(() => 'Reviewing #123 now'),
          })),
        },
      },
    } as unknown as typeof mockTerm

    const links = __computePullRequestLinksForTests(term as never, null, 1)

    expect(links).toHaveLength(0)
  })

  it('skips pull request link generation when the buffer line is missing', () => {
    const term = {
      buffer: {
        active: {
          getLine: vi.fn(() => null),
        },
      },
    } as unknown as typeof mockTerm

    const links = __computePullRequestLinksForTests(term as never, 'https://github.com/example/panemux', 1)

    expect(links).toHaveLength(0)
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

  it('notifies the caller when the terminal container receives interaction', () => {
    const container = makeContainer()
    const onInteraction = vi.fn()

    renderHook(() => useTerminal({ sessionId: 's1', container, onInteraction }))

    act(() => {
      container.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }))
      container.dispatchEvent(new KeyboardEvent('keydown', { key: 'a', bubbles: true }))
    })

    expect(onInteraction).toHaveBeenCalledTimes(2)
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

  it('waits for a non-zero terminal size before sending the initial resize', () => {
    vi.useFakeTimers()
    window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      cb(0)
      return 0
    }) as typeof window.requestAnimationFrame
    const container = makeContainer()
    let fitCalls = 0
    mockTerm.cols = 0
    mockTerm.rows = 0
    mockFitAddon.fit.mockImplementation(() => {
      fitCalls++
      if (fitCalls >= 3) {
        mockTerm.cols = 80
        mockTerm.rows = 24
      }
    })

    renderHook(() => useTerminal({ sessionId: 's1', container }))

    act(() => MockWebSocket.instances[0].simulateOpen())

    const initialResizeFrames = MockWebSocket.instances[0].sent.filter(
      (d) => typeof d === 'string' && (d as string).includes('"type":"resize"')
    )
    expect(initialResizeFrames).toHaveLength(0)

    act(() => vi.advanceTimersByTime(50))

    const resizeFrames = MockWebSocket.instances[0].sent.filter(
      (d) => typeof d === 'string' && (d as string).includes('"type":"resize"')
    )
    expect(resizeFrames).toHaveLength(1)
    expect(resizeFrames[0]).toContain('"cols":80')
    expect(resizeFrames[0]).toContain('"rows":24')
    vi.useRealTimers()
  })

  it('writes binary data directly to terminal', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    const buf = new ArrayBuffer(4)
    act(() => MockWebSocket.instances[0].simulateMessage(buf))
    expect(mockWrite).toHaveBeenCalledWith(expect.any(Uint8Array))
  })

  it('suppresses stdin while replayed output is being applied', () => {
    const container = makeContainer()
    const writesDisableState: boolean[] = []
    mockTerm.write.mockImplementation((data: string | Uint8Array, callback?: () => void) => {
      writesDisableState.push(mockTerm.options.disableStdin)
      mockWrite(data)
      callback?.()
    })

    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    const replayBuf = new TextEncoder().encode('\u001b[>0;276;0c').buffer
    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'replay', state: 'start' })
      )
    )
    expect(mockTerm.options.disableStdin).toBe(true)

    act(() => MockWebSocket.instances[0].simulateMessage(replayBuf))
    expect(writesDisableState).toEqual([true])

    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'replay', state: 'end' })
      )
    )
    expect(mockTerm.options.disableStdin).toBe(false)
  })

  it('keeps replay stdin suppression across buffered remount flushes', () => {
    const replayBuf = new TextEncoder().encode('\u001b[>0;276;0c').buffer
    const container = makeContainer()
    const writesDisableState: boolean[] = []
    mockTerm.write.mockImplementation((data: string | Uint8Array, callback?: () => void) => {
      writesDisableState.push(mockTerm.options.disableStdin)
      mockWrite(data)
      callback?.()
    })
    const { rerender } = renderHook(
      ({ currentContainer }: { currentContainer: HTMLDivElement | null }) =>
        useTerminal({ sessionId: 's1', container: currentContainer }),
      { initialProps: { currentContainer: null as HTMLDivElement | null } },
    )

    act(() => MockWebSocket.instances[0].simulateOpen())
    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'replay', state: 'start' })
      )
    )
    act(() => MockWebSocket.instances[0].simulateMessage(replayBuf))
    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'replay', state: 'end' })
      )
    )

    rerender({ currentContainer: container })

    expect(writesDisableState).toEqual([true])
    expect(mockTerm.options.disableStdin).toBe(false)
  })

  it('restores stdin before applying live output after replay ends', () => {
    const container = makeContainer()
    const writesDisableState: boolean[] = []
    mockTerm.write.mockImplementation((data: string | Uint8Array, callback?: () => void) => {
      writesDisableState.push(mockTerm.options.disableStdin)
      mockWrite(data)
      callback?.()
    })

    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    const replayBuf = new TextEncoder().encode('replayed prompt').buffer
    const liveBuf = new TextEncoder().encode('live output').buffer

    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'replay', state: 'start' })
      )
    )
    act(() => MockWebSocket.instances[0].simulateMessage(replayBuf))
    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'replay', state: 'end' })
      )
    )
    act(() => MockWebSocket.instances[0].simulateMessage(liveBuf))

    expect(writesDisableState).toEqual([true, false])
  })

  it('resets stale replay suppression when the socket reconnects', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    const replayBuf = new TextEncoder().encode('replayed prompt').buffer
    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'replay', state: 'start' })
      )
    )
    act(() => MockWebSocket.instances[0].simulateMessage(replayBuf))
    expect(mockTerm.options.disableStdin).toBe(true)

    act(() => MockWebSocket.instances[0].simulateOpen())
    expect(mockTerm.options.disableStdin).toBe(false)

    mockWrite.mockClear()
    const liveBuf = new TextEncoder().encode('live output').buffer
    act(() => MockWebSocket.instances[0].simulateMessage(liveBuf))

    expect(mockWrite).toHaveBeenCalledWith(expect.any(Uint8Array))
    expect(mockTerm.options.disableStdin).toBe(false)
  })

  it('buffers terminal output that arrives before the container is attached', () => {
    const buf = new TextEncoder().encode('prompt before attach').buffer
    const container = makeContainer()
    const { rerender } = renderHook(
      ({ currentContainer }: { currentContainer: HTMLDivElement | null }) =>
        useTerminal({ sessionId: 's1', container: currentContainer }),
      { initialProps: { currentContainer: null as HTMLDivElement | null } },
    )

    act(() => MockWebSocket.instances[0].simulateOpen())
    act(() => MockWebSocket.instances[0].simulateMessage(buf))

    expect(mockWrite).not.toHaveBeenCalledWith(expect.any(Uint8Array))

    rerender({ currentContainer: container })

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

  it('writes session ended message to terminal on exited status', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    mockWrite.mockClear()
    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'status', state: 'exited' })
      )
    )
    expect(mockWrite).toHaveBeenCalledWith(
      expect.stringContaining('[Session ended]')
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

  it('sessionExited is false initially', () => {
    const container = makeContainer()
    const { result } = renderHook(() => useTerminal({ sessionId: 's1', container }))
    expect(result.current.sessionState).toBe('running')
  })

  it('sessionState is exited after exited status frame', () => {
    const container = makeContainer()
    const { result } = renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'status', state: 'exited' })
      )
    )
    expect(result.current.sessionState).toBe('exited')
  })

  it('sessionState resets to running when WebSocket reconnects', () => {
    const container = makeContainer()
    const { result } = renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'status', state: 'exited' })
      )
    )
    expect(result.current.sessionState).toBe('exited')

    // Simulate WebSocket reconnecting
    act(() => MockWebSocket.instances[0].simulateOpen())
    expect(result.current.sessionState).toBe('running')
  })

  it('restartSession calls restart API then reconnects', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)

    const container = makeContainer()
    const { result } = renderHook(() => useTerminal({ sessionId: 'pane1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    const countBefore = MockWebSocket.instances.length
    await act(async () => { await result.current.restartSession() })

    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/pane1/restart', { method: 'POST' })
    expect(MockWebSocket.instances.length).toBeGreaterThan(countBefore)
  })

  it('disconnected status triggers one restart attempt and reconnect', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)

    const container = makeContainer()
    const { result } = renderHook(() => useTerminal({ sessionId: 'pane1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    const countBefore = MockWebSocket.instances.length
    await act(async () => {
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'status', state: 'disconnected' })
      )
      await Promise.resolve()
    })

    expect(result.current.sessionState).toBe('disconnected')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/pane1/restart', { method: 'POST' })
    expect(MockWebSocket.instances.length).toBeGreaterThan(countBefore)
    expect(result.current.reconnectFailed).toBe(false)
  })

  it('disconnected status does not start concurrent restart attempts', async () => {
    let resolveFetch: ((value: { ok: boolean }) => void) | null = null
    const fetchMock = vi.fn().mockImplementation(
      () => new Promise<{ ok: boolean }>((resolve) => { resolveFetch = resolve })
    )
    vi.stubGlobal('fetch', fetchMock)

    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 'pane1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    act(() => {
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'status', state: 'disconnected' })
      )
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'status', state: 'disconnected' })
      )
    })

    expect(fetchMock).toHaveBeenCalledTimes(1)

    await act(async () => {
      resolveFetch?.({ ok: true })
      await Promise.resolve()
    })
  })

  it('failed disconnected recovery exposes reconnectFailed state', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('network error'))
    vi.stubGlobal('fetch', fetchMock)

    const container = makeContainer()
    const { result } = renderHook(() => useTerminal({ sessionId: 'pane1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    await act(async () => {
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'status', state: 'disconnected' })
      )
      await Promise.resolve()
    })

    expect(result.current.sessionState).toBe('disconnected')
    expect(result.current.reconnectFailed).toBe(true)
  })

  it('WS reconnect exhaustion drives sessionState to disconnected and attempts recovery', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)
    vi.useFakeTimers()

    const container = makeContainer()
    const { result } = renderHook(() =>
      useTerminal({ sessionId: 'pane1', container, reconnectDelay: 10, maxReconnectAttempts: 1 })
    )

    await act(async () => {
      // No simulateOpen(): the WebSocket itself keeps failing to establish,
      // independent of any backend "disconnected" status frame.
      MockWebSocket.instances[0].close()
      await vi.advanceTimersByTimeAsync(20)
    })

    expect(result.current.sessionState).toBe('disconnected')
    expect(fetchMock).toHaveBeenCalledWith('/api/sessions/pane1/restart', { method: 'POST' })

    vi.useRealTimers()
  })

  // Regression test for the reported bug: an unstable ssh/ssh+tmux connection
  // left panes showing "reconnecting..." forever with no way out, because the
  // low-level WebSocket reconnect loop gave up silently after its attempt
  // budget without ever producing a "disconnected" status frame, so the
  // manual "Reconnect Session" button (driven by sessionState/reconnectFailed)
  // never appeared.
  it('manual Reconnect Session affordance is reachable after WS exhaustion without any status frame ever arriving', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('network error'))
    vi.stubGlobal('fetch', fetchMock)
    vi.useFakeTimers()

    const container = makeContainer()
    const { result } = renderHook(() =>
      useTerminal({ sessionId: 'pane1', container, reconnectDelay: 10, maxReconnectAttempts: 1 })
    )

    await act(async () => {
      MockWebSocket.instances[0].close()
      await vi.advanceTimersByTimeAsync(20)
    })

    expect(result.current.sessionState).toBe('disconnected')
    expect(result.current.reconnectFailed).toBe(true)

    vi.useRealTimers()
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

  it('repaints a reused terminal again after remount timing settles', () => {
    vi.useFakeTimers()
    window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      cb(0)
      return 0
    }) as typeof window.requestAnimationFrame
    const firstContainer = makeContainer()
    const secondContainer = makeContainer()

    const first = renderHook(() => useTerminal({ sessionId: 's1', container: firstContainer }))
    first.unmount()
    mockFitAddon.fit.mockClear()
    mockTerm.refresh.mockClear()

    renderHook(() => useTerminal({ sessionId: 's1', container: secondContainer }))

    expect(mockFitAddon.fit).toHaveBeenCalledTimes(1)
    expect(mockTerm.refresh).toHaveBeenCalledTimes(1)

    act(() => vi.advanceTimersByTime(50))

    expect(mockFitAddon.fit).toHaveBeenCalledTimes(2)
    expect(mockTerm.refresh).toHaveBeenCalledTimes(2)

    act(() => vi.advanceTimersByTime(200))

    expect(mockFitAddon.fit).toHaveBeenCalledTimes(3)
    expect(mockTerm.refresh).toHaveBeenCalledTimes(3)
    vi.useRealTimers()
  })

  it('repaints the attached terminal when the window regains focus', () => {
    vi.useFakeTimers()
    window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      cb(0)
      return 0
    }) as typeof window.requestAnimationFrame
    const container = makeContainer()

    renderHook(() => useTerminal({ sessionId: 's1', container }))
    mockFitAddon.fit.mockClear()
    mockTerm.refresh.mockClear()

    act(() => window.dispatchEvent(new Event('focus')))

    expect(mockFitAddon.fit).toHaveBeenCalledTimes(1)
    expect(mockTerm.refresh).toHaveBeenCalledTimes(1)

    act(() => vi.advanceTimersByTime(50))

    expect(mockFitAddon.fit).toHaveBeenCalledTimes(2)
    expect(mockTerm.refresh).toHaveBeenCalledTimes(2)

    act(() => vi.advanceTimersByTime(200))

    expect(mockFitAddon.fit).toHaveBeenCalledTimes(3)
    expect(mockTerm.refresh).toHaveBeenCalledTimes(3)
    vi.useRealTimers()
  })

  it('repaints the attached terminal when the document becomes visible', () => {
    vi.useFakeTimers()
    window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      cb(0)
      return 0
    }) as typeof window.requestAnimationFrame
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    const container = makeContainer()

    renderHook(() => useTerminal({ sessionId: 's1', container }))
    mockFitAddon.fit.mockClear()
    mockTerm.refresh.mockClear()

    act(() => document.dispatchEvent(new Event('visibilitychange')))

    expect(mockFitAddon.fit).toHaveBeenCalledTimes(1)
    expect(mockTerm.refresh).toHaveBeenCalledTimes(1)

    act(() => vi.advanceTimersByTime(50))

    expect(mockFitAddon.fit).toHaveBeenCalledTimes(2)
    expect(mockTerm.refresh).toHaveBeenCalledTimes(2)

    act(() => vi.advanceTimersByTime(200))

    expect(mockFitAddon.fit).toHaveBeenCalledTimes(3)
    expect(mockTerm.refresh).toHaveBeenCalledTimes(3)
    vi.useRealTimers()
  })

  it('does not repaint the attached terminal when the document becomes hidden', () => {
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    const container = makeContainer()

    renderHook(() => useTerminal({ sessionId: 's1', container }))
    mockFitAddon.fit.mockClear()
    mockTerm.refresh.mockClear()

    act(() => document.dispatchEvent(new Event('visibilitychange')))

    expect(mockFitAddon.fit).not.toHaveBeenCalled()
    expect(mockTerm.refresh).not.toHaveBeenCalled()
  })

  it('passes terminal input to the websocket', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    const ws = MockWebSocket.instances[0]
    const sentBefore = ws.sent.length
    const onDataCallback = mockTerm.onData.mock.calls[0][0] as (data: string) => void
    act(() => onDataCallback('hello'))
    expect(ws.sent.length).toBeGreaterThan(sentBefore)
  })

  it('passes binary input to the websocket', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    const ws = MockWebSocket.instances[0]
    const sentBefore = ws.sent.length
    const onBinaryCallback = mockTerm.onBinary.mock.calls[0][0] as (data: string) => void
    act(() => onBinaryCallback('ABC'))
    expect(ws.sent.length).toBeGreaterThan(sentBefore)
  })

  it('strips complete ANSI sequences from error messages before writing to terminal', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    mockWrite.mockClear()
    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'error', message: '\x1b[1mInjected bold\x1b[0m plain text' })
      )
    )

    expect(mockWrite).toHaveBeenCalledTimes(1)
    const written = mockWrite.mock.calls[0][0] as string
    // Full CSI sequences from the message payload must be stripped — no remnant brackets
    expect(written).not.toContain('\x1b[1m')
    expect(written).not.toContain('[1m')
    // Plain text from the message must still appear
    expect(written).toContain('Injected bold')
    expect(written).toContain('plain text')
    // The surrounding ANSI red/reset from the template are intentional and expected
    expect(written).toContain('\x1b[31m')
    expect(written).toContain('\x1b[0m')
  })

  it('strips private-use CSI sequences (DEC private markers) from error messages', () => {
    const container = makeContainer()
    renderHook(() => useTerminal({ sessionId: 's1', container }))
    act(() => MockWebSocket.instances[0].simulateOpen())

    mockWrite.mockClear()
    // \x1b[?25l (hide cursor) and \x1b[>1h (DEC private) are private-use CSI sequences
    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'error', message: '\x1b[?25lhidden cursor\x1b[>1htext' })
      )
    )

    expect(mockWrite).toHaveBeenCalledTimes(1)
    const written = mockWrite.mock.calls[0][0] as string
    expect(written).not.toContain('\x1b[?25l')
    expect(written).not.toContain('[?25l')
    expect(written).not.toContain('\x1b[>1h')
    expect(written).toContain('hidden cursor')
    expect(written).toContain('text')
  })
})

// The web links addon applies the regex as `new RegExp(regex.source, regex.flags + 'g')`
// (see LinkComputer.computeLink), so mirror that here instead of matching the regex directly.
function detectURLs(line: string): string[] {
  const rex = new RegExp(TERMINAL_URL_REGEX.source, `${TERMINAL_URL_REGEX.flags}g`)
  const found: string[] = []
  let match: RegExpExecArray | null
  while ((match = rex.exec(line)) !== null) {
    found.push(match[0])
  }
  return found
}

describe('TERMINAL_URL_REGEX', () => {
  it('is unicode-aware and not global so the addon can safely append the g flag', () => {
    expect(TERMINAL_URL_REGEX.flags).toContain('u')
    expect(TERMINAL_URL_REGEX.global).toBe(false)
  })

  it.each([
    ['ideographic full stop', 'https://example.com/docs\u3002', 'https://example.com/docs'],
    ['ideographic comma then text', 'https://example.com/docs\u3001\u6b21\u306e\u624b\u9806\u3078', 'https://example.com/docs'],
    ['fullwidth parentheses', '\uff08https://example.com/docs\uff09', 'https://example.com/docs'],
    ['corner brackets', '\u300chttps://example.com/docs\u300d', 'https://example.com/docs'],
    ['lenticular bracket', 'https://example.com/docs\u3010', 'https://example.com/docs'],
    ['katakana middle dot', 'https://example.com/docs\u30fb', 'https://example.com/docs'],
    ['halfwidth ideographic full stop', 'https://example.com/docs\uff61', 'https://example.com/docs'],
    ['horizontal ellipsis', 'https://example.com/docs\u2026', 'https://example.com/docs'],
    ['curly double quotes', '\u201chttps://example.com/docs\u201d', 'https://example.com/docs'],
    ['wave dash', 'https://example.com/docs\u301c', 'https://example.com/docs'],
    ['fullwidth colon', 'https://example.com/docs\uff1a', 'https://example.com/docs'],
  ])('drops trailing CJK punctuation (%s)', (_name, line, expected) => {
    expect(detectURLs(line)).toEqual([expected])
  })

  it.each([
    ['full stop', 'https://example.com/a.', 'https://example.com/a'],
    ['comma', 'https://example.com/a,', 'https://example.com/a'],
    ['exclamation mark', 'https://example.com/a!', 'https://example.com/a'],
    ['question mark', 'https://example.com/a?', 'https://example.com/a'],
    ['colon', 'https://example.com/a:', 'https://example.com/a'],
    ['closing paren', 'see https://example.com/a) here', 'https://example.com/a'],
    ['angle brackets', '<https://example.com/a>', 'https://example.com/a'],
    ['double quotes', '"https://example.com/a"', 'https://example.com/a'],
  ])('keeps the existing ASCII boundary behaviour (%s)', (_name, line, expected) => {
    expect(detectURLs(line)).toEqual([expected])
  })

  it.each([
    ['raw IRI path', 'https://ja.wikipedia.org/wiki/\u65e5\u672c\u8a9e'],
    ['percent-encoded path', 'https://example.com/%E6%97%A5%E6%9C%AC%E8%AA%9E'],
    ['fullwidth alphanumerics in path', 'https://example.com/\uff46\uff55\uff4c\uff4c\uff11\uff12\uff13'],
    ['trailing slash', 'https://example.com/'],
    ['bare host', 'https://example.com'],
    ['credentials, port, query and fragment', 'https://user:pw@example.com:8443/p?q=1#frag'],
    ['loopback with port', 'http://localhost:8080/path'],
    ['uppercase scheme', 'HTTPS://example.com/a'],
    ['ideographic iteration mark in path', 'https://ja.wikipedia.org/wiki/\u65e5\u3005'],
    ['ideographic closing mark in path', 'https://ja.wikipedia.org/wiki/\u3006\u5207'],
    ['ideographic number zero in path', 'https://ja.wikipedia.org/wiki/\u3007\u3007'],
  ])('keeps valid urls intact (%s)', (_name, line) => {
    expect(detectURLs(line)).toEqual([line])
  })

  it('drops trailing CJK punctuation without truncating a raw IRI path', () => {
    expect(detectURLs('https://ja.wikipedia.org/wiki/\u65e5\u672c\u8a9e\u3002')).toEqual([
      'https://ja.wikipedia.org/wiki/\u65e5\u672c\u8a9e',
    ])
  })

  it('detects every url on a line that mixes CJK delimiters', () => {
    const line = '\uff08https://a.example.com/x\uff09\u3068\u300chttps://b.example.com/y\u300d'
    expect(detectURLs(line)).toEqual(['https://a.example.com/x', 'https://b.example.com/y'])
  })

  it.each([
    ['unsupported scheme', 'ftp://example.com/a'],
    ['file scheme', 'file:///tmp/sample-project'],
    ['scheme-less host', 'example.com/a'],
    ['scheme only', 'https://'],
  ])('does not detect a link for %s', (_name, line) => {
    expect(detectURLs(line)).toEqual([])
  })

  // Documented limitation (see issue #173): kana directly following a url cannot be
  // distinguished from a legitimate kana IRI path, so the whole run stays part of the link.
  it('still absorbs kana that directly follows a url with no delimiter', () => {
    const line = 'https://example.com/docs\u3092\u53c2\u7167\u3057\u3066\u304f\u3060\u3055\u3044'
    expect(detectURLs(line)).toEqual([line])
  })
})
