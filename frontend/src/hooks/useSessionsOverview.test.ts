import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useSessionsOverview } from './useSessionsOverview'

describe('useSessionsOverview', () => {
  beforeEach(() => {
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'visible',
    })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('fetches sessions and indexes them by id', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([
        { id: 'main', type: 'local', title: 'Main', state: 'connected' },
        { id: 'ops', type: 'ssh', title: 'Ops', state: 'disconnected' },
      ]),
    } as Response)

    const { result } = renderHook(() => useSessionsOverview())

    await waitFor(() => expect(result.current.main?.state).toBe('connected'))
    expect(result.current.ops?.title).toBe('Ops')
  })

  it('does not fetch while disabled', () => {
    window.fetch = vi.fn()

    renderHook(() => useSessionsOverview(false))

    expect(window.fetch).not.toHaveBeenCalled()
  })

  it('polls every 10 seconds while visible', async () => {
    vi.useFakeTimers()
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([{ id: 'main', type: 'local', title: 'Main', state: 'connected' }]),
    } as Response)

    renderHook(() => useSessionsOverview())
    await act(async () => {})

    expect(window.fetch).toHaveBeenCalledTimes(1)

    act(() => {
      vi.advanceTimersByTime(10000)
    })

    await act(async () => {})
    expect(window.fetch).toHaveBeenCalledTimes(2)
  })

  it('stops polling while hidden and refetches after visibility returns', async () => {
    vi.useFakeTimers()
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([{ id: 'main', type: 'local', title: 'Main', state: 'connected' }]),
    } as Response)

    renderHook(() => useSessionsOverview())
    await act(async () => {})

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'hidden',
    })
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })

    act(() => {
      vi.advanceTimersByTime(10000)
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'visible',
    })
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })

    await act(async () => {})
    expect(window.fetch).toHaveBeenCalledTimes(2)
  })

  it('keeps the previous state when the response is not ok', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve([{ id: 'main', type: 'local', title: 'Main', state: 'connected' }]),
      } as Response)
      .mockResolvedValueOnce({ ok: false, status: 500 } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useSessionsOverview())
    await waitFor(() => expect(result.current.main?.state).toBe('connected'))

    await act(async () => {
      await fetchMock.mock.results[0]?.value
    })

    await act(async () => {
      const refetch = fetchMock.mock.calls.length
      await (fetchMock as ReturnType<typeof vi.fn>).mock.results[refetch - 1]?.value
    })

    expect(result.current.main?.state).toBe('connected')
  })
})
