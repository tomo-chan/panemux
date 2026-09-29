import { StrictMode } from 'react'
import { renderHook, act, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { usePaneUrlOpen } from './usePaneUrlOpen'

interface FakeWindow {
  location: { href: string }
  close: ReturnType<typeof vi.fn>
  opener: unknown
}

function mockWindowOpen(): { calls: string[]; opened: FakeWindow[] } {
  const calls: string[] = []
  const opened: FakeWindow[] = []
  window.open = vi.fn((url?: string | URL) => {
    calls.push(String(url ?? ''))
    const win: FakeWindow = { location: { href: '' }, close: vi.fn(), opener: {} }
    opened.push(win)
    return win as unknown as Window
  }) as unknown as typeof window.open
  return { calls, opened }
}

function mockForwardResponse(body: Record<string, unknown>) {
  const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve(body) } as Response)
  window.fetch = fetchMock
  return fetchMock
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('usePaneUrlOpen', () => {
  it('opens a remote URL immediately and asks the backend to forward its callback port', async () => {
    const { calls } = mockWindowOpen()
    const fetchMock = mockForwardResponse({ url: 'https://example.com/auth', forwarded: true, port: 8085 })
    const { result } = renderHook(() => usePaneUrlOpen('pane1'))

    act(() => result.current.openUrl('https://example.com/auth'))

    expect(calls).toEqual(['https://example.com/auth'])
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    expect(result.current.error).toBeNull()
  })

  it('opens a blank tab first for a loopback URL and navigates once the forward is up', async () => {
    const { calls, opened } = mockWindowOpen()
    mockForwardResponse({ url: 'http://localhost:8085/cb', forwarded: true, port: 8085 })
    const { result } = renderHook(() => usePaneUrlOpen('pane1'))

    act(() => result.current.openUrl('http://localhost:8085/cb'))

    expect(calls).toEqual(['about:blank'])
    await waitFor(() => expect(opened[0].location.href).toBe('http://localhost:8085/cb'))
    expect(opened[0].close).not.toHaveBeenCalled()
  })

  it('closes the blank tab and reports the error when a loopback forward fails', async () => {
    const { opened } = mockWindowOpen()
    window.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      text: () => Promise.resolve('loopback port unavailable: cannot bind 127.0.0.1:8085'),
    } as Response)
    const { result } = renderHook(() => usePaneUrlOpen('pane1'))

    act(() => result.current.openUrl('http://localhost:8085/cb'))

    await waitFor(() => expect(result.current.error).toContain('127.0.0.1:8085'))
    expect(opened[0].close).toHaveBeenCalled()
    expect(opened[0].location.href).toBe('')
  })

  it('keeps a remote URL open but still reports a failed forward', async () => {
    const { calls } = mockWindowOpen()
    window.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      text: () => Promise.resolve('loopback port unavailable'),
    } as Response)
    const { result } = renderHook(() => usePaneUrlOpen('pane1'))

    act(() => result.current.openUrl('https://example.com/auth?redirect_uri=http://localhost:8085/cb'))

    await waitFor(() => expect(result.current.error).toBe('loopback port unavailable'))
    expect(calls).toEqual(['https://example.com/auth?redirect_uri=http://localhost:8085/cb'])
  })

  it('ignores URLs it must never open', () => {
    const { calls } = mockWindowOpen()
    const fetchMock = mockForwardResponse({ url: 'x', forwarded: false })
    const { result } = renderHook(() => usePaneUrlOpen('pane1'))

    act(() => result.current.openUrl('javascript:alert(1)'))
    act(() => result.current.openUrl('file:///etc/passwd'))

    expect(calls).toEqual([])
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('holds a pane-initiated request until it is confirmed', async () => {
    const { calls } = mockWindowOpen()
    const fetchMock = mockForwardResponse({ url: 'https://example.com/auth', forwarded: true, port: 8085 })
    const { result } = renderHook(() => usePaneUrlOpen('pane1'))

    act(() => result.current.requestOpen('https://example.com/auth'))

    expect(result.current.pendingUrl).toBe('https://example.com/auth')
    expect(calls).toEqual([])

    act(() => result.current.confirmPendingOpen())

    expect(calls).toEqual(['https://example.com/auth'])
    expect(result.current.pendingUrl).toBeNull()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
  })

  // React re-runs state updater functions (StrictMode does so on every
  // update), so approving a request must not open the tab from inside one.
  it('opens a confirmed request exactly once under StrictMode', async () => {
    const { calls } = mockWindowOpen()
    const fetchMock = mockForwardResponse({ url: 'https://example.com/auth', forwarded: true, port: 8085 })
    const { result } = renderHook(() => usePaneUrlOpen('pane1'), { wrapper: StrictMode })

    act(() => result.current.requestOpen('https://example.com/auth'))
    act(() => result.current.confirmPendingOpen())

    expect(calls).toEqual(['https://example.com/auth'])
    expect(result.current.pendingUrl).toBeNull()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
  })

  it('drops a pane-initiated request when it is dismissed', () => {
    const { calls } = mockWindowOpen()
    const fetchMock = mockForwardResponse({ url: 'x', forwarded: false })
    const { result } = renderHook(() => usePaneUrlOpen('pane1'))

    act(() => result.current.requestOpen('https://example.com/auth'))
    act(() => result.current.dismissPendingOpen())

    expect(result.current.pendingUrl).toBeNull()
    expect(calls).toEqual([])
    expect(fetchMock).not.toHaveBeenCalled()

    act(() => result.current.confirmPendingOpen())
    expect(calls).toEqual([])
  })

  it('ignores a pane-initiated request for a URL it must never open', () => {
    const { result } = renderHook(() => usePaneUrlOpen('pane1'))

    act(() => result.current.requestOpen('file:///etc/passwd'))

    expect(result.current.pendingUrl).toBeNull()
  })

  it('replaces an earlier pending request with the newest one', () => {
    const { result } = renderHook(() => usePaneUrlOpen('pane1'))

    act(() => result.current.requestOpen('https://example.com/first'))
    act(() => result.current.requestOpen('https://example.com/second'))

    expect(result.current.pendingUrl).toBe('https://example.com/second')
  })

  it('lets a dismissed error be cleared and a later open start clean', async () => {
    mockWindowOpen()
    window.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      text: () => Promise.resolve('loopback port unavailable'),
    } as Response)
    const { result } = renderHook(() => usePaneUrlOpen('pane1'))

    act(() => result.current.openUrl('https://example.com/auth'))
    await waitFor(() => expect(result.current.error).toBe('loopback port unavailable'))

    act(() => result.current.dismissError())
    expect(result.current.error).toBeNull()

    mockForwardResponse({ url: 'https://example.com/auth', forwarded: true, port: 8085 })
    act(() => result.current.openUrl('https://example.com/auth'))
    await waitFor(() => expect(result.current.error).toBeNull())
  })
})
