import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { TASKS_POLL_INTERVAL_MS, useTasks } from './useTasks'

const payload = {
  hosts: [{ name: '', status: 'ok', collected_at: '2026-09-25T12:00:00Z' }],
  tasks: [
    {
      id: 'local:claude:a',
      host: '',
      agent: 'claude',
      state: 'wait',
      location: { kind: 'tmux', tmux_session: 'task-a', attachable: true },
    },
  ],
}

function ok(body: unknown): Response {
  return { ok: true, status: 200, json: () => Promise.resolve(body) } as Response
}

function setVisibility(value: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value })
}

describe('useTasks', () => {
  beforeEach(() => setVisibility('visible'))

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('fetches tasks when enabled and records when it did', async () => {
    window.fetch = vi.fn().mockResolvedValue(ok(payload))

    const { result } = renderHook(() => useTasks(true))

    await waitFor(() => expect(result.current.data?.tasks).toHaveLength(1))
    expect(window.fetch).toHaveBeenCalledWith('/api/tasks')
    expect(result.current.error).toBeNull()
    expect(result.current.updatedAt).not.toBeNull()
  })

  it('does not fetch while disabled', () => {
    window.fetch = vi.fn()
    renderHook(() => useTasks(false))
    expect(window.fetch).not.toHaveBeenCalled()
  })

  it('polls while enabled and visible, and stops when disabled', async () => {
    vi.useFakeTimers()
    window.fetch = vi.fn().mockResolvedValue(ok(payload))

    const { rerender } = renderHook(({ enabled }) => useTasks(enabled), { initialProps: { enabled: true } })
    await act(async () => {})
    expect(window.fetch).toHaveBeenCalledTimes(1)

    await act(async () => {
      vi.advanceTimersByTime(TASKS_POLL_INTERVAL_MS)
    })
    expect(window.fetch).toHaveBeenCalledTimes(2)

    rerender({ enabled: false })
    await act(async () => {
      vi.advanceTimersByTime(TASKS_POLL_INTERVAL_MS * 3)
    })
    expect(window.fetch).toHaveBeenCalledTimes(2)
  })

  it('pauses while the page is hidden', async () => {
    vi.useFakeTimers()
    window.fetch = vi.fn().mockResolvedValue(ok(payload))

    renderHook(() => useTasks(true))
    await act(async () => {})
    setVisibility('hidden')
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    await act(async () => {
      vi.advanceTimersByTime(TASKS_POLL_INTERVAL_MS * 2)
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)

    setVisibility('visible')
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(window.fetch).toHaveBeenCalledTimes(2)
  })

  it('keeps the last result and reports an error when a fetch fails', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(ok(payload))
    window.fetch = fetchMock
    const { result } = renderHook(() => useTasks(true))
    await waitFor(() => expect(result.current.data).not.toBeNull())

    fetchMock.mockResolvedValueOnce({ ok: false, status: 500 } as Response)
    await act(async () => {
      await result.current.refresh()
    })
    expect(result.current.error).toBe('HTTP 500')
    expect(result.current.data?.tasks).toHaveLength(1)

    fetchMock.mockResolvedValueOnce(ok({ hosts: [], tasks: [{ id: 'x' }] }))
    await act(async () => {
      await result.current.refresh()
    })
    expect(result.current.error).toMatch(/Unexpected response/)

    fetchMock.mockRejectedValueOnce(new Error('network down'))
    await act(async () => {
      await result.current.refresh()
    })
    expect(result.current.error).toBe('network down')

    fetchMock.mockResolvedValueOnce(ok(payload))
    await act(async () => {
      await result.current.refresh()
    })
    expect(result.current.error).toBeNull()
  })

  it('does not start a second request while one is in flight', async () => {
    let resolve: (value: Response) => void = () => {}
    window.fetch = vi.fn().mockImplementation(() => new Promise<Response>((r) => { resolve = r }))
    const { result } = renderHook(() => useTasks(true))
    expect(result.current.loading).toBe(true)

    await act(async () => {
      void result.current.refresh()
    })
    expect(window.fetch).toHaveBeenCalledTimes(1)

    await act(async () => {
      resolve(ok(payload))
    })
    expect(result.current.loading).toBe(false)
  })

  it('reconnects a host and then refreshes', async () => {
    const fetchMock = vi.fn().mockResolvedValue(ok(payload))
    window.fetch = fetchMock
    const { result } = renderHook(() => useTasks(true))
    await waitFor(() => expect(result.current.data).not.toBeNull())
    fetchMock.mockClear()

    fetchMock.mockResolvedValueOnce({ ok: true, status: 204 } as Response)
    await act(async () => {
      await result.current.reconnect('gpu box/1')
    })
    expect(fetchMock.mock.calls[0]).toEqual(['/api/tasks/hosts/gpu%20box%2F1/reconnect', { method: 'POST' }])
    expect(fetchMock.mock.calls[1]).toEqual(['/api/tasks'])
  })

  // efficacy:exempt unchanged by this branch; the new describe block after it falls inside its line range
  it('reports a failed reconnect', async () => {
    const fetchMock = vi.fn().mockResolvedValue(ok(payload))
    window.fetch = fetchMock
    const { result } = renderHook(() => useTasks(true))
    await waitFor(() => expect(result.current.data).not.toBeNull())

    fetchMock.mockResolvedValueOnce({ ok: false, status: 404 } as Response)
    await act(async () => {
      await result.current.reconnect('gone')
    })
    expect(result.current.error).toBe('Reconnect gone failed: HTTP 404')
  })
})

describe('useTasks saveRecord', () => {
  const recordPayload = {
    hosts: [{ name: '', status: 'ok' }],
    tasks: [
      { ...payload.tasks[0], session_id: 's1' },
      { ...payload.tasks[0], id: 'local:claude:b', session_id: 's2' },
    ],
  }

  beforeEach(() => setVisibility('visible'))
  afterEach(() => vi.restoreAllMocks())

  async function loaded() {
    const hook = renderHook(() => useTasks(true))
    await waitFor(() => expect(hook.result.current.data?.tasks).toHaveLength(2))
    return hook
  }

  it('PUTs the record and applies the answer to the task at once', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(ok(recordPayload)).mockResolvedValueOnce(
      ok({ host: '', agent: 'claude', session_id: 's1', done: true, labels: ['payment'] }),
    )
    window.fetch = fetchMock
    const { result } = await loaded()

    let error: string | null = 'unset'
    await act(async () => {
      error = await result.current.saveRecord(result.current.data!.tasks[0], { done: true, labels: ['payment '] })
    })

    expect(error).toBeNull()
    expect(fetchMock).toHaveBeenLastCalledWith('/api/tasks/records', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ host: '', agent: 'claude', session_id: 's1', done: true, labels: ['payment '] }),
    })
    expect(result.current.data!.tasks[0]).toMatchObject({ done: true, labels: ['payment'] })
    expect(result.current.data!.tasks[1].done).toBeUndefined()
  })

  it('reports what the server refused, and changes nothing', async () => {
    window.fetch = vi.fn().mockResolvedValueOnce(ok(recordPayload)).mockResolvedValueOnce({
      ok: false,
      status: 400,
      text: () => Promise.resolve('invalid task record: label "x" is longer than 32 characters\n'),
    } as Response)
    const { result } = await loaded()

    let error: string | null = null
    await act(async () => {
      error = await result.current.saveRecord(result.current.data!.tasks[0], { done: false, labels: ['x'] })
    })

    expect(error).toBe('invalid task record: label "x" is longer than 32 characters')
    expect(result.current.data!.tasks[0].labels).toBeUndefined()
  })

  it('reports a network failure and an unexpected answer', async () => {
    window.fetch = vi.fn()
      .mockResolvedValueOnce(ok(recordPayload))
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(ok({ unexpected: true }))
    const { result } = await loaded()

    const errors: Array<string | null> = []
    await act(async () => {
      errors.push(await result.current.saveRecord(result.current.data!.tasks[0], { done: true, labels: [] }))
      errors.push(await result.current.saveRecord(result.current.data!.tasks[0], { done: true, labels: [] }))
    })

    expect(errors).toEqual(['offline', 'Unexpected response from /api/tasks/records'])
    expect(result.current.data!.tasks[0].done).toBeUndefined()
  })

  it('refuses a task without a session id without asking the server', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(ok(recordPayload))
    window.fetch = fetchMock
    const { result } = await loaded()

    let error: string | null = null
    await act(async () => {
      error = await result.current.saveRecord({ ...result.current.data!.tasks[0], session_id: undefined },
        { done: true, labels: [] })
    })

    expect(error).toBe('This task has no session ID to record against')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  // A collection that was already running when the record was saved read the
  // records before the save, so its answer would put the old record back.
  it('drops a collection that started before a save', async () => {
    let resolveSlow: (value: Response) => void = () => {}
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(ok(recordPayload))
      .mockImplementationOnce(() => new Promise<Response>((resolve) => { resolveSlow = resolve }))
      .mockResolvedValueOnce(ok({ host: '', agent: 'claude', session_id: 's1', done: true, labels: [] }))
    window.fetch = fetchMock
    const { result } = await loaded()

    let pending: Promise<void> = Promise.resolve()
    act(() => {
      pending = result.current.refresh()
    })
    await act(async () => {
      await result.current.saveRecord(result.current.data!.tasks[0], { done: true, labels: [] })
    })
    await act(async () => {
      resolveSlow(ok(recordPayload))
      await pending
    })

    expect(result.current.data!.tasks[0].done).toBe(true)
  })
})

describe('useTasks launch and resume', () => {
  const launched = {
    id: 'local:claude:0f0e0d0c-0b0a-4908-8706-050403020100',
    session_id: '0f0e0d0c-0b0a-4908-8706-050403020100',
    tmux_session: 'task-0f0e0d0c',
  }
  const stopped = {
    ...payload.tasks[0],
    id: 'local:claude:5d7e3a90-1b2c-4d3e-8f40-51627384a5b6',
    session_id: '5d7e3a90-1b2c-4d3e-8f40-51627384a5b6',
    state: 'stop',
    location: { kind: 'none', attachable: false },
  }
  const refused = (status: number, text: string) =>
    ({ ok: false, status, text: () => Promise.resolve(text) }) as Response

  beforeEach(() => setVisibility('visible'))
  afterEach(() => vi.restoreAllMocks())

  it('POSTs a new task, answers with what was started, and collects again', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(ok(payload))
      .mockResolvedValueOnce({ ...ok({ ...launched, labels: ['payment'] }), status: 201 } as Response)
      .mockResolvedValueOnce(ok(payload))
    window.fetch = fetchMock
    const { result } = renderHook(() => useTasks(true))
    await waitFor(() => expect(result.current.data).not.toBeNull())

    let outcome: Awaited<ReturnType<typeof result.current.launch>> | null = null
    await act(async () => {
      outcome = await result.current.launch({ host: 'build-box', cwd: '/workspace/user/project', prompt: 'go', labels: ['payment'] })
    })

    expect(outcome).toEqual({ ok: true, launched: { ...launched, labels: ['payment'] } })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/tasks', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ host: 'build-box', agent: 'claude', cwd: '/workspace/user/project', prompt: 'go', labels: ['payment'] }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/tasks')
  })

  it('reports a refused launch, a network failure and an unexpected answer, without collecting', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(ok(payload))
      .mockResolvedValueOnce(refused(409, 'the working directory does not exist on the host\n'))
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(ok({ unexpected: true }))
      .mockResolvedValueOnce(refused(502, ''))
    window.fetch = fetchMock
    const { result } = renderHook(() => useTasks(true))
    await waitFor(() => expect(result.current.data).not.toBeNull())

    const outcomes: unknown[] = []
    await act(async () => {
      for (let i = 0; i < 4; i++) {
        outcomes.push(await result.current.launch({ host: '', cwd: '/w', prompt: 'go', labels: [] }))
      }
    })

    expect(outcomes).toEqual([
      { ok: false, error: 'the working directory does not exist on the host' },
      { ok: false, error: 'offline' },
      { ok: false, error: 'Unexpected response from /api/tasks' },
      { ok: false, error: 'HTTP 502' },
    ])
    expect(fetchMock).toHaveBeenCalledTimes(5)
  })

  it('POSTs a resume for a stopped task and collects again', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(ok({ ...payload, tasks: [stopped] }))
      .mockResolvedValueOnce(ok({ ...launched, id: stopped.id, session_id: stopped.session_id, tmux_session: 'task-5d7e3a90' }))
      .mockResolvedValueOnce(ok(payload))
    window.fetch = fetchMock
    const { result } = renderHook(() => useTasks(true))
    await waitFor(() => expect(result.current.data).not.toBeNull())

    let outcome: unknown = null
    await act(async () => {
      outcome = await result.current.resume(result.current.data!.tasks[0])
    })

    expect(outcome).toEqual({
      ok: true,
      launched: { id: stopped.id, session_id: stopped.session_id, tmux_session: 'task-5d7e3a90' },
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/tasks/resume', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ host: '', session_id: stopped.session_id }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/tasks')
  })

  it('refuses to resume a task without a session id, and reports a refused resume', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(ok(payload))
      .mockResolvedValueOnce(refused(409, "a tmux session with the task's name already exists on the host"))
      .mockResolvedValueOnce(ok({ id: '' }))
    window.fetch = fetchMock
    const { result } = renderHook(() => useTasks(true))
    await waitFor(() => expect(result.current.data).not.toBeNull())

    const outcomes: unknown[] = []
    await act(async () => {
      outcomes.push(await result.current.resume(payload.tasks[0] as never))
      outcomes.push(await result.current.resume(stopped as never))
      outcomes.push(await result.current.resume(stopped as never))
    })

    expect(outcomes).toEqual([
      { ok: false, error: 'This task has no session ID to resume' },
      { ok: false, error: "a tmux session with the task's name already exists on the host" },
      { ok: false, error: 'Unexpected response from /api/tasks/resume' },
    ])
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })
})

describe('useTasks collection after a launch', () => {
  beforeEach(() => setVisibility('visible'))
  afterEach(() => vi.restoreAllMocks())

  // A collection already running when a task was started began before the
  // task existed, so the launch's own collection runs once it finishes rather
  // than being dropped until the next poll.
  it('collects again after a collection that was in flight when the task started', async () => {
    let resolveSlow: (value: Response) => void = () => {}
    const withTask = { ...payload, tasks: [...payload.tasks, { ...payload.tasks[0], id: 'local:claude:new' }] }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(ok(payload))
      .mockImplementationOnce(() => new Promise<Response>((resolve) => { resolveSlow = resolve }))
      .mockResolvedValueOnce({ ...ok({ id: 'local:claude:new', session_id: 'new', tmux_session: 'task-new' }), status: 201 } as Response)
      .mockResolvedValueOnce(ok(withTask))
    window.fetch = fetchMock
    const { result } = renderHook(() => useTasks(true))
    await waitFor(() => expect(result.current.data).not.toBeNull())

    let slow: Promise<void> = Promise.resolve()
    act(() => {
      slow = result.current.refresh()
    })
    let launching: Promise<unknown> = Promise.resolve()
    await act(async () => {
      launching = result.current.launch({ host: '', cwd: '/w', prompt: 'go', labels: [] })
      await Promise.resolve()
    })
    await act(async () => {
      resolveSlow(ok(payload))
      await slow
      await launching
    })

    await waitFor(() => expect(result.current.data!.tasks.map((t) => t.id)).toContain('local:claude:new'))
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })
})
