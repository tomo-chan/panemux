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
  vertical_bar_width: 280,
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

function workspacesForLayout(layout: LayoutNode) {
  return {
    active: 'dev',
    tab_position: 'top',
    vertical_bar_width: 280,
    items: [{ id: 'dev', title: 'Dev', layout }],
  }
}

describe('useLayout', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('fetches and parses workspaces on mount', async () => {
    window.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
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

  it('falls back to first workspace when active id is not in the response', async () => {
    const workspaces = { ...validWorkspaces, active: 'missing' }
    window.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(workspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())
    expect(result.current.layout?.children[0].pane?.id).toBe('main')
  })

  it('does not call active workspace API for same or missing workspace selections', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())
    const callsAfterInit = fetchMock.mock.calls.length

    await act(async () => {
      await result.current.setActiveWorkspace('dev')
      await result.current.setActiveWorkspace('missing')
    })

    expect(fetchMock).toHaveBeenCalledTimes(callsAfterInit)
    expect(result.current.workspaces?.active).toBe('dev')
  })

  it('adds a workspace and switches to the returned active workspace', async () => {
    const addedLayout: LayoutNode = {
      direction: 'horizontal',
      children: [{ size: 100, pane: { id: 'workspace-3-main', type: 'local' } }],
    }
    const addedWorkspaces = {
      ...validWorkspaces,
      active: 'workspace-3',
      items: [
        ...validWorkspaces.items,
        { id: 'workspace-3', title: 'Workspace 3', layout: addedLayout },
      ],
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(addedWorkspaces) } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    await act(async () => {
      await result.current.addWorkspace()
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/workspaces', { method: 'POST' })
    expect(result.current.workspaces?.active).toBe('workspace-3')
    expect(result.current.layout?.children[0].pane?.id).toBe('workspace-3-main')
  })

  it('sets error when adding a workspace fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: false, status: 403 } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    await act(async () => {
      await result.current.addWorkspace()
    })

    expect(result.current.error).toContain('403')
    expect(result.current.workspaces?.active).toBe('dev')
  })

  it('deletes a workspace and switches to the returned active workspace', async () => {
    const remainingWorkspaces = {
      active: 'ops',
      tab_position: 'top',
      vertical_bar_width: 280,
      items: [validWorkspaces.items[1]],
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(remainingWorkspaces) } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    await act(async () => {
      await result.current.deleteWorkspace('dev')
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/workspaces/dev', { method: 'DELETE' })
    expect(result.current.workspaces?.items).toHaveLength(1)
    expect(result.current.workspaces?.active).toBe('ops')
    expect(result.current.layout?.children[0].pane?.id).toBe('ops-main')
  })

  it('sets error when deleting a workspace fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: false, status: 409 } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    await act(async () => {
      await result.current.deleteWorkspace('dev')
    })

    expect(result.current.error).toContain('409')
    expect(result.current.workspaces?.items).toHaveLength(2)
  })

  it('renames a workspace and keeps the current layout', async () => {
    const renamedWorkspaces = {
      ...validWorkspaces,
      items: [
        { ...validWorkspaces.items[0], title: 'Development' },
        validWorkspaces.items[1],
      ],
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(renamedWorkspaces) } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    await act(async () => {
      await result.current.renameWorkspace('dev', 'Development')
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/workspaces/dev', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ title: 'Development' }),
    }))
    expect(result.current.workspaces?.items[0].title).toBe('Development')
    expect(result.current.layout?.children[0].pane?.id).toBe('main')
  })

  it('sets error when renaming a workspace fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: false, status: 422 } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    await act(async () => {
      await result.current.renameWorkspace('dev', '   ')
    })

    expect(result.current.error).toContain('422')
    expect(result.current.workspaces?.items[0].title).toBe('Dev')
  })

  it('updates workspace tab position from the returned response', async () => {
    const updatedWorkspaces = { ...validWorkspaces, tab_position: 'left' as const }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(updatedWorkspaces) } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    await act(async () => {
      await result.current.setWorkspaceTabPosition('left')
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/workspaces/tab-position', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify({ tab_position: 'left' }),
    }))
    expect(result.current.workspaces?.tab_position).toBe('left')
    expect(result.current.layout?.children[0].pane?.id).toBe('main')
  })

  it('adopts server top-level workspace fields while preserving current items when updating tab position', async () => {
    const updatedWorkspaces = {
      ...validWorkspaces,
      active: 'ops',
      tab_position: 'right' as const,
      items: validWorkspaces.items.map((workspace) => (
        workspace.id === 'dev' ? { ...workspace, title: 'Server Dev' } : workspace
      )),
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(updatedWorkspaces) } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    await act(async () => {
      await result.current.setWorkspaceTabPosition('right')
    })

    expect(result.current.workspaces?.active).toBe('ops')
    expect(result.current.workspaces?.tab_position).toBe('right')
    expect(result.current.workspaces?.items[0].title).toBe('Dev')
  })

  it('preserves a pending local layout change when updating workspace tab position', async () => {
    const pendingLayout: LayoutNode = {
      direction: 'vertical',
      children: [{ size: 100, pane: { id: 'pending-main', type: 'local' } }],
    }
    const staleServerWorkspaces = { ...validWorkspaces, tab_position: 'left' as const }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(staleServerWorkspaces) } as Response)
      .mockResolvedValue({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    vi.useFakeTimers()
    try {
      act(() => {
        result.current.updateSizes(pendingLayout)
      })

      expect(result.current.layout?.children[0].pane?.id).toBe('pending-main')

      await act(async () => {
        await result.current.setWorkspaceTabPosition('left')
      })

      expect(result.current.workspaces?.tab_position).toBe('left')
      expect(result.current.layout?.children[0].pane?.id).toBe('pending-main')
      expect(result.current.workspaces?.items[0].layout.children[0].pane?.id).toBe('pending-main')
    } finally {
      vi.useRealTimers()
    }
  })

  it('accepts a tab position response before workspace state has loaded', async () => {
    const updatedWorkspaces = { ...validWorkspaces, tab_position: 'left' as const }
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === '/api/workspaces' && init?.method === undefined) {
        return new Promise<Response>(() => {})
      }
      if (url === '/api/display') {
        return Promise.resolve({ ok: false } as Response)
      }
      if (url === '/api/workspaces/tab-position') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(updatedWorkspaces) } as Response)
      }
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    expect(result.current.workspaces).toBeNull()

    await act(async () => {
      await result.current.setWorkspaceTabPosition('left')
    })

    expect(result.current.workspaces?.tab_position).toBe('left')
    expect(result.current.layout).toBeNull()
  })

  it('sets error when updating workspace tab position fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: false, status: 422 } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    await act(async () => {
      await result.current.setWorkspaceTabPosition('left')
    })

    expect(result.current.error).toContain('422')
    expect(result.current.workspaces?.tab_position).toBe('top')
  })

  it('updates workspace vertical bar width optimistically and persists it', async () => {
    const updatedWorkspaces = { ...validWorkspaces, vertical_bar_width: 320 }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(updatedWorkspaces) } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    act(() => {
      result.current.setWorkspaceVerticalBarWidth(320)
    })

    expect(result.current.workspaces?.vertical_bar_width).toBe(320)

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/workspaces/vertical-bar-width', expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({ vertical_bar_width: 320 }),
      }))
    })
    expect(result.current.workspaces?.vertical_bar_width).toBe(320)
  })

  it('sets error when updating workspace vertical bar width fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validDisplay) } as Response)
      .mockResolvedValueOnce({ ok: false, status: 422 } as Response)
    window.fetch = fetchMock

    const { result } = renderHook(() => useLayout())
    await waitFor(() => expect(result.current.workspaces).not.toBeNull())

    act(() => {
      result.current.setWorkspaceVerticalBarWidth(120)
    })
    expect(result.current.error).toContain('greater than or equal to 180')

    act(() => {
      result.current.setWorkspaceVerticalBarWidth(320)
    })

    await waitFor(() => {
      expect(result.current.error).toContain('422')
    })
  })

  it('fetches display config on mount', async () => {
    window.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
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
        json: () => Promise.resolve(validWorkspaces),
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
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response) // GET /api/workspaces
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

    it('saves split changes to the active workspace endpoint', async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ shell: '/bin/zsh' }) } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.workspaces).not.toBeNull())

      await act(async () => {
        await result.current.splitPane('main', 'horizontal')
      })

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/workspaces/dev/layout',
        expect.objectContaining({ method: 'PUT' }),
      )
      expect(result.current.workspaces?.items[0].layout.children[0].children).toHaveLength(2)
      expect(result.current.workspaces?.items[1].layout.direction).toBe('vertical')
    })

    it('sets detected shell on the new pane when splitting', async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
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
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(workspacesForLayout(sshLayout)) } as Response)
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
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(workspacesForLayout(localLayout)) } as Response)
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
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
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
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(workspacesForLayout(tmuxLayout)) } as Response)
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
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(workspacesForLayout(twoChildLayout)) } as Response) // GET /api/workspaces
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

    it('saves close changes to the active workspace endpoint', async () => {
      const workspaceLayout: LayoutNode = {
        direction: 'horizontal',
        children: [
          { size: 50, pane: { id: 'main', type: 'local' } },
          { size: 50, pane: { id: 'other', type: 'local' } },
        ],
      }
      const workspaces = {
        ...validWorkspaces,
        items: [
          { id: 'dev', title: 'Dev', layout: workspaceLayout },
          validWorkspaces.items[1],
        ],
      }
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(workspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: true } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.workspaces).not.toBeNull())

      await act(async () => {
        await result.current.closePane('main')
      })

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/workspaces/dev/layout',
        expect.objectContaining({ method: 'PUT' }),
      )
      expect(result.current.workspaces?.items[0].layout.children[0].pane?.id).toBe('other')
    })

    it('sets layout to null when last pane is closed', async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
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
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(workspacesForLayout(twoChildLayout)) } as Response) // GET /api/workspaces
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
        (c) => c[0] === '/api/workspaces/dev/layout' && (c[1] as RequestInit)?.method === 'PUT',
      )
      expect(putCall).toBeDefined()
    })

    it('saves swapped panes to the active workspace endpoint', async () => {
      const workspaceLayout: LayoutNode = {
        direction: 'horizontal',
        children: [
          { size: 50, pane: { id: 'left', type: 'local' } },
          { size: 50, pane: { id: 'right', type: 'ssh' } },
        ],
      }
      const workspaces = {
        ...validWorkspaces,
        items: [
          { id: 'dev', title: 'Dev', layout: workspaceLayout },
          validWorkspaces.items[1],
        ],
      }
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(workspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.workspaces).not.toBeNull())

      await act(async () => {
        await result.current.swapPanes('left', 'right')
      })

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/workspaces/dev/layout',
        expect.objectContaining({ method: 'PUT' }),
      )
      expect(result.current.workspaces?.items[0].layout.children[0].pane?.id).toBe('right')
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

  describe('createPane', () => {
    it('creates a pane on the workspace edge and persists the layout', async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ shell: '/bin/zsh' }) } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ id: 'created', type: 'local', state: 'connected' }) } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.createPane({ id: 'created', type: 'local' }, { type: 'workspace-edge', edge: 'right' })
      })

      expect(fetchMock).toHaveBeenCalledWith('/api/sessions', expect.objectContaining({ method: 'POST' }))
      expect(fetchMock).toHaveBeenCalledWith('/api/workspaces/dev/layout', expect.objectContaining({ method: 'PUT' }))
      expect(result.current.layout?.children.some((child) => child.pane?.id === 'created')).toBe(true)
    })

    it('creates a pane beside an existing target pane', async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ id: 'created', type: 'ssh', state: 'connected' }) } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.createPane(
          { id: 'created', type: 'ssh', connection: 'prod' },
          { type: 'pane-edge', targetPaneId: 'main', edge: 'bottom' },
        )
      })

      expect(result.current.layout?.children[0].direction).toBe('vertical')
      expect(fetchMock).toHaveBeenCalledWith('/api/workspaces/dev/layout', expect.objectContaining({ method: 'PUT' }))
    })

    it('throws when session creation fails', async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ shell: '/bin/zsh' }) } as Response)
        .mockResolvedValueOnce({ ok: false, status: 500 } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await expect(result.current.createPane(
          { id: 'created', type: 'local' },
          { type: 'workspace-edge', edge: 'left' },
        )).rejects.toThrow('HTTP 500')
      })
    })
  })

  describe('movePane', () => {
    it('moves a pane to the workspace edge without creating a new session', async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.movePane('main', { type: 'workspace-edge', edge: 'right' })
      })

      const sessionPosts = fetchMock.mock.calls.filter((call) => call[0] === '/api/sessions')
      expect(sessionPosts).toHaveLength(0)
      expect(fetchMock).toHaveBeenCalledWith('/api/workspaces/dev/layout', expect.objectContaining({ method: 'PUT' }))
      expect(result.current.layout?.children[result.current.layout.children.length - 1].pane?.id).toBe('main')
    })

    it('moves a pane beside another target pane', async () => {
      const nestedLayout: LayoutNode = {
        direction: 'horizontal',
        children: [
          { size: 50, pane: { id: 'left', type: 'local' } },
          { size: 50, pane: { id: 'right', type: 'local' } },
        ],
      }
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(workspacesForLayout(nestedLayout)) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.movePane('right', { type: 'pane-edge', targetPaneId: 'left', edge: 'bottom' })
      })

      expect(result.current.layout?.children[0].direction).toBe('vertical')
      expect(fetchMock).toHaveBeenCalledWith('/api/workspaces/dev/layout', expect.objectContaining({ method: 'PUT' }))
    })

    it('moves a pane to another workspace and activates the destination workspace', async () => {
      const multiPaneWorkspaces = {
        active: 'dev',
        tab_position: 'top' as const,
        vertical_bar_width: 280,
        items: [
          {
            id: 'dev',
            title: 'Dev',
            layout: {
              direction: 'horizontal' as const,
              children: [
                { size: 50, pane: { id: 'main', type: 'local' as const } },
                { size: 50, pane: { id: 'side', type: 'local' as const } },
              ],
            },
          },
          {
            id: 'ops',
            title: 'Ops',
            layout: {
              direction: 'vertical' as const,
              children: [{ size: 100, pane: { id: 'ops-main', type: 'local' as const } }],
            },
          },
        ],
      }
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(multiPaneWorkspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await result.current.movePane('main', { type: 'workspace-tab', workspaceId: 'ops' })
      })

      expect(fetchMock).toHaveBeenCalledWith('/api/workspaces/dev/layout', expect.objectContaining({ method: 'PUT' }))
      expect(fetchMock).toHaveBeenCalledWith('/api/workspaces/ops/layout', expect.objectContaining({ method: 'PUT' }))
      expect(result.current.workspaces?.active).toBe('ops')
      expect(result.current.layout?.children[result.current.layout.children.length - 1].pane?.id).toBe('main')
      expect(result.current.workspaces?.items.find((workspace) => workspace.id === 'dev')?.layout.children).toHaveLength(1)
      expect(result.current.workspaces?.items.find((workspace) => workspace.id === 'dev')?.layout.children[0].pane?.id).toBe('side')
    })

    it('rejects moving the last pane out of a workspace', async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await expect(result.current.movePane('main', { type: 'workspace-tab', workspaceId: 'ops' })).rejects.toThrow(
          'Cannot move the last pane out of a workspace',
        )
      })

      expect(fetchMock).not.toHaveBeenCalledWith('/api/workspaces/dev/layout', expect.anything())
      expect(fetchMock).not.toHaveBeenCalledWith('/api/workspaces/ops/layout', expect.anything())
      expect(result.current.workspaces?.active).toBe('dev')
    })

    it('rolls back frontend state and restores the source workspace when the destination save fails', async () => {
      const multiPaneWorkspaces = {
        active: 'dev',
        tab_position: 'top' as const,
        vertical_bar_width: 280,
        items: [
          {
            id: 'dev',
            title: 'Dev',
            layout: {
              direction: 'horizontal' as const,
              children: [
                { size: 50, pane: { id: 'main', type: 'local' as const } },
                { size: 50, pane: { id: 'side', type: 'local' as const } },
              ],
            },
          },
          {
            id: 'ops',
            title: 'Ops',
            layout: {
              direction: 'vertical' as const,
              children: [{ size: 100, pane: { id: 'ops-main', type: 'local' as const } }],
            },
          },
        ],
      }
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(multiPaneWorkspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
        .mockResolvedValueOnce({ ok: false, status: 500 } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await expect(result.current.movePane('main', { type: 'workspace-tab', workspaceId: 'ops' })).rejects.toThrow('HTTP 500')
      })

      expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/workspaces/dev/layout', expect.objectContaining({ method: 'PUT' }))
      expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/workspaces/ops/layout', expect.objectContaining({ method: 'PUT' }))
      expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/workspaces/dev/layout', expect.objectContaining({ method: 'PUT' }))
      expect(result.current.workspaces?.active).toBe('dev')
      expect(result.current.layout?.children[0].pane?.id).toBe('main')
      expect(result.current.workspaces?.items.find((workspace) => workspace.id === 'dev')?.layout.children).toHaveLength(2)
      expect(result.current.workspaces?.items.find((workspace) => workspace.id === 'ops')?.layout.children[0].pane?.id).toBe('ops-main')
    })

    it('throws a distinct error when both the destination save and rollback save fail', async () => {
      const multiPaneWorkspaces = {
        active: 'dev',
        tab_position: 'top' as const,
        vertical_bar_width: 280,
        items: [
          {
            id: 'dev',
            title: 'Dev',
            layout: {
              direction: 'horizontal' as const,
              children: [
                { size: 50, pane: { id: 'main', type: 'local' as const } },
                { size: 50, pane: { id: 'side', type: 'local' as const } },
              ],
            },
          },
          {
            id: 'ops',
            title: 'Ops',
            layout: {
              direction: 'vertical' as const,
              children: [{ size: 100, pane: { id: 'ops-main', type: 'local' as const } }],
            },
          },
        ],
      }
      const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(multiPaneWorkspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) } as Response)
        .mockResolvedValueOnce({ ok: false, status: 500 } as Response)
        .mockRejectedValueOnce(new Error('rollback failed'))
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await expect(result.current.movePane('main', { type: 'workspace-tab', workspaceId: 'ops' })).rejects.toThrow(
          'Move failed and rollback also failed. Reload to sync.',
        )
      })

      expect(consoleErrorSpy).toHaveBeenCalledWith(expect.any(Error))
      expect(result.current.workspaces?.active).toBe('dev')
      expect(result.current.layout?.children[0].pane?.id).toBe('main')
    })

    it('throws when persisting a moved pane fails', async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockResolvedValueOnce({ ok: false, status: 500 } as Response)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await expect(result.current.movePane('main', { type: 'workspace-edge', edge: 'left' })).rejects.toThrow('HTTP 500')
      })
    })

    it('throws when persisting a moved pane hits a network failure', async () => {
      const networkError = new Error('network down')
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(validWorkspaces) } as Response)
        .mockResolvedValueOnce({ ok: false } as Response)
        .mockRejectedValueOnce(networkError)
      window.fetch = fetchMock

      const { result } = renderHook(() => useLayout())
      await waitFor(() => expect(result.current.layout).not.toBeNull())

      await act(async () => {
        await expect(result.current.movePane('main', { type: 'workspace-edge', edge: 'left' })).rejects.toThrow('network down')
      })
    })
  })
})
