import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Task, TaskHost, Workspace } from '../schemas'
import type { TaskOpenAction, TaskPaneRef } from '../utils/taskBoard'
import { useSessionLink } from './useSessionLink'

const state = vi.hoisted(() => ({ value: {} as Record<string, unknown>, enabled: false }))
vi.mock('./useTasks', () => ({ useTasks: (enabled: boolean) => { state.enabled = enabled; return state.value } }))
const query = '?session_host=&session_agent=claude&session_id=s1'
const task: Task = { id: 't1', host: '', agent: 'claude', session_id: 's1', state: 'unknown', location: { kind: 'tmux', tmux_session: 's1', attachable: true } }
const spaces: Workspace[] = [{ id: 'w1', title: 'Work', layout: { direction: 'horizontal', children: [{ size: 100, pane: { id: 'p1', type: 'tmux', tmux_session: 's1' } }] } }]
const opened = { paneId: 'p1', workspaceId: 'w1', paneTitle: 'P', workspaceTitle: 'Work' }
let open = vi.fn<(task: Task, action: TaskOpenAction, current: () => boolean) => Promise<TaskPaneRef | null>>()
let stopped = vi.fn<(task: Task) => void>()
function hook(workspaces: Workspace[] | null = spaces) {
  return useSessionLink(false, workspaces, null, open, stopped)
}
function collect(tasks: Task[] = [task], hosts: TaskHost[] = [{ name: '', status: 'ok' }]) {
  state.value = { ...state.value, data: { tasks, hosts }, loading: false }
}
beforeEach(() => {
  window.history.replaceState({}, '', '/' + query)
  state.value = { data: null, error: null, loading: false, refresh: vi.fn() }
  open = vi.fn().mockResolvedValue(opened)
  stopped = vi.fn()
})
afterEach(() => window.history.replaceState({}, '', '/'))

describe('session link navigation', () => {
  it('waits for tasks and layout, opens unknown by its location, and consumes once', async () => {
    let workspaces: Workspace[] | null = null
    const { result, rerender } = renderHook(() => hook(workspaces))
    expect(state.enabled).toBe(true)
    collect(); rerender()
    expect(open).not.toHaveBeenCalled()
    workspaces = spaces; rerender()
    await waitFor(() => expect(open).toHaveBeenCalledTimes(1))
    expect(open.mock.calls[0][1]).toEqual({ kind: 'goto', pane: expect.objectContaining({ paneId: 'p1', workspaceId: 'w1' }) })
    await waitFor(() => expect(result.current.notice).toBeNull())
    collect(); rerender()
    expect(open).toHaveBeenCalledTimes(1)
    expect(state.enabled).toBe(false)
    expect(window.location.search).toBe(query)
  })
  it.each(['stop', 'busy'] as const)('uses taskOpenAction for %s without resuming', async (status) => {
    const { result, rerender } = renderHook(() => hook())
    collect([{ ...task, state: status, location: status === 'stop' ? { kind: 'none', attachable: false } : task.location }]); rerender()
    await waitFor(() => expect(status === 'stop' ? stopped : open).toHaveBeenCalledTimes(1))
    if (status === 'stop') expect(result.current.notice).toContain('Resume manually')
  })
  it('creates a pane only through Open when no current pane matches', async () => {
    const { rerender } = renderHook(() => hook([]))
    collect(); rerender()
    await waitFor(() => expect(open.mock.calls[0][1]).toEqual({ kind: 'open' }))
  })
  it.each([
    { location: { kind: 'outside', attachable: false } },
    { location: { kind: 'daemon', attachable: false } },
    { location: { kind: 'tmux', attachable: false, tmux_session: 'bad name' } },
  ])('explains an unavailable location: %j', async (extra) => {
    const { result, rerender } = renderHook(() => hook([]))
    collect([{ ...task, ...extra } as Task]); rerender()
    await waitFor(() => expect(result.current.pending).toBe(false))
    expect(result.current.notice).toBeTruthy()
    expect(open).not.toHaveBeenCalled()
  })
  it('waits for connecting, distinguishes host failure, and retries with fresh data', async () => {
    const { result, rerender } = renderHook(() => hook())
    collect([], [{ name: '', status: 'connecting' }]); rerender()
    expect(result.current.pending).toBe(true)
    collect([], [{ name: '', status: 'error', error: 'offline' }]); rerender()
    await waitFor(() => expect(result.current.notice).toContain('offline'))
    act(() => result.current.retry())
    expect(result.current.pending).toBe(true)
    collect(); rerender()
    await waitFor(() => expect(open).toHaveBeenCalledTimes(1))
  })
  it('reports a missing or ambiguous session without opening', async () => {
    const { result, rerender } = renderHook(() => hook())
    collect([]); rerender()
    await waitFor(() => expect(result.current.notice).toBe('Session not found'))
    act(() => result.current.retry())
    collect([task, { ...task, id: 'duplicate' }]); rerender()
    await waitFor(() => expect(result.current.notice).toContain('Ambiguous'))
    expect(open).not.toHaveBeenCalled()
  })
  it('clears only for a different explicit selection, and reopens on popstate', async () => {
    const { result, rerender } = renderHook(() => hook())
    collect(); rerender()
    await waitFor(() => expect(result.current.notice).toBeNull())
    act(() => result.current.selectPane('p1'))
    expect(window.location.search).toBe(query)
    act(() => result.current.selectPane('other'))
    expect(window.location.search).toBe('')
    act(() => { window.history.replaceState({}, '', '/' + query); window.dispatchEvent(new PopStateEvent('popstate')) })
    expect(result.current.pending).toBe(true)
    expect(open).toHaveBeenCalledTimes(1)
    collect(); rerender()
    await waitFor(() => expect(open).toHaveBeenCalledTimes(2))
  })
  it('cancels a late creation without stealing focus', async () => {
    let current: (() => boolean) | undefined
    open.mockImplementation((_task, _action, isCurrent) => { current = isCurrent; return new Promise(() => {}) })
    const { result, rerender } = renderHook(() => hook([]))
    collect(); rerender()
    await waitFor(() => expect(open).toHaveBeenCalledOnce())
    expect(current?.()).toBe(true)
    act(() => result.current.clear())
    expect(current?.()).toBe(false)
    expect(result.current.notice).toBeNull()
  })
})
