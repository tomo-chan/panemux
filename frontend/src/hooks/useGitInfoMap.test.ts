import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useGitInfoMap } from './useGitInfoMap'

describe('useGitInfoMap', () => {
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

  it('fetches git metadata for multiple pane ids', async () => {
    window.fetch = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/main/git-info')) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ is_git: true, repo: 'panemux', branch: 'main' }),
        } as Response)
      }
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ is_git: false }),
      } as Response)
    })

    const { result } = renderHook(() => useGitInfoMap(['main', 'ops']))

    await waitFor(() => expect(result.current.main?.repo).toBe('panemux'))
    expect(result.current.ops?.is_git).toBe(false)
  })

  it('clears stale pane ids after rerender', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: true, repo: 'panemux', branch: 'main' }),
    } as Response)

    const { result, rerender } = renderHook(({ ids }) => useGitInfoMap(ids), {
      initialProps: { ids: ['main', 'ops'] },
    })

    await waitFor(() => expect(result.current.main?.repo).toBe('panemux'))

    rerender({ ids: ['main'] })

    await waitFor(() => expect(result.current.ops).toBeUndefined())
    expect(result.current.main?.repo).toBe('panemux')
  })

  it('does not fetch while disabled', () => {
    window.fetch = vi.fn()

    renderHook(() => useGitInfoMap(['main'], false))

    expect(window.fetch).not.toHaveBeenCalled()
  })

  it('polls every 10 seconds while visible', async () => {
    vi.useFakeTimers()
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: false }),
    } as Response)

    renderHook(() => useGitInfoMap(['main']))

    expect(window.fetch).toHaveBeenCalledTimes(1)

    act(() => {
      vi.advanceTimersByTime(10000)
    })

    expect(window.fetch).toHaveBeenCalledTimes(2)
  })
})
