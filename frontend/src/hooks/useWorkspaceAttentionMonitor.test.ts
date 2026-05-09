import { renderHook, act } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useWorkspaceAttentionMonitor } from './useWorkspaceAttentionMonitor'
import type { WorkspacesResponse } from '../schemas'

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

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }

  close() {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.()
  }

  simulateOpen() {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.()
  }

  simulateMessage(data: string | ArrayBuffer) {
    this.onmessage?.({ data })
  }
}

const workspaces: WorkspacesResponse = {
  active: 'dev',
  tab_position: 'top',
  items: [
    {
      id: 'dev',
      title: 'Dev',
      layout: {
        direction: 'horizontal',
        children: [
          { size: 50, pane: { id: 'main', type: 'local' } },
          { size: 50, pane: { id: 'side', type: 'local' } },
        ],
      },
    },
    {
      id: 'ops',
      title: 'Ops',
      layout: {
        direction: 'vertical',
        children: [{ size: 100, pane: { id: 'ops-main', type: 'local' } }],
      },
    },
  ],
}

describe('useWorkspaceAttentionMonitor', () => {
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

  it('subscribes to every pane across all workspaces', () => {
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, onAttention: vi.fn() }))

    expect(MockWebSocket.instances).toHaveLength(3)
    expect(MockWebSocket.instances.map((instance) => instance.url)).toEqual(
      expect.arrayContaining([
        'ws://localhost:3000/ws/main',
        'ws://localhost:3000/ws/side',
        'ws://localhost:3000/ws/ops-main',
      ]),
    )
  })

  it('notifies when an inactive workspace pane emits a confirmation prompt', () => {
    const onAttention = vi.fn()
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, onAttention }))

    act(() => MockWebSocket.instances.find((instance) => instance.url.endsWith('/ops-main'))?.simulateOpen())
    act(() =>
      MockWebSocket.instances
        .find((instance) => instance.url.endsWith('/ops-main'))
        ?.simulateMessage(new TextEncoder().encode('Agent is waiting for confirmation: proceed?').buffer),
    )

    expect(onAttention).toHaveBeenCalledWith('ops-main')
  })

  it('deduplicates repeated redraws from the same pane', () => {
    const onAttention = vi.fn()
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, onAttention }))
    const socket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/main'))

    act(() => socket?.simulateOpen())
    const prompt = new TextEncoder().encode('Codex needs confirmation before continuing. Approve?').buffer
    act(() => socket?.simulateMessage(prompt))
    act(() => socket?.simulateMessage(prompt))

    expect(onAttention).toHaveBeenCalledTimes(1)
  })

  it('ignores text control frames because only terminal output bytes should trigger attention', () => {
    const onAttention = vi.fn()
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, onAttention }))
    const socket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/main'))

    act(() => socket?.simulateOpen())
    act(() => socket?.simulateMessage(JSON.stringify({ type: 'status', state: 'connected' })))

    expect(onAttention).not.toHaveBeenCalled()
  })

  it('closes a socket when it errors', () => {
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, onAttention: vi.fn() }))
    const socket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/main'))
    const closeSpy = vi.spyOn(socket!, 'close')

    act(() => socket?.onerror?.())

    expect(closeSpy).toHaveBeenCalledTimes(1)
  })

  it('does nothing when there are no workspaces to monitor', () => {
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces: null, onAttention: vi.fn() }))

    expect(MockWebSocket.instances).toHaveLength(0)
  })

  it('closes all sockets on unmount', () => {
    const closeSpy = vi.spyOn(MockWebSocket.prototype, 'close')
    const { unmount } = renderHook(() => useWorkspaceAttentionMonitor({ workspaces, onAttention: vi.fn() }))

    unmount()

    expect(closeSpy).toHaveBeenCalledTimes(3)
  })
})
