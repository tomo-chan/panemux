import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { useLayout } from './useLayout'
import type { LayoutNode } from '../schemas'

const validLayout: LayoutNode = {
  direction: 'horizontal',
  children: [{ size: 100, pane: { id: 'main', type: 'local' } }],
}

const validDisplay = { show_header: true, show_status_bar: true }

describe('useLayout', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('fetches and parses layout on mount', async () => {
    window.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validLayout) } as Response)
      .mockResolvedValue({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.layout).not.toBeNull())
    expect(result.current.layout?.direction).toBe('horizontal')
    expect(result.current.error).toBeNull()
  })

  it('fetches display config on mount', async () => {
    window.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validLayout) } as Response)
      .mockResolvedValue({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.displayConfig).not.toBeNull())
    expect(result.current.displayConfig?.show_header).toBe(true)
    expect(result.current.displayConfig?.show_status_bar).toBe(true)
  })

  it('sets error on fetch failure', async () => {
    window.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: false, status: 500 } as Response)
      .mockResolvedValue({ ok: false } as Response)

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.error).not.toBeNull())
    expect(result.current.error).toContain('500')
  })

  it('sets error when server returns invalid schema', async () => {
    window.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ direction: 'diagonal', children: [] }),
      } as Response)
      .mockResolvedValue({ ok: false } as Response)

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.error).not.toBeNull())
  })

  it('debounces updateSizes calls', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(validLayout),
      } as Response)
      .mockResolvedValue({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    // Wait for initial fetches (layout + display) without fake timers
    await waitFor(() => {
      expect(result.current.layout).not.toBeNull()
      expect(result.current.displayConfig).not.toBeNull()
    })

    // Count calls so far (layout + display = 2)
    const callsAfterInit = fetchMock.mock.calls.length

    // Enable fake timers just for the debounce assertion
    vi.useFakeTimers()
    try {
      const updated: LayoutNode = { ...validLayout, direction: 'vertical' }
      act(() => {
        result.current.updateSizes(updated)
        result.current.updateSizes(updated)
        result.current.updateSizes(updated)
      })

      // Debounce not yet fired
      expect(fetchMock).toHaveBeenCalledTimes(callsAfterInit)

      // Advance past debounce delay (500 ms)
      await vi.runAllTimersAsync()

      // Exactly one debounced PUT on top of init calls
      expect(fetchMock).toHaveBeenCalledTimes(callsAfterInit + 1)
    } finally {
      vi.useRealTimers()
    }
  })

  describe('splitPane', () => {
    it('creates a session and updates layout', async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validLayout) } as Response) // GET /api/layout
        .mockResolvedValueOnce({ ok: false } as Response) // GET /api/display (non-fatal)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ shell: '/bin/zsh' }) } as Response) // GET /api/detect-shell
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({ id: 'new-pane', type: 'local', title: '', state: 'connecting' }),
        } as Response) // POST /api/sessions
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response) // PUT /api/layout
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.splitPane('main', 'horizontal')
      })

      // Layout should have a split node at root child
      const child = result.current.layout?.children[0]
      expect(child?.direction).toBe('horizontal')
      expect(child?.children).toHaveLength(2)
      expect(child?.children?.[0].pane?.id).toBe('main')
    })

    it('sets detected shell on the new pane when splitting', async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validLayout) } as Response) // GET /api/layout
        .mockResolvedValueOnce({ ok: false } as Response) // GET /api/display (non-fatal)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ shell: '/bin/zsh' }) } as Response) // GET /api/detect-shell
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response) // POST /api/sessions
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response) // PUT /api/layout
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.splitPane('main', 'horizontal')
      })

      const newPane = result.current.layout?.children[0].children?.[1].pane
      expect(newPane?.shell).toBe('/bin/zsh')
    })

    it('splits successfully even when detect-shell fails', async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validLayout) } as Response) // GET /api/layout
        .mockResolvedValueOnce({ ok: false } as Response) // GET /api/display (non-fatal)
        .mockResolvedValueOnce({ ok: false, status: 500 } as Response) // GET /api/detect-shell fails
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response) // POST /api/sessions
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response) // PUT /api/layout
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.splitPane('main', 'horizontal')
      })

      const child = result.current.layout?.children[0]
      expect(child?.children).toHaveLength(2)
      expect(child?.children?.[1].pane?.shell).toBeUndefined()
    })
  })

  describe('closePane', () => {
    it('removes the pane from layout', async () => {
      const twoChildLayout: LayoutNode = {
        direction: 'horizontal',
        children: [
          { size: 50, pane: { id: 'main', type: 'local' } },
          { size: 50, pane: { id: 'other', type: 'local' } },
        ],
      }
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(twoChildLayout) } as Response) // GET /api/layout
        .mockResolvedValueOnce({ ok: false } as Response) // GET /api/display (non-fatal)
        .mockResolvedValueOnce({ ok: true } as Response) // DELETE /api/sessions/main
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response) // PUT /api/layout
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.closePane('main')
      })

      expect(result.current.layout?.children).toHaveLength(1)
      expect(result.current.layout?.children[0].pane?.id).toBe('other')
    })

    it('sets layout to null when last pane is closed', async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validLayout) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response) // display (non-fatal)
        .mockResolvedValueOnce({ ok: true } as Response) // DELETE
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.closePane('main')
      })

      expect(result.current.layout).toBeNull()
    })
  })

  describe('swapPanes', () => {
    it('swaps two panes and PUTs updated layout', async () => {
      const twoChildLayout: LayoutNode = {
        direction: 'horizontal',
        children: [
          { size: 50, pane: { id: 'left', type: 'local' } },
          { size: 50, pane: { id: 'right', type: 'ssh' } },
        ],
      }
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(twoChildLayout) } as Response) // GET /api/layout
        .mockResolvedValueOnce({ ok: false } as Response) // GET /api/display
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response) // PUT /api/layout
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.swapPanes('left', 'right')
      })

      expect(result.current.layout?.children[0].pane?.id).toBe('right')
      expect(result.current.layout?.children[1].pane?.id).toBe('left')
      const putCall = fetchMock.mock.calls.find(
        (c) => c[0] === '/api/layout' && (c[1] as RequestInit)?.method === 'PUT',
      )
      expect(putCall).toBeDefined()
    })

    it('does nothing when layout is null', async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: false, status: 500 } as Response) // GET /api/layout fails
        .mockResolvedValueOnce({ ok: false } as Response) // GET /api/display
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.error).not.toBeNull())

      const callsBefore = fetchMock.mock.calls.length
      await act(async () => {
        await result.current.swapPanes('a', 'b')
      })
      expect(fetchMock).toHaveBeenCalledTimes(callsBefore)
    })
  })
})
