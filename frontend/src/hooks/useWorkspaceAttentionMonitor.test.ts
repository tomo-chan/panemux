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
  let originalLocalStorage: Storage | undefined
  let hasFocusSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    originalWebSocket = window.WebSocket
    originalLocalStorage = window.localStorage
    MockWebSocket.instances = []
    window.WebSocket = MockWebSocket as unknown as typeof WebSocket
    const storageData = new Map<string, string>()
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => storageData.get(key) ?? null,
        setItem: (key: string, value: string) => {
          storageData.set(key, value)
        },
        removeItem: (key: string) => {
          storageData.delete(key)
        },
        clear: () => {
          storageData.clear()
        },
        key: (index: number) => [...storageData.keys()][index] ?? null,
        get length() {
          return storageData.size
        },
      } satisfies Storage,
    })
    window.localStorage.clear()
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'visible',
    })
    hasFocusSpy = vi.spyOn(document, 'hasFocus').mockReturnValue(true)
  })

  afterEach(() => {
    window.WebSocket = originalWebSocket
    if (originalLocalStorage) {
      Object.defineProperty(window, 'localStorage', {
        configurable: true,
        value: originalLocalStorage,
      })
    }
    vi.restoreAllMocks()
  })

  it('subscribes to every pane across all workspaces', () => {
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention: vi.fn() }))

    expect(MockWebSocket.instances).toHaveLength(3)
    expect(MockWebSocket.instances.map((instance) => instance.url)).toEqual(
      expect.arrayContaining([
        'ws://localhost:3000/ws/main',
        'ws://localhost:3000/ws/side',
        'ws://localhost:3000/ws/ops-main',
      ]),
    )
  })

  it('does not reconnect monitor sockets when only active workspace or maximize state changes', () => {
    const onAttention = vi.fn()
    const { rerender } = renderHook(
      ({ currentWorkspaces, currentMaximizedPaneId }) =>
        useWorkspaceAttentionMonitor({
          workspaces: currentWorkspaces,
          maximizedPaneId: currentMaximizedPaneId,
          onAttention,
        }),
      {
        initialProps: {
          currentWorkspaces: workspaces,
          currentMaximizedPaneId: null as string | null,
        },
      },
    )

    expect(MockWebSocket.instances).toHaveLength(3)

    rerender({
      currentWorkspaces: { ...workspaces, active: 'ops' },
      currentMaximizedPaneId: null,
    })
    rerender({
      currentWorkspaces: { ...workspaces, active: 'ops' },
      currentMaximizedPaneId: 'ops-main',
    })

    expect(MockWebSocket.instances).toHaveLength(3)
  })

  it('notifies when an inactive workspace pane emits a confirmation prompt', () => {
    const onAttention = vi.fn()
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention }))

    act(() => MockWebSocket.instances.find((instance) => instance.url.endsWith('/ops-main'))?.simulateOpen())
    act(() =>
      MockWebSocket.instances
        .find((instance) => instance.url.endsWith('/ops-main'))
        ?.simulateMessage(new TextEncoder().encode('Agent is waiting for confirmation: proceed?').buffer),
    )

    expect(onAttention).toHaveBeenCalledWith('ops-main', true)
  })

  it('does not notify for a visible pane while the browser is active', () => {
    const onAttention = vi.fn()
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention }))
    const socket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/main'))

    act(() => socket?.simulateOpen())
    const prompt = new TextEncoder().encode('Codex needs confirmation before continuing. Approve?').buffer
    act(() => socket?.simulateMessage(prompt))

    expect(onAttention).toHaveBeenCalledWith('main', false)
  })

  it('notifies for the active workspace when the browser is inactive', () => {
    hasFocusSpy.mockReturnValue(false)
    const onAttention = vi.fn()
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention }))
    const socket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/main'))

    act(() => socket?.simulateOpen())
    act(() =>
      socket?.simulateMessage(new TextEncoder().encode('Codex needs confirmation before continuing. Approve?').buffer),
    )

    expect(onAttention).toHaveBeenCalledWith('main', true)
  })

  it('notifies for a non-maximized pane hidden by maximize while the browser is active', () => {
    const onAttention = vi.fn()
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: 'main', onAttention }))
    const socket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/side'))

    act(() => socket?.simulateOpen())
    act(() =>
      socket?.simulateMessage(new TextEncoder().encode('Agent is waiting for confirmation: proceed?').buffer),
    )

    expect(onAttention).toHaveBeenCalledWith('side', true)
  })

  it('deduplicates repeated redraws from the same pane across reconnects', () => {
    const onAttention = vi.fn()
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention }))
    const socket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/ops-main'))

    act(() => socket?.simulateOpen())
    const prompt = new TextEncoder().encode('Codex needs confirmation before continuing. Approve?').buffer
    act(() => socket?.simulateMessage(prompt))
    act(() => socket?.simulateMessage(prompt))
    act(() => socket?.simulateOpen())
    act(() => socket?.simulateMessage(prompt))

    expect(onAttention).toHaveBeenCalledTimes(3)
    expect(onAttention).toHaveBeenNthCalledWith(1, 'ops-main', true)
    expect(onAttention).toHaveBeenNthCalledWith(2, 'ops-main', false)
    expect(onAttention).toHaveBeenNthCalledWith(3, 'ops-main', false)
  })

  it('deduplicates the last notified prompt after a remount', () => {
    const onAttention = vi.fn()
    const { unmount } = renderHook(() =>
      useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention }),
    )
    const socket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/ops-main'))
    const prompt = new TextEncoder().encode('Codex needs confirmation before continuing. Approve?').buffer

    act(() => socket?.simulateOpen())
    act(() => socket?.simulateMessage(prompt))
    unmount()
    MockWebSocket.instances = []

    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention }))
    const remountedSocket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/ops-main'))
    act(() => remountedSocket?.simulateOpen())
    act(() => remountedSocket?.simulateMessage(prompt))

    expect(onAttention).toHaveBeenCalledTimes(2)
    expect(onAttention).toHaveBeenNthCalledWith(1, 'ops-main', true)
    expect(onAttention).toHaveBeenNthCalledWith(2, 'ops-main', false)
  })

  it('notifies again when the same pane receives a different prompt', () => {
    const onAttention = vi.fn()
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention }))
    const socket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/ops-main'))

    act(() => socket?.simulateOpen())
    act(() =>
      socket?.simulateMessage(new TextEncoder().encode('Codex needs confirmation before continuing. Approve?').buffer),
    )
    act(() =>
      socket?.simulateMessage(new TextEncoder().encode('Allow the github MCP server to run tool "list_pull_requests"?\n1. Allow\n2. Allow for this session\n3. Always allow\n4. Cancel\nenter to submit | esc to cancel').buffer),
    )

    expect(onAttention).toHaveBeenCalledTimes(2)
  })

  it('notifies for the same prompt text when it appears in a different pane', () => {
    const onAttention = vi.fn()
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention }))
    const prompt = new TextEncoder().encode('Codex needs confirmation before continuing. Approve?').buffer
    const mainSocket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/main'))
    const opsSocket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/ops-main'))

    hasFocusSpy.mockReturnValue(false)
    act(() => mainSocket?.simulateOpen())
    act(() => mainSocket?.simulateMessage(prompt))
    hasFocusSpy.mockReturnValue(true)
    act(() => opsSocket?.simulateOpen())
    act(() => opsSocket?.simulateMessage(prompt))

    expect(onAttention).toHaveBeenCalledTimes(2)
    expect(onAttention).toHaveBeenNthCalledWith(1, 'main', true)
    expect(onAttention).toHaveBeenNthCalledWith(2, 'ops-main', true)
  })

  it('falls back to in-memory dedupe when localStorage is unavailable', () => {
    const failingStorage = {
      getItem() {
        throw new Error('storage unavailable')
      },
      setItem() {
        throw new Error('storage unavailable')
      },
      removeItem() {
        throw new Error('storage unavailable')
      },
      clear() {
        throw new Error('storage unavailable')
      },
      key() {
        return null
      },
      length: 0,
    } as Storage
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: failingStorage,
    })

    const onAttention = vi.fn()
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention }))
    const socket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/ops-main'))
    const prompt = new TextEncoder().encode('Codex needs confirmation before continuing. Approve?').buffer

    act(() => socket?.simulateOpen())
    act(() => socket?.simulateMessage(prompt))
    act(() => socket?.simulateMessage(prompt))

    expect(onAttention).toHaveBeenCalledTimes(2)
    expect(onAttention).toHaveBeenNthCalledWith(1, 'ops-main', true)
    expect(onAttention).toHaveBeenNthCalledWith(2, 'ops-main', false)
  })

  it('ignores text control frames because only terminal output bytes should trigger attention', () => {
    const onAttention = vi.fn()
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention }))
    const socket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/main'))

    act(() => socket?.simulateOpen())
    act(() => socket?.simulateMessage(JSON.stringify({ type: 'status', state: 'connected' })))

    expect(onAttention).not.toHaveBeenCalled()
  })

  it('closes a socket when it errors', () => {
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention: vi.fn() }))
    const socket = MockWebSocket.instances.find((instance) => instance.url.endsWith('/main'))
    const closeSpy = vi.spyOn(socket!, 'close')

    act(() => socket?.onerror?.())

    expect(closeSpy).toHaveBeenCalledTimes(1)
  })

  it('does nothing when there are no workspaces to monitor', () => {
    renderHook(() => useWorkspaceAttentionMonitor({ workspaces: null, maximizedPaneId: null, onAttention: vi.fn() }))

    expect(MockWebSocket.instances).toHaveLength(0)
  })

  it('closes all sockets on unmount', () => {
    const closeSpy = vi.spyOn(MockWebSocket.prototype, 'close')
    const { unmount } = renderHook(() => useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId: null, onAttention: vi.fn() }))

    unmount()

    expect(closeSpy).toHaveBeenCalledTimes(3)
  })
})
