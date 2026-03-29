import { renderHook, waitFor, act } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { useGitInfo } from './useGitInfo'

describe('useGitInfo', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('returns isGit false initially before fetch resolves', () => {
    window.fetch = vi.fn().mockReturnValue(new Promise(() => {})) // never resolves
    const { result } = renderHook(() => useGitInfo('pane1'))
    expect(result.current.is_git).toBe(false)
    expect(result.current.branch).toBeUndefined()
    expect(result.current.repo).toBeUndefined()
  })

  it('returns isGit false on non-git response', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: false }),
    } as Response)

    const { result } = renderHook(() => useGitInfo('pane1'))
    await waitFor(() => expect(window.fetch).toHaveBeenCalledTimes(1))
    await act(async () => {})
    expect(result.current.is_git).toBe(false)
  })

  it('returns branch and repo when in a git repo', async () => {
    window.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ is_git: true, branch: 'main', repo: 'myrepo' }),
    } as Response)

    const { result } = renderHook(() => useGitInfo('pane1'))
    await waitFor(() => expect(result.current.is_git).toBe(true))
    expect(result.current.branch).toBe('main')
    expect(result.current.repo).toBe('myrepo')
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

  it('registers a 5-second polling interval', () => {
    const intervalSpy = vi.spyOn(window, 'setInterval')
    window.fetch = vi.fn().mockReturnValue(new Promise(() => {}))

    renderHook(() => useGitInfo('pane1'))

    expect(intervalSpy).toHaveBeenCalledWith(expect.any(Function), 5000)
  })

  it('silently ignores fetch errors — state stays at default', async () => {
    let resolveFetch!: () => void
    const fetchPromise = new Promise<Response>((_, reject) => {
      resolveFetch = () => reject(new Error('network error'))
    })
    window.fetch = vi.fn().mockReturnValue(fetchPromise)

    const { result } = renderHook(() => useGitInfo('pane1'))

    await act(async () => {
      resolveFetch()
      await fetchPromise.catch(() => {})
    })

    expect(result.current.is_git).toBe(false)
  })

  it('silently ignores non-ok responses — state stays at default', async () => {
    let resolveFetch!: (r: Response) => void
    const fetchPromise = new Promise<Response>((resolve) => {
      resolveFetch = resolve
    })
    window.fetch = vi.fn().mockReturnValue(fetchPromise)

    const { result } = renderHook(() => useGitInfo('pane1'))

    await act(async () => {
      resolveFetch({ ok: false, status: 404 } as Response)
      await fetchPromise
    })

    expect(result.current.is_git).toBe(false)
  })
})
