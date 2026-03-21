import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { useEditMode } from './useEditMode'

describe('useEditMode', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('fetches initial editMode false on mount', async () => {
    window.fetch = vi.fn().mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ editMode: false }),
    } as Response)

    const { result } = renderHook(() => useEditMode())
    await waitFor(() => expect(result.current.isLoading).toBe(false))
    expect(result.current.editMode).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('fetches initial editMode true on mount', async () => {
    window.fetch = vi.fn().mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ editMode: true }),
    } as Response)

    const { result } = renderHook(() => useEditMode())
    await waitFor(() => expect(result.current.isLoading).toBe(false))
    expect(result.current.editMode).toBe(true)
  })

  it('sets error on fetch failure', async () => {
    window.fetch = vi.fn().mockResolvedValueOnce({
      ok: false,
      status: 500,
    } as Response)

    const { result } = renderHook(() => useEditMode())
    await waitFor(() => expect(result.current.isLoading).toBe(false))
    expect(result.current.error).toContain('500')
  })

  it('toggleEditMode calls PUT with toggled value and updates state on success', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ editMode: false }) } as Response) // initial GET
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ editMode: true }) } as Response) // PUT

    window.fetch = fetchMock

    const { result } = renderHook(() => useEditMode())
    await waitFor(() => expect(result.current.isLoading).toBe(false))
    expect(result.current.editMode).toBe(false)

    await act(async () => {
      await result.current.toggleEditMode()
    })

    expect(result.current.editMode).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    const [putUrl, putInit] = fetchMock.mock.calls[1]
    expect(putUrl).toBe('/api/edit-mode')
    expect(putInit?.method).toBe('PUT')
    expect(JSON.parse(putInit?.body as string)).toEqual({ editMode: true })
  })

  it('toggleEditMode reverts state on server error', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ editMode: false }) } as Response) // initial GET
      .mockResolvedValueOnce({ ok: false, status: 500 } as Response) // PUT fails

    window.fetch = fetchMock

    const { result } = renderHook(() => useEditMode())
    await waitFor(() => expect(result.current.isLoading).toBe(false))
    expect(result.current.editMode).toBe(false)

    await act(async () => {
      await result.current.toggleEditMode()
    })

    // Should revert to false after server error
    expect(result.current.editMode).toBe(false)
  })

  it('setEditMode sets the value directly', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ editMode: false }) } as Response) // initial GET
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ editMode: true }) } as Response) // PUT

    window.fetch = fetchMock

    const { result } = renderHook(() => useEditMode())
    await waitFor(() => expect(result.current.isLoading).toBe(false))

    await act(async () => {
      await result.current.setEditMode(true)
    })

    expect(result.current.editMode).toBe(true)
  })
})
