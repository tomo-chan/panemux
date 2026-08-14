import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useBoardStatus } from './useBoardStatus'

function statusResponse(statuses: Record<string, unknown> = {}) {
  return { ok: true, json: () => Promise.resolve({ statuses }) } as Response
}

function messagesResponse(messages: unknown[] = []) {
  return { ok: true, json: () => Promise.resolve({ messages }) } as Response
}

function fetchRouter(handlers: { status?: () => Response | Promise<Response>; messages?: (since: string) => Response | Promise<Response> }) {
  return vi.fn((input: RequestInfo | URL, _init?: RequestInit) => {
    const url = String(input)
    if (url.startsWith('/api/board/status')) {
      return Promise.resolve(handlers.status ? handlers.status() : statusResponse())
    }
    if (url.startsWith('/api/board/messages')) {
      const since = new URL(url, 'http://localhost').searchParams.get('since') ?? '0'
      return Promise.resolve(handlers.messages ? handlers.messages(since) : messagesResponse())
    }
    throw new Error(`unexpected fetch: ${url}`)
  })
}

describe('useBoardStatus', () => {
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

  it('does not fetch while disabled', () => {
    window.fetch = vi.fn()

    renderHook(() => useBoardStatus({ enabled: false, token: 'tok' }))

    expect(window.fetch).not.toHaveBeenCalled()
  })

  it('does not fetch while the token is empty', () => {
    window.fetch = vi.fn()

    renderHook(() => useBoardStatus({ enabled: true, token: '' }))

    expect(window.fetch).not.toHaveBeenCalled()
  })

  it('populates statuses and messages from the initial fetch', async () => {
    window.fetch = fetchRouter({
      status: () => statusResponse({ main: { updated_at: '2026-08-14T12:00:00Z', state: 'working' } }),
      messages: () => messagesResponse([
        { at: '2026-08-14T12:00:00Z', host: 'devbox', team: 'panemux', from: 'a', to: 'b', body: 'hi', seq: 1 },
      ]),
    })

    const { result } = renderHook(() => useBoardStatus({ enabled: true, token: 'tok' }))

    await waitFor(() => {
      expect(result.current.statuses.main?.state).toBe('working')
    })
    expect(result.current.messages).toHaveLength(1)
    expect(result.current.messages[0].body).toBe('hi')
  })

  it('sends Authorization: Bearer <token> on both requests', async () => {
    const fetchMock = fetchRouter({})
    window.fetch = fetchMock

    renderHook(() => useBoardStatus({ enabled: true, token: 'sekret' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    for (const call of fetchMock.mock.calls) {
      const init = call[1] as RequestInit
      expect(init.headers).toEqual({ Authorization: 'Bearer sekret' })
    }
  })

  it('fetches messages with ?since=0 on the first poll', async () => {
    const fetchMock = fetchRouter({})
    window.fetch = fetchMock

    renderHook(() => useBoardStatus({ enabled: true, token: 'tok' }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/board/messages?since=0', expect.anything())
    })
  })

  it('polls again after 5 seconds using the max seq seen so far', async () => {
    vi.useFakeTimers()
    const fetchMock = fetchRouter({
      messages: (since) => {
        if (since === '0') {
          return messagesResponse([
            { at: '2026-08-14T12:00:00Z', host: 'devbox', team: 'panemux', from: 'a', to: 'b', body: 'one', seq: 5 },
          ])
        }
        return messagesResponse([])
      },
    })
    window.fetch = fetchMock

    renderHook(() => useBoardStatus({ enabled: true, token: 'tok' }))
    await act(async () => {})

    act(() => {
      vi.advanceTimersByTime(5000)
    })
    await act(async () => {})

    expect(fetchMock).toHaveBeenCalledWith('/api/board/messages?since=5', expect.anything())
  })

  it('merges incremental messages without dropping earlier ones', async () => {
    vi.useFakeTimers()
    const fetchMock = fetchRouter({
      messages: (since) => {
        if (since === '0') {
          return messagesResponse([
            { at: '2026-08-14T12:00:00Z', host: 'devbox', team: 'panemux', from: 'a', to: 'b', body: 'one', seq: 1 },
          ])
        }
        if (since === '1') {
          return messagesResponse([
            { at: '2026-08-14T12:00:05Z', host: 'devbox', team: 'panemux', from: 'a', to: 'b', body: 'two', seq: 2 },
          ])
        }
        return messagesResponse([])
      },
    })
    window.fetch = fetchMock

    const { result } = renderHook(() => useBoardStatus({ enabled: true, token: 'tok' }))
    await act(async () => {})
    expect(result.current.messages).toHaveLength(1)

    act(() => {
      vi.advanceTimersByTime(5000)
    })
    await act(async () => {})
    expect(result.current.messages).toHaveLength(2)

    expect(result.current.messages.map((m) => m.body)).toEqual(['one', 'two'])
  })

  it('caps accumulated messages at 500, dropping the oldest', async () => {
    vi.useFakeTimers()
    const firstBatch = Array.from({ length: 400 }, (_, i) => ({
      at: '2026-08-14T12:00:00Z', host: 'devbox', team: 'panemux', from: 'a', to: 'b', body: `m${i}`, seq: i + 1,
    }))
    const secondBatch = Array.from({ length: 400 }, (_, i) => ({
      at: '2026-08-14T12:00:05Z', host: 'devbox', team: 'panemux', from: 'a', to: 'b', body: `n${i}`, seq: 401 + i,
    }))
    const fetchMock = fetchRouter({
      messages: (since) => {
        if (since === '0') return messagesResponse(firstBatch)
        if (since === '400') return messagesResponse(secondBatch)
        return messagesResponse([])
      },
    })
    window.fetch = fetchMock

    const { result } = renderHook(() => useBoardStatus({ enabled: true, token: 'tok' }))
    await act(async () => {})
    expect(result.current.messages).toHaveLength(400)

    act(() => {
      vi.advanceTimersByTime(5000)
    })
    await act(async () => {})
    expect(result.current.messages).toHaveLength(500)

    expect(result.current.messages[0].body).toBe('m300')
    expect(result.current.messages[499].body).toBe('n399')
  })

  it('excludes _system board_status rows from the message feed but keeps other _system rows', async () => {
    window.fetch = fetchRouter({
      messages: () => messagesResponse([
        { at: '2026-08-14T12:00:00Z', host: 'devbox', team: 'panemux', from: 'main', to: '_system', body: JSON.stringify({ kind: 'board_status', state: 'working' }), seq: 1 },
        { at: '2026-08-14T12:00:01Z', host: 'devbox', team: 'panemux', from: 'main', to: '_system', body: 'not a status report', seq: 2 },
        { at: '2026-08-14T12:00:02Z', host: 'devbox', team: 'panemux', from: 'main', to: 'other-pane', body: 'ordinary message', seq: 3 },
      ]),
    })

    const { result } = renderHook(() => useBoardStatus({ enabled: true, token: 'tok' }))

    await waitFor(() => expect(result.current.messages).toHaveLength(2))
    expect(result.current.messages.map((m) => m.body)).toEqual(['not a status report', 'ordinary message'])
  })

  it('sets a not-authorized error on 401', async () => {
    window.fetch = fetchRouter({
      status: () => ({ ok: false, status: 401 } as Response),
    })

    const { result } = renderHook(() => useBoardStatus({ enabled: true, token: 'tok' }))

    await waitFor(() => {
      expect(result.current.error).toBe('Not authorized to view the agent board.')
    })
  })

  it('sets a generic status-code error on other failures', async () => {
    window.fetch = fetchRouter({
      status: () => ({ ok: false, status: 500 } as Response),
    })

    const { result } = renderHook(() => useBoardStatus({ enabled: true, token: 'tok' }))

    await waitFor(() => {
      expect(result.current.error).toBe('Failed to load board status (500).')
    })
  })

  it('sets an error and preserves prior state when the response fails schema validation', async () => {
    vi.useFakeTimers()
    window.fetch = fetchRouter({
      status: () => statusResponse({ main: { updated_at: '2026-08-14T12:00:00Z', state: 'working' } }),
    })

    const { result } = renderHook(() => useBoardStatus({ enabled: true, token: 'tok' }))
    await act(async () => {})
    expect(result.current.statuses.main?.state).toBe('working')

    window.fetch = fetchRouter({
      status: () => ({ ok: true, json: () => Promise.resolve({ statuses: 'not-a-map' }) } as Response),
    })
    // Re-render is not needed: advance the existing interval instead so the
    // hook's own poll loop (not a fresh mount) performs the failing fetch.
    act(() => {
      vi.advanceTimersByTime(5000)
    })
    await act(async () => {})

    expect(result.current.error).not.toBeNull()
    expect(result.current.statuses.main?.state).toBe('working')
  })

  it('stops polling while the tab is hidden', async () => {
    vi.useFakeTimers()
    const fetchMock = fetchRouter({})
    window.fetch = fetchMock

    renderHook(() => useBoardStatus({ enabled: true, token: 'tok' }))
    await act(async () => {})
    const callsWhileVisible = fetchMock.mock.calls.length

    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })

    act(() => {
      vi.advanceTimersByTime(20000)
    })
    await act(async () => {})

    expect(fetchMock.mock.calls.length).toBe(callsWhileVisible)
  })

  it('does not update state after unmount', async () => {
    let resolveStatus: ((value: Response) => void) | undefined
    window.fetch = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.startsWith('/api/board/status')) {
        return new Promise<Response>((resolve) => {
          resolveStatus = resolve
        })
      }
      return Promise.resolve(messagesResponse())
    })
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    const { unmount } = renderHook(() => useBoardStatus({ enabled: true, token: 'tok' }))
    unmount()

    await act(async () => {
      resolveStatus?.(statusResponse({ main: { updated_at: '2026-08-14T12:00:00Z' } }))
      await Promise.resolve()
    })

    expect(consoleErrorSpy).not.toHaveBeenCalledWith(expect.stringContaining('act('))
    consoleErrorSpy.mockRestore()
  })
})
