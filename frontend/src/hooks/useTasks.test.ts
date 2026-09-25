import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { TASKS_POLL_INTERVAL_MS, useTasks } from './useTasks'

const payload = {
  hosts: [{ name: '', status: 'ok', collected_at: '2026-09-25T12:00:00Z' }],
  tasks: [
    {
      id: 'local:claude:a',
      host: '',
      agent: 'claude',
      state: 'wait',
      location: { kind: 'tmux', tmux_session: 'task-a', attachable: true },
    },
  ],
}

function ok(body: unknown): Response {
  return { ok: true, status: 200, json: () => Promise.resolve(body) } as Response
}

function setVisibility(value: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value })
}

describe('useTasks', () => {
  beforeEach(() => setVisibility('visible'))

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('fetches tasks when enabled and records when it did', async () => {
    window.fetch = vi.fn().mockResolvedValue(ok(payload))

    const { result } = renderHook(() => useTasks(true))

    await waitFor(() => expect(result.current.data?.tasks).toHaveLength(1))
    expect(window.fetch).toHaveBeenCalledWith('/api/tasks')
    expect(result.current.error).toBeNull()
    expect(result.current.updatedAt).not.toBeNull()
  })

  it('does not fetch while disabled', () => {
    window.fetch = vi.fn()
    renderHook(() => useTasks(false))
    expect(window.fetch).not.toHaveBeenCalled()
  })

  it('polls while enabled and visible, and stops when disabled', async () => {
    vi.useFakeTimers()
    window.fetch = vi.fn().mockResolvedValue(ok(payload))

    const { rerender } = renderHook(({ enabled }) => useTasks(enabled), { initialProps: { enabled: true } })
    await act(async () => {})
    expect(window.fetch).toHaveBeenCalledTimes(1)

    await act(async () => {
      vi.advanceTimersByTime(TASKS_POLL_INTERVAL_MS)
    })
    expect(window.fetch).toHaveBeenCalledTimes(2)

    rerender({ enabled: false })
    await act(async () => {
      vi.advanceTimersByTime(TASKS_POLL_INTERVAL_MS * 3)
    })
    expect(window.fetch).toHaveBeenCalledTimes(2)
  })

  it('pauses while the page is hidden', async () => {
    vi.useFakeTimers()
    window.fetch = vi.fn().mockResolvedValue(ok(payload))

    renderHook(() => useTasks(true))
    await act(async () => {})
    setVisibility('hidden')
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    await act(async () => {
      vi.advanceTimersByTime(TASKS_POLL_INTERVAL_MS * 2)
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)

    setVisibility('visible')
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(window.fetch).toHaveBeenCalledTimes(2)
  })

  it('keeps the last result and reports an error when a fetch fails', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(ok(payload))
    window.fetch = fetchMock
    const { result } = renderHook(() => useTasks(true))
    await waitFor(() => expect(result.current.data).not.toBeNull())

    fetchMock.mockResolvedValueOnce({ ok: false, status: 500 } as Response)
    await act(async () => {
      await result.current.refresh()
    })
    expect(result.current.error).toBe('HTTP 500')
    expect(result.current.data?.tasks).toHaveLength(1)

    fetchMock.mockResolvedValueOnce(ok({ hosts: [], tasks: [{ id: 'x' }] }))
    await act(async () => {
      await result.current.refresh()
    })
    expect(result.current.error).toMatch(/Unexpected response/)

    fetchMock.mockRejectedValueOnce(new Error('network down'))
    await act(async () => {
      await result.current.refresh()
    })
    expect(result.current.error).toBe('network down')

    fetchMock.mockResolvedValueOnce(ok(payload))
    await act(async () => {
      await result.current.refresh()
    })
    expect(result.current.error).toBeNull()
  })

  it('does not start a second request while one is in flight', async () => {
    let resolve: (value: Response) => void = () => {}
    window.fetch = vi.fn().mockImplementation(() => new Promise<Response>((r) => { resolve = r }))
    const { result } = renderHook(() => useTasks(true))
    expect(result.current.loading).toBe(true)

    await act(async () => {
      void result.current.refresh()
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)

    await act(async () => {
      resolve(ok(payload))
    })
    expect(result.current.loading).toBe(false)
  })

  it('reconnects a host and then refreshes', async () => {
    const fetchMock = vi.fn().mockResolvedValue(ok(payload))
    window.fetch = fetchMock
    const { result } = renderHook(() => useTasks(true))
    await waitFor(() => expect(result.current.data).not.toBeNull())
    fetchMock.mockClear()

    fetchMock.mockResolvedValueOnce({ ok: true, status: 204 } as Response)
    await act(async () => {
      await result.current.reconnect('gpu box/1')
    })
    expect(fetchMock.mock.calls[0]).toEqual(['/api/tasks/hosts/gpu%20box%2F1/reconnect', { method: 'POST' }])
    expect(fetchMock.mock.calls[1]).toEqual(['/api/tasks'])
  })

  it('reports a failed reconnect', async () => {
    const fetchMock = vi.fn().mockResolvedValue(ok(payload))
    window.fetch = fetchMock
    const { result } = renderHook(() => useTasks(true))
    await waitFor(() => expect(result.current.data).not.toBeNull())

    fetchMock.mockResolvedValueOnce({ ok: false, status: 404 } as Response)
    await act(async () => {
      await result.current.reconnect('gone')
    })
    expect(result.current.error).toBe('Reconnect gone failed: HTTP 404')
  })
})
