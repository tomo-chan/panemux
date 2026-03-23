import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { usePaneSettings } from './usePaneSettings'
import type { LayoutNode, PaneConfig, SSHConfigHost } from '../schemas'

const mockLayout: LayoutNode = {
  direction: 'horizontal',
  children: [
    { size: 50, pane: { id: 'pane-1', type: 'local' } },
    { size: 50, pane: { id: 'pane-2', type: 'ssh', connection: 'prod' } },
  ],
}

const mockPane: PaneConfig = { id: 'pane-1', type: 'local' }

function makeFetchOk(body: unknown) {
  return vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve(body),
  })
}


beforeEach(() => {
  vi.restoreAllMocks()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('usePaneSettings', () => {
  it('fetches ssh connection names on mount', async () => {
    vi.stubGlobal('fetch', makeFetchOk({ names: ['prod', 'dev'] }))
    const { result } = renderHook(() => usePaneSettings(mockLayout, vi.fn()))
    await waitFor(() => expect(result.current.sshConnectionNames).toEqual(['prod', 'dev']))
  })

  it('returns empty names when API returns empty', async () => {
    vi.stubGlobal('fetch', makeFetchOk({ names: [] }))
    const { result } = renderHook(() => usePaneSettings(mockLayout, vi.fn()))
    await waitFor(() => expect(result.current.sshConnectionNames).toEqual([]))
  })

  it('openSettings sets isOpen true with the given pane', async () => {
    vi.stubGlobal('fetch', makeFetchOk({ names: [] }))
    const { result } = renderHook(() => usePaneSettings(mockLayout, vi.fn()))
    await waitFor(() => expect(result.current.sshConnectionNames).toEqual([]))
    act(() => result.current.openSettings(mockPane))
    expect(result.current.isOpen).toBe(true)
    expect(result.current.currentPane).toEqual(mockPane)
  })

  it('closeSettings sets isOpen false', async () => {
    vi.stubGlobal('fetch', makeFetchOk({ names: [] }))
    const { result } = renderHook(() => usePaneSettings(mockLayout, vi.fn()))
    await waitFor(() => expect(result.current.sshConnectionNames).toEqual([]))
    act(() => result.current.openSettings(mockPane))
    act(() => result.current.closeSettings())
    expect(result.current.isOpen).toBe(false)
    expect(result.current.currentPane).toBeNull()
  })

  it('saveSettings calls PUT /api/layout then POST restart on success', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ names: [] }) }) // GET ssh-connections
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(mockLayout) })     // PUT layout
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) })             // POST restart

    vi.stubGlobal('fetch', fetchMock)
    const onLayoutChange = vi.fn()
    const { result } = renderHook(() => usePaneSettings(mockLayout, onLayoutChange))

    act(() => result.current.openSettings(mockPane))
    await act(async () => {
      await result.current.saveSettings({ ...mockPane, type: 'local', shell: '/bin/zsh' })
    })

    // PUT layout called
    expect(fetchMock).toHaveBeenCalledWith('/api/layout', expect.objectContaining({ method: 'PUT' }))
    // POST restart called
    expect(fetchMock).toHaveBeenCalledWith(`/api/sessions/${mockPane.id}/restart`, expect.objectContaining({ method: 'POST' }))
    expect(onLayoutChange).toHaveBeenCalled()
  })

  it('saveSettings closes dialog on success', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ names: [] }) })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(mockLayout) })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) })

    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => usePaneSettings(mockLayout, vi.fn()))

    act(() => result.current.openSettings(mockPane))
    await act(async () => {
      await result.current.saveSettings(mockPane)
    })

    expect(result.current.isOpen).toBe(false)
  })

  it('saveSettings shows error and keeps dialog open on PUT failure', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ names: [] }) })
      .mockResolvedValueOnce({ ok: false, status: 422, json: () => Promise.resolve({ error: 'invalid layout' }) })

    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => usePaneSettings(mockLayout, vi.fn()))

    act(() => result.current.openSettings(mockPane))
    await act(async () => {
      await result.current.saveSettings(mockPane)
    })

    expect(result.current.isOpen).toBe(true)
    expect(result.current.saveError).toBe('invalid layout')
  })

  it('saveSettings does not call restart if PUT failed', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ names: [] }) })
      .mockResolvedValueOnce({ ok: false, status: 422, json: () => Promise.resolve({ error: 'fail' }) })

    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => usePaneSettings(mockLayout, vi.fn()))

    act(() => result.current.openSettings(mockPane))
    await act(async () => {
      await result.current.saveSettings(mockPane)
    })

    // Only 2 calls: GET ssh-connections + PUT layout (no restart)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('restart failure is non-fatal and dialog closes', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ names: [] }) })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(mockLayout) })
      .mockRejectedValueOnce(new Error('network error')) // restart fails

    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => usePaneSettings(mockLayout, vi.fn()))

    act(() => result.current.openSettings(mockPane))
    await act(async () => {
      await result.current.saveSettings(mockPane)
    })

    // Dialog should still close even if restart fails
    expect(result.current.isOpen).toBe(false)
  })

  describe('addSSHConfigHost', () => {
    const mockHost: SSHConfigHost = {
      name: 'new-host',
      hostname: 'new.example.com',
      user: 'ubuntu',
    }

    it('posts to /api/ssh-config/hosts and refreshes ssh connection names', async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ names: [] }) }) // initial GET ssh-connections
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) })            // POST ssh-config/hosts
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ names: ['new-host'] }) }) // GET ssh-connections refresh

      vi.stubGlobal('fetch', fetchMock)
      const { result } = renderHook(() => usePaneSettings(mockLayout, vi.fn()))
      await waitFor(() => expect(result.current.sshConnectionNames).toEqual([]))

      await act(async () => {
        const name = await result.current.addSSHConfigHost(mockHost)
        expect(name).toBe('new-host')
      })

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/ssh-config/hosts',
        expect.objectContaining({ method: 'POST' }),
      )
      await waitFor(() => expect(result.current.sshConnectionNames).toEqual(['new-host']))
    })

    it('throws on POST failure', async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ names: [] }) })
        .mockResolvedValueOnce({
          ok: false,
          status: 409,
          json: () => Promise.resolve({ error: 'host already exists' }),
        })

      vi.stubGlobal('fetch', fetchMock)
      const { result } = renderHook(() => usePaneSettings(mockLayout, vi.fn()))

      await expect(
        act(async () => {
          await result.current.addSSHConfigHost(mockHost)
        }),
      ).rejects.toThrow('host already exists')
    })

    it('returns host name on success', async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ names: [] }) })
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) })
        .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ names: ['new-host'] }) })

      vi.stubGlobal('fetch', fetchMock)
      const { result } = renderHook(() => usePaneSettings(mockLayout, vi.fn()))

      let returnedName = ''
      await act(async () => {
        returnedName = await result.current.addSSHConfigHost(mockHost)
      })
      expect(returnedName).toBe('new-host')
    })
  })
})
