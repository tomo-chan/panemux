import { renderHook, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useBoardSessionToken } from './useBoardSessionToken'

describe('useBoardSessionToken', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns default (empty token, disabled) before the fetch resolves', () => {
    vi.mocked(fetch).mockReturnValue(new Promise(() => {}))

    const { result } = renderHook(() => useBoardSessionToken())

    expect(result.current).toEqual({ token: '', commandCenterEnabled: false })
  })

  it('populates token and commandCenterEnabled from a successful response', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ token: 'sekret', command_center_enabled: true }),
    } as Response)

    const { result } = renderHook(() => useBoardSessionToken())

    await waitFor(() => {
      expect(result.current).toEqual({ token: 'sekret', commandCenterEnabled: true })
    })
  })

  it('stays at defaults when the request fails', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: false, json: async () => ({}) } as Response)

    const { result } = renderHook(() => useBoardSessionToken())

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith('/api/session-token')
    })
    expect(result.current).toEqual({ token: '', commandCenterEnabled: false })
  })

  it('stays at defaults when the response fails schema validation', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ token: 'sekret' }), // missing command_center_enabled
    } as Response)

    const { result } = renderHook(() => useBoardSessionToken())

    await waitFor(() => {
      expect(fetch).toHaveBeenCalled()
    })
    expect(result.current).toEqual({ token: '', commandCenterEnabled: false })
  })
})
