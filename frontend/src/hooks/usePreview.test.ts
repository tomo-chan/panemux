import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { usePreview } from './usePreview'

afterEach(() => {
  vi.restoreAllMocks()
})

function respond(...bodies: Array<{ status: number; body: unknown }>) {
  const fetchMock = vi.spyOn(globalThis, 'fetch')
  for (const { status, body } of bodies) {
    fetchMock.mockResolvedValueOnce(
      new Response(typeof body === 'string' ? body : JSON.stringify(body), { status }),
    )
  }
  return fetchMock
}

const ready = { status: 'ready', url: '/preview/ab/index.html', path: '/w/index.html', host: '' }

describe('usePreview', () => {
  it('starts closed', () => {
    const { result } = renderHook(() => usePreview('pane-1', true))
    expect(result.current.view).toBeNull()
  })

  it('shows loading, then the file', async () => {
    respond({ status: 200, body: ready })
    const { result } = renderHook(() => usePreview('pane-1', true))

    act(() => result.current.open('./index.html'))
    expect(result.current.view).toEqual({ kind: 'loading', request: './index.html' })

    await waitFor(() => expect(result.current.view?.kind).toBe('ready'))
    expect(result.current.view).toEqual({
      kind: 'ready', url: '/preview/ab/index.html', path: '/w/index.html', host: '', generation: 0,
    })
  })

  it('offers the candidates, and opens the chosen one', async () => {
    const fetchMock = respond(
      { status: 200, body: { status: 'choose', candidates: ['/a/x.md', '/b/x.md'], host: '' } },
      { status: 200, body: { ...ready, path: '/b/x.md', url: '/preview/cd/x.md' } },
    )
    const { result } = renderHook(() => usePreview('pane-1', true))

    act(() => result.current.open('x.md'))
    await waitFor(() => expect(result.current.view?.kind).toBe('choose'))
    expect(result.current.view).toEqual({ kind: 'choose', request: 'x.md', candidates: ['/a/x.md', '/b/x.md'], host: '' })

    act(() => result.current.open('/b/x.md'))
    await waitFor(() => expect(result.current.view?.kind).toBe('ready'))
    expect(fetchMock).toHaveBeenLastCalledWith('/api/sessions/pane-1/preview', expect.objectContaining({
      body: JSON.stringify({ path: '/b/x.md' }),
    }))
  })

  it("shows the server's reason", async () => {
    respond({ status: 404, body: 'file not found: x.md (looked in /w)' })
    const { result } = renderHook(() => usePreview('pane-1', true))

    act(() => result.current.open('x.md'))
    await waitFor(() => expect(result.current.view?.kind).toBe('error'))
    expect(result.current.view).toEqual({ kind: 'error', request: 'x.md', message: 'file not found: x.md (looked in /w)' })
  })

  it('keeps only the latest click', async () => {
    let resolveFirst: (r: Response) => void = () => {}
    const fetchMock = vi.spyOn(globalThis, 'fetch')
    fetchMock.mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve }))
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ ...ready, path: '/w/second.html' })))
    const { result } = renderHook(() => usePreview('pane-1', true))

    act(() => result.current.open('first.html'))
    act(() => result.current.open('second.html'))
    await waitFor(() => expect(result.current.view?.kind).toBe('ready'))
    await act(async () => resolveFirst(new Response(JSON.stringify({ ...ready, path: '/w/first.html' }))))

    expect(result.current.view).toMatchObject({ kind: 'ready', path: '/w/second.html' })
  })

  it('reload asks the frame to load again', async () => {
    respond({ status: 200, body: ready })
    const { result } = renderHook(() => usePreview('pane-1', true))
    act(() => result.current.open('./index.html'))
    await waitFor(() => expect(result.current.view?.kind).toBe('ready'))

    act(() => result.current.reload())
    expect(result.current.view).toMatchObject({ kind: 'ready', generation: 1 })
  })

  it('close hides the preview and drops a request in flight', async () => {
    let resolve: (r: Response) => void = () => {}
    vi.spyOn(globalThis, 'fetch').mockReturnValueOnce(new Promise((r) => { resolve = r }))
    const { result } = renderHook(() => usePreview('pane-1', true))

    act(() => result.current.open('./index.html'))
    act(() => result.current.close())
    await act(async () => resolve(new Response(JSON.stringify(ready))))

    expect(result.current.view).toBeNull()
  })

  it('hides the preview when the terminal disconnects, and does not bring it back', async () => {
    respond({ status: 200, body: ready })
    const { result, rerender } = renderHook(({ connected }) => usePreview('pane-1', connected), {
      initialProps: { connected: true },
    })
    act(() => result.current.open('./index.html'))
    await waitFor(() => expect(result.current.view?.kind).toBe('ready'))

    rerender({ connected: false })
    expect(result.current.view).toBeNull()
    rerender({ connected: true })
    expect(result.current.view).toBeNull()
  })

  it('opens nothing while the terminal is disconnected', () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
    const { result } = renderHook(() => usePreview('pane-1', false))

    act(() => result.current.open('./index.html'))
    expect(result.current.view).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  // Issue #386: a localhost URL on the panemux host is shown as it is served,
  // with nothing to resolve on the server.
  describe('a localhost page', () => {
    it('shows the page itself, without asking the server', () => {
      const fetchMock = vi.spyOn(globalThis, 'fetch')
      const { result } = renderHook(() => usePreview('pane-1', true))

      act(() => result.current.openUrl('http://localhost:5173/app?x=1'))

      expect(result.current.view).toEqual({ kind: 'page', url: 'http://localhost:5173/app?x=1', generation: 0 })
      expect(fetchMock).not.toHaveBeenCalled()
    })

    it('accepts every loopback spelling', () => {
      const { result } = renderHook(() => usePreview('pane-1', true))
      for (const url of ['http://127.0.0.1:3000/', 'https://[::1]:8443/', 'http://LOCALHOST:8080/']) {
        act(() => result.current.openUrl(url))
        expect(result.current.view).toMatchObject({ kind: 'page', url })
      }
    })

    it('shows nothing for a URL that is not a loopback http(s) page', () => {
      const { result } = renderHook(() => usePreview('pane-1', true))
      for (const url of ['https://example.com/', 'file:///etc/passwd', 'javascript:alert(1)', 'not a url']) {
        act(() => result.current.openUrl(url))
        expect(result.current.view).toBeNull()
      }
    })

    it('reloads the page', () => {
      const { result } = renderHook(() => usePreview('pane-1', true))
      act(() => result.current.openUrl('http://localhost:5173/'))

      act(() => result.current.reload())
      expect(result.current.view).toEqual({ kind: 'page', url: 'http://localhost:5173/', generation: 1 })
    })

    it('replaces a file still being resolved, whose answer is then dropped', async () => {
      let resolve: (r: Response) => void = () => {}
      vi.spyOn(globalThis, 'fetch').mockReturnValueOnce(new Promise((r) => { resolve = r }))
      const { result } = renderHook(() => usePreview('pane-1', true))

      act(() => result.current.open('./index.html'))
      act(() => result.current.openUrl('http://localhost:5173/'))
      await act(async () => resolve(new Response(JSON.stringify(ready))))

      expect(result.current.view).toMatchObject({ kind: 'page', url: 'http://localhost:5173/' })
    })

    it('opens nothing while the terminal is disconnected, and is hidden when it disconnects', () => {
      const { result, rerender } = renderHook(({ connected }) => usePreview('pane-1', connected), {
        initialProps: { connected: false },
      })
      act(() => result.current.openUrl('http://localhost:5173/'))
      expect(result.current.view).toBeNull()

      rerender({ connected: true })
      act(() => result.current.openUrl('http://localhost:5173/'))
      expect(result.current.view?.kind).toBe('page')
      rerender({ connected: false })
      expect(result.current.view).toBeNull()
    })
  })
})
