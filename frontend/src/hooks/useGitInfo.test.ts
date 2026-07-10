import { renderHook, waitFor, act } from '@testing-library/react'
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { __resetGitInfoCacheForTests, useGitInfo, useGitInfoSnapshotMap } from './useGitInfo'

describe('useGitInfo', () => {
  beforeEach(() => {
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'visible',
    })
  })

  afterEach(() => {
    __resetGitInfoCacheForTests()
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('returns isGit false initially before fetch resolves', () => {
    window.fetch = vi.fn().mockReturnValue(new Promise(() => {}))
    const { result } = renderHook(() => useGitInfo('pane1'))
    expect(result.current.gitInfo.is_git).toBe(false)
    expect(result.current.gitInfo.branch).toBeUndefined()
    expect(result.current.gitInfo.repo).toBeUndefined()
  })

  it('returns branch and repo when in a git repo', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: true, branch: 'main', repo: 'myrepo', pr_number: 1, pr_url: 'https://github.com/example/repo/pull/1' }),
    } as Response)

    const { result } = renderHook(() => useGitInfo('pane1'))
    await waitFor(() => expect(result.current.gitInfo.is_git).toBe(true))
    expect(result.current.gitInfo.branch).toBe('main')
    expect(result.current.gitInfo.repo).toBe('myrepo')
    expect(result.current.gitInfo.pr_number).toBe(1)
    expect(result.current.gitInfo.pr_url).toBe('https://github.com/example/repo/pull/1')
  })

  it('calls /api/sessions/{id}/git-info with the correct session id', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: false }),
    } as Response)

    renderHook(() => useGitInfo('my-session-id'))
    await waitFor(() => expect(window.fetch).toHaveBeenCalledTimes(1))
    expect(window.fetch).toHaveBeenCalledWith('/api/sessions/my-session-id/git-info')
  })

  it('does not fetch while disabled', () => {
    window.fetch = vi.fn()

    renderHook(() => useGitInfo('pane1', false))

    expect(window.fetch).not.toHaveBeenCalled()
  })

  it('fetches immediately when re-enabled', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: false }),
    } as Response)

    const { rerender } = renderHook(({ enabled }) => useGitInfo('pane1', enabled), {
      initialProps: { enabled: false },
    })

    expect(window.fetch).not.toHaveBeenCalled()

    rerender({ enabled: true })

    await waitFor(() => expect(window.fetch).toHaveBeenCalledTimes(1))
  })

  it('does not refetch on visibility restore while data is still fresh', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: false }),
    } as Response)

    renderHook(() => useGitInfo('pane1'))
    await waitFor(() => expect(window.fetch).toHaveBeenCalledTimes(1))

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'hidden',
    })
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'visible',
    })
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })

    expect(window.fetch).toHaveBeenCalledTimes(1)
  })

  it('refetches on visibility restore after the cached data becomes stale', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-21T00:00:00Z'))
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: false }),
    } as Response)

    renderHook(() => useGitInfo('pane1'))
    await act(async () => {
      await Promise.resolve()
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)

    // Go hidden immediately so the steady-state poll (which only runs while
    // the tab is visible) cannot itself refresh the cache in the background;
    // this isolates the visibility-restore refresh path from polling.
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'hidden',
    })
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })

    act(() => {
      vi.advanceTimersByTime(10001)
    })

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'visible',
    })
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })

    await act(async () => {
      await Promise.resolve()
    })
    expect(window.fetch).toHaveBeenCalledTimes(2)
  })

  it('throttles stale checks triggered in quick succession', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-21T00:00:00Z'))
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: false }),
    } as Response)

    // Disabled so the steady-state poll doesn't run; this test targets
    // refreshIfStale's own throttle in isolation.
    const { result } = renderHook(() => useGitInfo('pane1', false))
    await act(async () => {
      await result.current.refreshIfStale()
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)

    act(() => {
      vi.advanceTimersByTime(10001)
    })

    await act(async () => {
      await result.current.refreshIfStale()
      await result.current.refreshIfStale()
    })

    expect(window.fetch).toHaveBeenCalledTimes(2)
  })

  it('refreshes immediately when refreshNow is called', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: false }),
    } as Response)

    const { result } = renderHook(() => useGitInfo('pane1'))
    await waitFor(() => expect(window.fetch).toHaveBeenCalledTimes(1))

    await act(async () => {
      await result.current.refreshNow()
    })

    expect(window.fetch).toHaveBeenCalledTimes(2)
  })

  it('reuses the in-flight request when refreshNow is called twice before the first fetch settles', async () => {
    let resolveFetch!: (value: Response) => void
    const fetchPromise = new Promise<Response>((resolve) => {
      resolveFetch = resolve
    })
    window.fetch = vi.fn().mockReturnValue(fetchPromise)

    const { result } = renderHook(() => useGitInfo('pane1', false))

    const firstCall = result.current.refreshNow()
    const secondCall = result.current.refreshNow()

    expect(window.fetch).toHaveBeenCalledTimes(1)

    await act(async () => {
      resolveFetch({
        ok: true,
        json: () => Promise.resolve({ is_git: false }),
      } as Response)
      await Promise.all([firstCall, secondCall])
    })

    expect(window.fetch).toHaveBeenCalledTimes(1)
  })

  it('throttles repeated stale checks after a non-ok response', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-21T00:00:00Z'))
    window.fetch = vi.fn().mockResolvedValue({ ok: false, status: 404 } as Response)

    const { result } = renderHook(() => useGitInfo('pane1', false))

    await act(async () => {
      await result.current.refreshIfStale()
      await result.current.refreshIfStale()
    })

    expect(window.fetch).toHaveBeenCalledTimes(1)
    expect(result.current.gitInfo.is_git).toBe(false)
  })

  it('silently ignores fetch errors', async () => {
    let rejectFetch!: (error: Error) => void
    const fetchPromise = new Promise<Response>((_, reject) => {
      rejectFetch = reject
    })
    window.fetch = vi.fn().mockReturnValue(fetchPromise)

    const { result } = renderHook(() => useGitInfo('pane1'))

    await act(async () => {
      rejectFetch(new Error('network error'))
      await fetchPromise.catch(() => {})
    })

    expect(result.current.gitInfo.is_git).toBe(false)
  })

  it('polls automatically every 10 seconds while the tab and pane remain visible, with no interaction required', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-21T00:00:00Z'))
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: false }),
    } as Response)

    renderHook(() => useGitInfo('pane1'))
    await act(async () => {
      await Promise.resolve()
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)

    await act(async () => {
      vi.advanceTimersByTime(10000)
      await Promise.resolve()
    })
    expect(window.fetch).toHaveBeenCalledTimes(2)

    await act(async () => {
      vi.advanceTimersByTime(10000)
      await Promise.resolve()
    })
    expect(window.fetch).toHaveBeenCalledTimes(3)
  })

  it('stops polling once the pane becomes disabled', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-21T00:00:00Z'))
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: false }),
    } as Response)

    const { rerender } = renderHook(({ enabled }) => useGitInfo('pane1', enabled), {
      initialProps: { enabled: true },
    })
    await act(async () => {
      await Promise.resolve()
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)

    rerender({ enabled: false })

    await act(async () => {
      vi.advanceTimersByTime(30000)
      await Promise.resolve()
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)
  })

  it('stops polling while the browser tab is hidden and resumes once visible again', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-21T00:00:00Z'))
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: false }),
    } as Response)

    renderHook(() => useGitInfo('pane1'))
    await act(async () => {
      await Promise.resolve()
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'hidden',
    })
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })

    await act(async () => {
      vi.advanceTimersByTime(30000)
      await Promise.resolve()
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value: 'visible',
    })
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
      await Promise.resolve()
    })

    await act(async () => {
      vi.advanceTimersByTime(10000)
      await Promise.resolve()
    })
    expect(window.fetch).not.toHaveBeenCalledTimes(1)
  })

  it('shares cached git info with snapshot consumers without triggering extra fetches', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: true, branch: 'main', repo: 'panemux', pr_number: 42 }),
    } as Response)

    const snapshot = renderHook(() => useGitInfoSnapshotMap(['pane1']))
    expect(snapshot.result.current.pane1?.is_git).toBe(false)

    renderHook(() => useGitInfo('pane1'))

    await waitFor(() => expect(snapshot.result.current.pane1?.repo).toBe('panemux'))
    expect(snapshot.result.current.pane1?.branch).toBe('main')
    expect(snapshot.result.current.pane1?.pr_number).toBe(42)
    expect(window.fetch).toHaveBeenCalledTimes(1)
  })
})
