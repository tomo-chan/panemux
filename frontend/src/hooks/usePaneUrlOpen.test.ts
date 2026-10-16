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

describe('usePaneUrlOpen (issue #386)', () => {
  // Issue #386: a localhost URL from a terminal on the panemux host can be
  // shown beside it, whichever way it arrived.
  describe('with a preview', () => {
    it('previews a clicked localhost link instead of opening a tab', async () => {
      const { calls } = mockWindowOpen()
      const fetchMock = mockForwardResponse({ url: 'x', forwarded: false })
      const previewUrl = vi.fn()
      const { result } = renderHook(() => usePaneUrlOpen('pane1', { previewUrl }))

      act(() => result.current.openUrl('http://localhost:5173/'))

      expect(previewUrl).toHaveBeenCalledTimes(1)
      expect(previewUrl).toHaveBeenCalledWith('http://localhost:5173/')
      expect(calls).toEqual([])
      expect(fetchMock).not.toHaveBeenCalled()

      // An external link still opens in a tab, with its forward.
      act(() => result.current.openUrl('https://example.com/'))
      expect(previewUrl).toHaveBeenCalledTimes(1)
      expect(calls).toEqual(['https://example.com/'])
      await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    })

    it('offers Preview for a requested localhost URL, and previews it once', () => {
      const { calls } = mockWindowOpen()
      const previewUrl = vi.fn()
      const { result } = renderHook(() => usePaneUrlOpen('pane1', { previewUrl }))

      act(() => result.current.requestOpen('http://localhost:5173/'))
      expect(result.current.canPreview).toBe(true)

      act(() => {
        result.current.previewPendingOpen()
        result.current.previewPendingOpen()
        result.current.confirmPendingOpen()
      })

      expect(previewUrl).toHaveBeenCalledTimes(1)
      expect(calls).toEqual([])
      expect(result.current.pendingUrl).toBeNull()
    })

    it('keeps Open for a requested localhost URL opening a tab and preparing the forward', async () => {
      const { calls, opened } = mockWindowOpen()
      const fetchMock = mockForwardResponse({ url: 'http://localhost:8085/cb', forwarded: false })
      const previewUrl = vi.fn()
      const { result } = renderHook(() => usePaneUrlOpen('pane1', { previewUrl }))

      act(() => result.current.requestOpen('http://localhost:8085/cb'))
      expect(result.current.canPreview).toBe(true)
      act(() => result.current.confirmPendingOpen())

      expect(previewUrl).not.toHaveBeenCalled()
      expect(calls).toEqual(['about:blank'])
      await waitFor(() => expect(opened[0].location.href).toBe('http://localhost:8085/cb'))
      expect(fetchMock).toHaveBeenCalledTimes(1)
    })

    it('offers no Preview for an external URL, and previews nothing for it', () => {
      mockWindowOpen()
      const previewUrl = vi.fn()
      const { result } = renderHook(() => usePaneUrlOpen('pane1', { previewUrl }))

      act(() => result.current.requestOpen('https://example.com/auth'))
      expect(result.current.canPreview).toBe(false)
      act(() => result.current.previewPendingOpen())

      expect(previewUrl).not.toHaveBeenCalled()
      expect(result.current.pendingUrl).toBe('https://example.com/auth')
    })

    it('offers no Preview without a preview', () => {
      const { result } = renderHook(() => usePaneUrlOpen('pane1'))
      act(() => result.current.requestOpen('http://localhost:5173/'))
      expect(result.current.canPreview).toBe(false)
    })

    it('drops a pending request for the URL a click has just opened', () => {
      mockWindowOpen()
      const previewUrl = vi.fn()
      const { result } = renderHook(() => usePaneUrlOpen('pane1', { previewUrl }))

      act(() => result.current.requestOpen('http://localhost:5173/'))
      act(() => result.current.openUrl('http://localhost:5173/'))
      expect(result.current.pendingUrl).toBeNull()
      act(() => result.current.previewPendingOpen())

      expect(previewUrl).toHaveBeenCalledTimes(1)
    })
  })

  it('opens a confirmed request once however often Open is pressed before it hides', async () => {
    const { calls } = mockWindowOpen()
    const fetchMock = mockForwardResponse({ url: 'https://example.com/auth', forwarded: true, port: 8085 })
    const { result } = renderHook(() => usePaneUrlOpen('pane1'))

    act(() => result.current.requestOpen('https://example.com/auth'))
    act(() => {
      result.current.confirmPendingOpen()
      result.current.confirmPendingOpen()
    })

    expect(calls).toEqual(['https://example.com/auth'])
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
  })

  it('records whether a program or a link asked', () => {
    const { result } = renderHook(() => usePaneUrlOpen('pane1'))

    act(() => result.current.requestOpen('https://example.com/a'))
    expect(result.current.pendingSource).toBe('program')
    act(() => result.current.requestOpen('https://example.com/b', 'link'))
    expect(result.current.pendingSource).toBe('link')
    act(() => result.current.dismissPendingOpen())
    expect(result.current.pendingSource).toBeNull()
  })

  // The Type in pane popup's attach is not a pane: no forward is prepared for it.
  it('opens a tab without asking for a forward when forwarding is off', () => {
    const { calls } = mockWindowOpen()
    const fetchMock = mockForwardResponse({ url: 'x', forwarded: false })
    const { result } = renderHook(() => usePaneUrlOpen('attach-1', { forward: false }))

    act(() => result.current.openUrl('http://localhost:8085/cb'))
    act(() => result.current.openUrl('https://example.com/'))

    expect(calls).toEqual(['http://localhost:8085/cb', 'https://example.com/'])
    expect(fetchMock).not.toHaveBeenCalled()
  })
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
