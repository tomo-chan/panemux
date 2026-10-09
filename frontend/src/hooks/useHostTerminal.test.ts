import { describe, it, expect, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { useHostTerminal } from './useHostTerminal'
import type { TaskActionResult } from './useTasks'
import type { HostTerminal } from '../schemas'

type Result = TaskActionResult<HostTerminal>

function deferred() {
  let resolve!: (value: Result) => void
  const promise = new Promise<Result>((r) => { resolve = r })
  return { promise, resolve }
}

const opened = (id: string, tmux = ''): Result => ({ ok: true, launched: { session_id: id, tmux_session: tmux } })

describe('useHostTerminal (issue #314)', () => {
  it('opens a host terminal and shows it once the server answers', async () => {
    const open = vi.fn().mockResolvedValue(opened('board-1'))
    const close = vi.fn().mockResolvedValue(undefined)
    const { result } = renderHook(() => useHostTerminal(open, close))

    await act(async () => result.current.open('gpu-box', 'ssh', undefined, null))

    expect(open).toHaveBeenCalledWith('gpu-box', 'ssh', undefined)
    expect(result.current.session).toMatchObject({
      host: 'gpu-box',
      type: 'ssh',
      attach: { phase: 'ready', sessionId: 'board-1', tmuxSession: '' },
      terminal: 'connecting',
    })
  })

  it('reports a refusal', async () => {
    const open = vi.fn().mockResolvedValue({ ok: false, error: 'connection is required' })
    const { result } = renderHook(() => useHostTerminal(open, vi.fn()))

    await act(async () => result.current.open('gpu-box', 'ssh', undefined, null))

    expect(result.current.session?.attach).toEqual({ phase: 'failed', error: 'connection is required' })
  })

  it('ends the terminal on close, and one that answers after the close at once', async () => {
    const late = deferred()
    const open = vi.fn().mockResolvedValueOnce(opened('board-1')).mockReturnValueOnce(late.promise)
    const close = vi.fn().mockResolvedValue(undefined)
    const { result } = renderHook(() => useHostTerminal(open, close))

    await act(async () => result.current.open('gpu-box', 'ssh', undefined, null))
    act(() => {
      result.current.close()
    })
    expect(close).toHaveBeenCalledWith('board-1')
    expect(result.current.session).toBeNull()

    act(() => {
      void result.current.open('gpu-box', 'ssh', undefined, null)
    })
    act(() => {
      result.current.close()
    })
    await act(async () => late.resolve(opened('board-2')))
    expect(close).toHaveBeenLastCalledWith('board-2')
    expect(result.current.session).toBeNull()
  })

  it('retries with a new terminal on the same tmux session, ending the old one', async () => {
    const open = vi.fn()
      .mockResolvedValueOnce(opened('board-1', 'gpu-box-0123abcd'))
      .mockResolvedValueOnce(opened('board-2', 'gpu-box-0123abcd'))
    const close = vi.fn().mockResolvedValue(undefined)
    const { result } = renderHook(() => useHostTerminal(open, close))

    await act(async () => result.current.open('gpu-box', 'ssh_tmux', 'gpu-box-0123abcd', null))
    act(() => result.current.setTerminal('ended'))
    await act(async () => result.current.retry())

    expect(close).toHaveBeenCalledWith('board-1')
    expect(open).toHaveBeenLastCalledWith('gpu-box', 'ssh_tmux', 'gpu-box-0123abcd')
    expect(result.current.session).toMatchObject({
      attach: { phase: 'ready', sessionId: 'board-2' },
      attempt: 1,
      terminal: 'connecting',
    })
  })

  it('opens nothing while a terminal is already open', async () => {
    const open = vi.fn().mockResolvedValue(opened('board-1'))
    const { result } = renderHook(() => useHostTerminal(open, vi.fn()))

    await act(async () => result.current.open('gpu-box', 'ssh', undefined, null))
    await act(async () => result.current.open('build-01', 'ssh', undefined, null))

    expect(open).toHaveBeenCalledTimes(1)
    expect(result.current.session?.host).toBe('gpu-box')
  })

  it('ends the terminal when the dashboard goes away, and when the page does', async () => {
    const open = vi.fn().mockResolvedValueOnce(opened('board-1')).mockResolvedValueOnce(opened('board-2'))
    const close = vi.fn().mockResolvedValue(undefined)
    const first = renderHook(() => useHostTerminal(open, close))
    await act(async () => first.result.current.open('gpu-box', 'ssh', undefined, null))
    first.unmount()
    expect(close).toHaveBeenCalledWith('board-1')

    const second = renderHook(() => useHostTerminal(open, close))
    await act(async () => second.result.current.open('gpu-box', 'ssh', undefined, null))
    act(() => {
      window.dispatchEvent(new Event('pagehide'))
    })
    expect(close).toHaveBeenLastCalledWith('board-2')
  })
})
