import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { useLayout } from './useLayout'
import type { LayoutNode } from '../schemas'

const validLayout: LayoutNode = {
  direction: 'horizontal',
  children: [{ size: 100, pane: { id: 'main', type: 'local' } }],
}

const validDisplay = { show_header: true, show_status_bar: true }

const validWorkspaces = {
  active: 'dev',
  tab_position: 'top',
  items: [
    { id: 'dev', title: 'Dev', layout: validLayout },
    {
      id: 'ops',
      title: 'Ops',
      layout: {
        direction: 'vertical',
        children: [{ size: 100, pane: { id: 'ops-main', type: 'local' } }],
      },
    },
  ],
}

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

  it('fetches workspaces and switches active workspace', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())
    expect(result.current.layout?.children[0].pane?.id).toBe('main')

    await act(async () => {
      await result.current.setActiveWorkspace('ops')
    })

    expect(result.current.workspaces?.active).toBe('ops')
    expect(result.current.layout?.direction).toBe('vertical')
    expect(fetchMock).toHaveBeenCalledWith('/api/workspaces/active', expect.objectContaining({ method: 'PUT' }))
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

  it('saves size updates to the active workspace endpoint', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(validWorkspaces),
      } as Response)
      .mockResolvedValue({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    vi.useFakeTimers()
    try {
      const updated: LayoutNode = { ...validLayout, direction: 'vertical' }
      act(() => {
        result.current.updateSizes(updated)
      })
      await vi.runAllTimersAsync()

      expect(result.current.workspaces?.items[0].layout.direction).toBe('vertical')
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/workspaces/dev/layout',
        expect.objectContaining({ method: 'PUT' }),
      )
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
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validLayout) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ shell: '/bin/zsh' }) } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.splitPane('main', 'horizontal')
      })

      const newPane = result.current.layout?.children[0].children?.[1].pane
      expect(newPane?.shell).toBe('/bin/zsh')
    })

    it('inherits source pane settings when splitting', async () => {
      const sshLayout: LayoutNode = {
        direction: 'horizontal',
        children: [
          {
            size: 100,
            pane: {
              id: 'ssh-pane',
              type: 'ssh',
              connection: 'prod',
              cwd: '/home/user',
              show_header: false,
              show_status_bar: false,
            },
          },
        ],
      }
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(sshLayout) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({ id: 'new-pane', type: 'ssh', title: '', state: 'connecting' }),
        } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.splitPane('ssh-pane', 'vertical')
      })

      const newPane = result.current.layout?.children[0].children?.[1].pane
      expect(newPane?.id).not.toBe('ssh-pane')
      expect(newPane?.type).toBe('ssh')
      expect(newPane?.connection).toBe('prod')
      expect(newPane?.cwd).toBe('/home/user')
      expect(newPane?.show_header).toBe(false)
      expect(newPane?.show_status_bar).toBe(false)
      expect(newPane?.title).toBeUndefined()

      const postCall = fetchMock.mock.calls.find(
        (c) => c[0] === '/api/sessions' && (c[1] as RequestInit)?.method === 'POST',
      )
      const body = JSON.parse((postCall![1] as RequestInit).body as string)
      expect(body.type).toBe('ssh')
      expect(body.connection).toBe('prod')
      expect(body.cwd).toBe('/home/user')
      expect(body.show_header).toBe(false)
      expect(body.show_status_bar).toBe(false)
    })

    it('inherits shell and cwd from local pane when splitting without detecting shell again', async () => {
      const localLayout: LayoutNode = {
        direction: 'horizontal',
        children: [
          {
            size: 100,
            pane: { id: 'local-pane', type: 'local', shell: '/bin/zsh', cwd: '/projects/myapp' },
          },
        ],
      }
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(localLayout) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({ id: 'new-pane', type: 'local', title: '', state: 'connecting' }),
        } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.splitPane('local-pane', 'horizontal')
      })

      const newPane = result.current.layout?.children[0].children?.[1].pane
      expect(newPane?.type).toBe('local')
      expect(newPane?.shell).toBe('/bin/zsh')
      expect(newPane?.cwd).toBe('/projects/myapp')
      expect(fetchMock).not.toHaveBeenCalledWith('/api/detect-shell')
    })

    it('splits successfully even when detect-shell fails', async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validLayout) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: false, status: 500 } as Response)
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({ id: 'new-pane', type: 'local', title: '', state: 'connecting' }),
        } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
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

    it('generates a new tmux_session name when splitting a tmux pane', async () => {
      const tmuxLayout: LayoutNode = {
        direction: 'horizontal',
        children: [
          {
            size: 100,
            pane: { id: 'tmux-pane', type: 'tmux', tmux_session: 'main' },
          },
        ],
      }
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(tmuxLayout) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({ id: 'new-pane', type: 'tmux', title: '', state: 'connecting' }),
        } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.splitPane('tmux-pane', 'vertical')
      })

      const newPane = result.current.layout?.children[0].children?.[1].pane
      expect(newPane?.type).toBe('tmux')
      // A new unique session name must be generated based on the original name
      expect(newPane?.tmux_session).toBeDefined()
      expect(newPane?.tmux_session).not.toBe('main')
      expect(newPane?.tmux_session).toMatch(/^main-[a-zA-Z0-9]+$/)

      const postCall = fetchMock.mock.calls.find(
        (c) => c[0] === '/api/sessions' && (c[1] as RequestInit)?.method === 'POST',
      )
      const body = JSON.parse((postCall![1] as RequestInit).body as string)
      expect(body.tmux_session).toBeDefined()
      expect(body.tmux_session).not.toBe('main')
      expect(body.tmux_session).toMatch(/^main-[a-zA-Z0-9]+$/)
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
