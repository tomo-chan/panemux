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
})
