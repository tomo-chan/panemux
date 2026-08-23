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

    expect(result.current).toEqual({ token: '', commandCenterEnabled: false, agentBoardEnabled: false })
  })

  it('populates token, commandCenterEnabled, and agentBoardEnabled from a successful response', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ token: 'sekret', command_center_enabled: true, agent_board_enabled: true }),
    } as Response)

    const { result } = renderHook(() => useBoardSessionToken())

    await waitFor(() => {
      expect(result.current).toEqual({ token: 'sekret', commandCenterEnabled: true, agentBoardEnabled: true })
    })
  })

  it('reports agentBoardEnabled false when the server says so independently of commandCenterEnabled', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ token: 'sekret', command_center_enabled: true, agent_board_enabled: false }),
    } as Response)

    const { result } = renderHook(() => useBoardSessionToken())

    await waitFor(() => {
      expect(result.current).toEqual({ token: 'sekret', commandCenterEnabled: true, agentBoardEnabled: false })
    })
  })

  it('stays at defaults when the request fails', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: false, json: async () => ({}) } as Response)

    const { result } = renderHook(() => useBoardSessionToken())

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith('/api/session-token')
    })
    expect(result.current).toEqual({ token: '', commandCenterEnabled: false, agentBoardEnabled: false })
  })

  it('stays at defaults when the response fails schema validation', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ token: 'sekret' }), // missing command_center_enabled and agent_board_enabled
    } as Response)

    const { result } = renderHook(() => useBoardSessionToken())

    await waitFor(() => {
      expect(fetch).toHaveBeenCalled()
    })
    expect(result.current).toEqual({ token: '', commandCenterEnabled: false, agentBoardEnabled: false })
  })
})
