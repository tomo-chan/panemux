import { renderHook, act } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useBoardCommand } from './useBoardCommand'

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
  url: string
  protocols: string | string[] | undefined
  sent: unknown[] = []

  constructor(url: string, protocols?: string | string[]) {
    this.url = url
    this.protocols = protocols
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

  simulateMessage(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) })
  }
}

describe('useBoardCommand', () => {
  let originalWebSocket: typeof WebSocket

  beforeEach(() => {
    originalWebSocket = window.WebSocket
    MockWebSocket.instances = []
    window.WebSocket = MockWebSocket as unknown as typeof WebSocket
  })

  afterEach(() => {
    window.WebSocket = originalWebSocket
  })

  it('does not connect when disabled', () => {
    renderHook(() => useBoardCommand({ enabled: false, token: 'tok' }))
    expect(MockWebSocket.instances).toHaveLength(0)
  })

  it('does not connect without a token', () => {
    renderHook(() => useBoardCommand({ enabled: true, token: '' }))
    expect(MockWebSocket.instances).toHaveLength(0)
  })

  it('connects with the token as a WS subprotocol', () => {
    renderHook(() => useBoardCommand({ enabled: true, token: 'sekret' }))

    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0].protocols).toEqual(['sekret'])
    expect(MockWebSocket.instances[0].url).toContain('/ws/board-command')
  })

  it('reflects connected state from ws open/close', () => {
    const { result } = renderHook(() => useBoardCommand({ enabled: true, token: 'tok' }))
    const ws = MockWebSocket.instances[0]

    expect(result.current.connected).toBe(false)
    act(() => ws.simulateOpen())
    expect(result.current.connected).toBe(true)
    act(() => ws.close())
    expect(result.current.connected).toBe(false)
  })

  it('sendPrompt appends a pending turn and sends the prompt as JSON', () => {
    const { result } = renderHook(() => useBoardCommand({ enabled: true, token: 'tok' }))
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())

    act(() => result.current.sendPrompt('hello'))

    expect(result.current.turns).toHaveLength(1)
    expect(result.current.turns[0]).toMatchObject({ prompt: 'hello', lines: [], done: false })
    expect(result.current.pending).toBe(true)
    expect(ws.sent).toEqual([JSON.stringify({ prompt: 'hello' })])
  })

  it('sendPrompt is a no-op when the socket is not open', () => {
    const { result } = renderHook(() => useBoardCommand({ enabled: true, token: 'tok' }))
    const ws = MockWebSocket.instances[0]
    ws.readyState = MockWebSocket.CONNECTING

    act(() => result.current.sendPrompt('hello'))

    expect(result.current.turns).toHaveLength(0)
    expect(ws.sent).toHaveLength(0)
  })

  it('appends line frames to the last turn', () => {
    const { result } = renderHook(() => useBoardCommand({ enabled: true, token: 'tok' }))
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())
    act(() => result.current.sendPrompt('hello'))

    act(() => ws.simulateMessage({ type: 'line', raw: { type: 'result' } }))

    expect(result.current.turns[0].lines).toEqual([{ type: 'result' }])
    expect(result.current.pending).toBe(true) // still pending after a line
  })

  it('marks the turn done and clears pending on a done frame', () => {
    const { result } = renderHook(() => useBoardCommand({ enabled: true, token: 'tok' }))
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())
    act(() => result.current.sendPrompt('hello'))

    act(() => ws.simulateMessage({ type: 'done' }))

    expect(result.current.turns[0].done).toBe(true)
    expect(result.current.pending).toBe(false)
  })

  it('records an error frame on the turn and clears pending', () => {
    const { result } = renderHook(() => useBoardCommand({ enabled: true, token: 'tok' }))
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())
    act(() => result.current.sendPrompt('hello'))

    act(() => ws.simulateMessage({ type: 'error', message: 'claude exited with error: exit status 1' }))

    expect(result.current.turns[0]).toMatchObject({
      error: 'claude exited with error: exit status 1',
      done: true,
    })
    expect(result.current.pending).toBe(false)
  })

  it('records a busy frame on the turn and clears pending', () => {
    const { result } = renderHook(() => useBoardCommand({ enabled: true, token: 'tok' }))
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())
    act(() => result.current.sendPrompt('hello'))

    act(() => ws.simulateMessage({ type: 'busy' }))

    expect(result.current.turns[0]).toMatchObject({ busy: true, done: true })
    expect(result.current.pending).toBe(false)
  })

  it('ignores a message frame when there is no turn to attach it to', () => {
    const { result } = renderHook(() => useBoardCommand({ enabled: true, token: 'tok' }))
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())

    act(() => ws.simulateMessage({ type: 'done' }))

    expect(result.current.turns).toHaveLength(0)
  })

  it('ignores malformed / schema-invalid messages', () => {
    const { result } = renderHook(() => useBoardCommand({ enabled: true, token: 'tok' }))
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())
    act(() => result.current.sendPrompt('hello'))

    act(() => ws.onmessage?.({ data: 'not json' }))
    act(() => ws.onmessage?.({ data: JSON.stringify({ type: 'unknown-type' }) }))

    expect(result.current.turns[0].done).toBe(false)
  })

  it('closes the socket on unmount', () => {
    const { unmount } = renderHook(() => useBoardCommand({ enabled: true, token: 'tok' }))
    const ws = MockWebSocket.instances[0]
    const closeSpy = vi.spyOn(ws, 'close')

    unmount()

    expect(closeSpy).toHaveBeenCalled()
  })

  it('reconnects a new socket when re-enabled after being disabled', () => {
    const { rerender } = renderHook(
      ({ enabled }) => useBoardCommand({ enabled, token: 'tok' }),
      { initialProps: { enabled: true } }
    )
    expect(MockWebSocket.instances).toHaveLength(1)

    rerender({ enabled: false })
    rerender({ enabled: true })

    expect(MockWebSocket.instances).toHaveLength(2)
  })
})
