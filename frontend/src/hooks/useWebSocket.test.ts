import { renderHook, act } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useWebSocket } from './useWebSocket'

// MockWebSocket must replicate the static constants so useWebSocket's
// `wsRef.current?.readyState === WebSocket.OPEN` comparison works.
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

  send(data: unknown) {
    this.sent.push(data)
  }

  close() {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.()
  }

  simulateOpen() {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.()
  }

  simulateClose() {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.()
  }

  simulateMessage(data: string | ArrayBuffer) {
    this.onmessage?.({ data })
  }
}

describe('useWebSocket', () => {
  let originalWebSocket: typeof WebSocket

  beforeEach(() => {
    originalWebSocket = window.WebSocket
    MockWebSocket.instances = []
    window.WebSocket = MockWebSocket as unknown as typeof WebSocket
    vi.useFakeTimers()
  })

  afterEach(() => {
    window.WebSocket = originalWebSocket
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('creates a WebSocket connection on mount', () => {
    const onMessage = vi.fn()
    renderHook(() => useWebSocket('ws://localhost/ws/s1', { onMessage }))
    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0].url).toBe('ws://localhost/ws/s1')
  })

  it('calls onOpen when connection opens', () => {
    const onMessage = vi.fn()
    const onOpen = vi.fn()
    renderHook(() => useWebSocket('ws://localhost/ws/s1', { onMessage, onOpen }))
    act(() => MockWebSocket.instances[0].simulateOpen())
    expect(onOpen).toHaveBeenCalledOnce()
  })

  it('calls onClose when connection closes', () => {
    const onMessage = vi.fn()
    const onClose = vi.fn()
    renderHook(() => useWebSocket('ws://localhost/ws/s1', { onMessage, onClose }))
    act(() => MockWebSocket.instances[0].simulateOpen())
    act(() => MockWebSocket.instances[0].simulateClose())
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('sends data when connection is open', () => {
    const onMessage = vi.fn()
    const { result } = renderHook(() =>
      useWebSocket('ws://localhost/ws/s1', { onMessage })
    )
    act(() => MockWebSocket.instances[0].simulateOpen())
    act(() => result.current.send('hello'))
    expect(MockWebSocket.instances[0].sent).toContain('hello')
  })

  it('reconnects after disconnect', () => {
    const onMessage = vi.fn()
    renderHook(() =>
      useWebSocket('ws://localhost/ws/s1', { onMessage, reconnectDelay: 100 })
    )
    act(() => MockWebSocket.instances[0].simulateOpen())
    act(() => MockWebSocket.instances[0].simulateClose())
    expect(MockWebSocket.instances).toHaveLength(1)
    act(() => vi.advanceTimersByTime(200))
    expect(MockWebSocket.instances).toHaveLength(2)
  })

  it('passes valid text messages to onMessage handler', () => {
    const onMessage = vi.fn()
    renderHook(() => useWebSocket('ws://localhost/ws/s1', { onMessage }))
    act(() => MockWebSocket.instances[0].simulateOpen())
    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'status', state: 'connected' })
      )
    )
    expect(onMessage).toHaveBeenCalledOnce()
  })

  it('ignores invalid text messages', () => {
    const onMessage = vi.fn()
    renderHook(() => useWebSocket('ws://localhost/ws/s1', { onMessage }))
    act(() => MockWebSocket.instances[0].simulateOpen())
    act(() => MockWebSocket.instances[0].simulateMessage('not json at all'))
    act(() =>
      MockWebSocket.instances[0].simulateMessage(
        JSON.stringify({ type: 'unknown_type' })
      )
    )
    expect(onMessage).not.toHaveBeenCalled()
  })

  it('closes connection when onerror fires', () => {
    const onMessage = vi.fn()
    renderHook(() => useWebSocket('ws://localhost/ws/s1', { onMessage }))
    act(() => MockWebSocket.instances[0].simulateOpen())
    act(() => MockWebSocket.instances[0].onerror?.())
    expect(MockWebSocket.instances[0].readyState).toBe(MockWebSocket.CLOSED)
  })

  it('stops reconnecting after max attempts', () => {
    const onMessage = vi.fn()
    renderHook(() =>
      useWebSocket('ws://localhost/ws/s1', { onMessage, reconnectDelay: 100, maxReconnectAttempts: 1 })
    )
    act(() => MockWebSocket.instances[0].simulateClose())
    act(() => vi.advanceTimersByTime(200))
    // connect() was called but attemptsRef >= maxReconnectAttempts → no new instance
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('does not reconnect if component unmounts before reconnect delay fires', () => {
    const onMessage = vi.fn()
    const { unmount } = renderHook(() =>
      useWebSocket('ws://localhost/ws/s1', { onMessage, reconnectDelay: 500 })
    )
    // Disconnect queues a reconnect timer
    act(() => MockWebSocket.instances[0].simulateOpen())
    act(() => MockWebSocket.instances[0].simulateClose())
    // Unmount before the timer fires: sets mountedRef.current = false
    unmount()
    // Timer fires: connect() sees !mountedRef.current and returns early
    act(() => vi.advanceTimersByTime(1000))
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('does not send data when WebSocket is not open', () => {
    const onMessage = vi.fn()
    const { result } = renderHook(() => useWebSocket('ws://localhost/ws/s1', { onMessage }))
    MockWebSocket.instances[0].readyState = MockWebSocket.CLOSED
    act(() => result.current.send('hello'))
    expect(MockWebSocket.instances[0].sent).toHaveLength(0)
  })

  it('reconnect creates a new WebSocket connection', () => {
    const onMessage = vi.fn()
    const { result } = renderHook(() =>
      useWebSocket('ws://localhost/ws/s1', { onMessage })
    )
    act(() => MockWebSocket.instances[0].simulateOpen())

    const countBefore = MockWebSocket.instances.length
    act(() => result.current.reconnect())

    expect(MockWebSocket.instances.length).toBeGreaterThan(countBefore)
  })

  it('reconnect works even after max attempts are exhausted', () => {
    const onMessage = vi.fn()
    const { result } = renderHook(() =>
      useWebSocket('ws://localhost/ws/s1', { onMessage, reconnectDelay: 100, maxReconnectAttempts: 1 })
    )
    // Exhaust reconnect attempts
    act(() => MockWebSocket.instances[0].simulateClose())
    act(() => vi.advanceTimersByTime(200))
    expect(MockWebSocket.instances).toHaveLength(1) // stopped reconnecting

    act(() => result.current.reconnect())
    expect(MockWebSocket.instances).toHaveLength(2)
  })

  it('passes binary messages through without validation', () => {
    const onMessage = vi.fn()
    renderHook(() => useWebSocket('ws://localhost/ws/s1', { onMessage }))
    act(() => MockWebSocket.instances[0].simulateOpen())
    const buf = new ArrayBuffer(4)
    act(() => MockWebSocket.instances[0].simulateMessage(buf))
    expect(onMessage).toHaveBeenCalledWith(buf, true)
  })
})
