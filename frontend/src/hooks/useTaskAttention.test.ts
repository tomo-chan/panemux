import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { TASK_ATTENTION_POLL_INTERVAL_MS, useTaskAttention } from './useTaskAttention'
import { createTaskAttentionTracker } from '../utils/taskAttention'
import type { TasksResponse, Workspace } from '../schemas'

const waitTask = {
  id: 'local:claude:a',
  host: '',
  agent: 'claude',
  state: 'wait',
  session_id: 'a',
  wait_signature: 'w1-a',
  status_since: '2026-10-01T10:00:00Z',
  location: { kind: 'tmux', tmux_session: 'task-a', attachable: true },
}

const attentionPayload = {
  hosts: [{ name: '', status: 'ok', collected_at: '2026-10-01T10:00:05Z' }],
  tasks: [waitTask],
}

const workspaces: Workspace[] = [
  {
    id: 'dev',
    title: 'Dev',
    layout: { direction: 'horizontal', children: [{ size: 100, pane: { id: 'p1', type: 'tmux', tmux_session: 'task-a' } }] },
  },
]

function ok(body: unknown): Response {
  return { ok: true, status: 200, json: () => Promise.resolve(body) } as Response
}

function setVisibility(value: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value })
  document.dispatchEvent(new Event('visibilitychange'))
}

function memoryTracker() {
  const data = new Map<string, string>()
  const storage = {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => void data.set(key, value),
  } as Storage
  return createTaskAttentionTracker(() => storage)
}

interface Props {
  dashboardShown: boolean
  dashboardData: TasksResponse | null
  dashboardUpdatedAt: number | null
}

function render(initial: Partial<Props> = {}, tracker = memoryTracker()) {
  const onTaskWaits = vi.fn()
  const hook = renderHook(
    (props: Props) => useTaskAttention({ ...props, workspaces, onTaskWaits, tracker }),
    { initialProps: { dashboardShown: false, dashboardData: null, dashboardUpdatedAt: null, ...initial } },
  )
  return { ...hook, onTaskWaits }
}

describe('useTaskAttention', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    setVisibility('visible')
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('collects running tasks while the dashboard is not shown, and reports a new wait with its pane', async () => {
    window.fetch = vi.fn().mockResolvedValue(ok(attentionPayload))
    const { onTaskWaits } = render()
    await act(async () => {})

    expect(window.fetch).toHaveBeenCalledWith('/api/tasks/attention', expect.anything())
    expect(onTaskWaits).toHaveBeenCalledTimes(1)
    expect(onTaskWaits.mock.calls[0][0]).toEqual([
      { task: expect.objectContaining({ id: 'local:claude:a' }), pane: expect.objectContaining({ paneId: 'p1', workspaceId: 'dev' }), notify: true },
    ])
  })

  it('collects again every interval, and does not report the same wait twice', async () => {
    window.fetch = vi.fn().mockResolvedValue(ok(attentionPayload))
    const { onTaskWaits } = render()
    await act(async () => {})
    await act(async () => {
      vi.advanceTimersByTime(TASK_ATTENTION_POLL_INTERVAL_MS)
    })

    expect(window.fetch).toHaveBeenCalledTimes(2)
    expect(onTaskWaits).toHaveBeenCalledTimes(1)
  })

  it('keeps collecting while the page is hidden', async () => {
    window.fetch = vi.fn().mockResolvedValue(ok({ ...attentionPayload, tasks: [] }))
    render()
    await act(async () => {})
    setVisibility('hidden')
    await act(async () => {
      vi.advanceTimersByTime(TASK_ATTENTION_POLL_INTERVAL_MS)
    })
    expect(window.fetch).toHaveBeenCalledTimes(2)
  })

  it('does not start a collection while the previous one is still running', async () => {
    window.fetch = vi.fn().mockReturnValue(new Promise(() => {}))
    render()
    await act(async () => {
      vi.advanceTimersByTime(TASK_ATTENTION_POLL_INTERVAL_MS * 3)
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)
  })

  it('leaves collection to the dashboard while it is shown on a visible page, and uses its snapshots', async () => {
    window.fetch = vi.fn()
    const { onTaskWaits, rerender } = render({ dashboardShown: true })
    await act(async () => {
      vi.advanceTimersByTime(TASK_ATTENTION_POLL_INTERVAL_MS * 2)
    })
    expect(window.fetch).not.toHaveBeenCalled()

    rerender({ dashboardShown: true, dashboardData: attentionPayload as TasksResponse, dashboardUpdatedAt: Date.now() })
    expect(onTaskWaits).toHaveBeenCalledTimes(1)
  })

  it('collects on its own while the dashboard is shown on a hidden page', async () => {
    window.fetch = vi.fn().mockResolvedValue(ok({ ...attentionPayload, tasks: [] }))
    render({ dashboardShown: true })
    setVisibility('hidden')
    await act(async () => {})
    expect(window.fetch).toHaveBeenCalledTimes(1)
  })

  it('drops a collection that was running when the dashboard took over', async () => {
    let resolve: (value: Response) => void = () => {}
    window.fetch = vi.fn().mockReturnValue(new Promise<Response>((r) => { resolve = r }))
    const { onTaskWaits, rerender } = render()
    rerender({ dashboardShown: true, dashboardData: null, dashboardUpdatedAt: null })
    await act(async () => {
      resolve(ok(attentionPayload))
    })
    expect(onTaskWaits).not.toHaveBeenCalled()
  })

  it('does not apply the dashboard’s old snapshot again when it is shown once more', async () => {
    window.fetch = vi.fn().mockResolvedValue(ok({ ...attentionPayload, tasks: [] }))
    const old = Date.now() - 5000
    const { onTaskWaits, rerender } = render({ dashboardShown: false, dashboardData: attentionPayload as TasksResponse, dashboardUpdatedAt: old })
    await act(async () => {
      vi.advanceTimersByTime(1000)
    })
    rerender({ dashboardShown: true, dashboardData: attentionPayload as TasksResponse, dashboardUpdatedAt: old })
    expect(onTaskWaits).not.toHaveBeenCalled()
  })

  it('reports nothing when the collection fails or is malformed', async () => {
    window.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: false, status: 500 } as Response)
      .mockResolvedValueOnce(ok({ hosts: 'nope' }))
      .mockRejectedValueOnce(new Error('offline'))
    const { onTaskWaits } = render()
    await act(async () => {})
    for (let i = 0; i < 2; i += 1) {
      await act(async () => {
        vi.advanceTimersByTime(TASK_ATTENTION_POLL_INTERVAL_MS)
      })
    }
    expect(window.fetch).toHaveBeenCalledTimes(3)
    expect(onTaskWaits).not.toHaveBeenCalled()
  })

  it('stops collecting when unmounted', async () => {
    window.fetch = vi.fn().mockResolvedValue(ok({ ...attentionPayload, tasks: [] }))
    const { unmount } = render()
    await act(async () => {})
    unmount()
    await act(async () => {
      vi.advanceTimersByTime(TASK_ATTENTION_POLL_INTERVAL_MS * 2)
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)
  })
})
