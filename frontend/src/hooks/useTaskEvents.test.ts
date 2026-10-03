import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useTaskEvents, type TaskEventChange } from './useTaskEvents'

class MockWebSocket {
  static instances: MockWebSocket[] = []

  onopen: (() => void) | null = null
  onmessage: ((e: { data: unknown }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  url: string

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }

  close() {
    if (this.closed) return
    this.closed = true
    this.onclose?.()
  }

  send(frame: unknown) {
    this.onmessage?.({ data: typeof frame === 'string' ? frame : JSON.stringify(frame) })
  }
}

const latest = () => MockWebSocket.instances[MockWebSocket.instances.length - 1]

const task = (id: string, state = 'busy', extra: Record<string, unknown> = {}) => ({
  id, host: '', agent: 'claude', state, location: { kind: 'tmux', tmux_session: id, attachable: true }, ...extra,
})

const snapshot = (seq: number, tasks: unknown[] = [], epoch = 'e0') => ({
  type: 'snapshot', epoch, seq, hosts: [{ name: '', status: 'ok' }], tasks,
})

describe('useTaskEvents', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    MockWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockWebSocket)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('opens one stream and goes live on its snapshot', () => {
    const changes: TaskEventChange[] = []
    const { result } = renderHook(() => useTaskEvents((change) => changes.push(change)))

    expect(MockWebSocket.instances).toHaveLength(1)
    expect(latest().url).toMatch(/\/ws\/tasks\/events$/)
    expect(result.current.status).toBe('connecting')

    act(() => latest().send(snapshot(3, [task('a')])))
    expect(result.current.status).toBe('live')
    expect(result.current.store?.tasks.get('a')?.state).toBe('busy')

    act(() => latest().send({ type: 'task', epoch: 'e0', seq: 4, op: 'changed', prev_state: 'busy', task: task('a', 'wait', { wait_id: 'w1-a' }) }))
    expect(result.current.store?.tasks.get('a')?.state).toBe('wait')
    expect(changes.map((change) => change.frame.type)).toEqual(['snapshot', 'task'])
    expect(changes[1].before?.tasks.get('a')?.state).toBe('busy')
    expect(changes[1].after.tasks.get('a')?.state).toBe('wait')
  })

  it.each([
    ['a gap in seq', { type: 'task', epoch: 'e0', seq: 9, op: 'added', task: task('b') }],
    ['a frame failing its schema', { type: 'task', epoch: 'e0', seq: 4, op: 'added' }],
    ['text that is not JSON', 'not json'],
  ])('discards what it holds and reconnects from a fresh snapshot on %s', (_name, frame) => {
    const changes: TaskEventChange[] = []
    const { result } = renderHook(() => useTaskEvents((change) => changes.push(change)))
    act(() => latest().send(snapshot(3, [task('a')])))

    act(() => latest().send(frame))
    expect(latest().closed).toBe(true)
    expect(result.current.store).toBeNull()
    expect(result.current.status).toBe('offline')
    expect(changes).toHaveLength(1)

    act(() => { vi.advanceTimersByTime(2000) })
    expect(MockWebSocket.instances).toHaveLength(2)
    act(() => latest().send(snapshot(20, [task('c')], 'e1')))
    expect(result.current.status).toBe('live')
    expect([...(result.current.store?.tasks.keys() ?? [])]).toEqual(['c'])
  })

  it('reconnects without a limit, backing off from 2 to 30 seconds', () => {
    renderHook(() => useTaskEvents(() => {}))
    const waits: number[] = []
    for (let attempt = 0; attempt < 8; attempt += 1) {
      const before = MockWebSocket.instances.length
      act(() => latest().close())
      let waited = 0
      while (MockWebSocket.instances.length === before) {
        act(() => { vi.advanceTimersByTime(1000) })
        waited += 1000
      }
      waits.push(waited)
    }
    expect(waits).toEqual([2000, 4000, 8000, 16000, 30000, 30000, 30000, 30000])
  })

  it('starts the backoff over once a connection delivers its snapshot', () => {
    renderHook(() => useTaskEvents(() => {}))
    act(() => latest().close())
    act(() => { vi.advanceTimersByTime(2000) })
    act(() => latest().close())
    act(() => { vi.advanceTimersByTime(4000) })
    act(() => latest().send(snapshot(1)))
    act(() => latest().close())
    const before = MockWebSocket.instances.length
    act(() => { vi.advanceTimersByTime(2000) })
    expect(MockWebSocket.instances.length).toBe(before + 1)
  })

  it('reports to the latest callback without reopening the stream', () => {
    const first = vi.fn()
    const second = vi.fn()
    const { rerender } = renderHook(({ cb }) => useTaskEvents(cb), { initialProps: { cb: first } })
    rerender({ cb: second })
    act(() => latest().send(snapshot(1)))
    expect(MockWebSocket.instances).toHaveLength(1)
    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledTimes(1)
  })

  it('closes the stream and stops reconnecting on unmount', () => {
    const { unmount } = renderHook(() => useTaskEvents(() => {}))
    const socket = latest()
    unmount()
    expect(socket.closed).toBe(true)
    act(() => { vi.advanceTimersByTime(60000) })
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('treats an error as a close', () => {
    const { result } = renderHook(() => useTaskEvents(() => {}))
    act(() => latest().onerror?.())
    expect(result.current.status).toBe('offline')
    act(() => { vi.advanceTimersByTime(2000) })
    expect(MockWebSocket.instances).toHaveLength(2)
  })
})
