import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { useSSHConnections } from './useSSHConnections'
import type { SSHConnectionEntry } from '../schemas'

const entry: SSHConnectionEntry = {
  name: 'build-box',
  host: 'build.invalid',
  user: 'demo',
  has_password: true,
  in_ssh_config: false,
  panes: ['build'],
}

function jsonResponse(status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(JSON.stringify(body)),
  }
}

// routes answers each request by "METHOD path"; anything else is a 404.
function stubFetch(routes: Record<string, () => unknown>) {
  const fetchMock = vi.fn((input: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${input}`
    const answer = routes[key]
    return Promise.resolve(answer ? answer() : jsonResponse(404, { error: `no route ${key}` }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const listRoutes = {
  'GET /api/config/ssh-connections': () => jsonResponse(200, { connections: [entry] }),
  'GET /api/ssh-config/hosts': () =>
    jsonResponse(200, { hosts: [{ name: 'gpu-box', hostname: 'gpu.invalid', user: 'demo' }] }),
}

beforeEach(() => {
  vi.restoreAllMocks()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useSSHConnections', () => {
  it('does not fetch until load is called', () => {
    const fetchMock = stubFetch(listRoutes)
    const { result } = renderHook(() => useSSHConnections())
    expect(result.current.connections).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('load reads the entries and the ~/.ssh/config host names', async () => {
    stubFetch(listRoutes)
    const { result } = renderHook(() => useSSHConnections())
    await act(() => result.current.load())
    expect(result.current.connections).toEqual([entry])
    expect(result.current.sshConfigNames).toEqual(['gpu-box'])
    expect(result.current.error).toBeNull()
  })

  it('load reports the server error and keeps going without ~/.ssh/config names', async () => {
    stubFetch({
      'GET /api/config/ssh-connections': () => jsonResponse(500, { error: 'boom' }),
      'GET /api/ssh-config/hosts': () => jsonResponse(500, 'failed to read ssh config'),
    })
    const { result } = renderHook(() => useSSHConnections())
    await act(() => result.current.load())
    expect(result.current.error).toBe('boom')
    expect(result.current.sshConfigNames).toEqual([])
  })

  it('load rejects a response that does not match the schema', async () => {
    stubFetch({
      ...listRoutes,
      'GET /api/config/ssh-connections': () => jsonResponse(200, { connections: [{ name: '' }] }),
    })
    const { result } = renderHook(() => useSSHConnections())
    await act(() => result.current.load())
    expect(result.current.connections).toBeNull()
    expect(result.current.error).toMatch(/unexpected response/)
  })

  it('create posts the body and reloads', async () => {
    const fetchMock = stubFetch({
      ...listRoutes,
      'POST /api/config/ssh-connections': () => jsonResponse(201, { ...entry, name: 'gpu-box' }),
    })
    const { result } = renderHook(() => useSSHConnections())
    let error: string | null = 'unset'
    await act(async () => {
      error = await result.current.create({ name: 'gpu-box' })
    })
    expect(error).toBeNull()
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({ name: 'gpu-box' })
    expect(result.current.connections).toEqual([entry])
  })

  it('create resolves to the server reason on a refusal', async () => {
    stubFetch({
      ...listRoutes,
      'POST /api/config/ssh-connections': () => jsonResponse(409, { error: 'ssh connection "x" already exists' }),
    })
    const { result } = renderHook(() => useSSHConnections())
    let error: string | null = null
    await act(async () => {
      error = await result.current.create({ name: 'x' })
    })
    expect(error).toBe('ssh connection "x" already exists')
  })

  it('update puts to the entry path, escaping the name', async () => {
    const fetchMock = stubFetch({
      ...listRoutes,
      'PUT /api/config/ssh-connections/a%2Fb': () => jsonResponse(200, entry),
    })
    const { result } = renderHook(() => useSSHConnections())
    let error: string | null = 'unset'
    await act(async () => {
      error = await result.current.update('a/b', { host: 'h.invalid', clear_password: true })
    })
    expect(error).toBeNull()
    const put = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({ host: 'h.invalid', clear_password: true })
  })

  it('remove deletes and reports a refusal', async () => {
    stubFetch({
      ...listRoutes,
      'DELETE /api/config/ssh-connections/build-box': () =>
        jsonResponse(409, { error: 'ssh connection "build-box" is used by pane build' }),
    })
    const { result } = renderHook(() => useSSHConnections())
    let error: string | null = null
    await act(async () => {
      error = await result.current.remove('build-box')
    })
    expect(error).toBe('ssh connection "build-box" is used by pane build')
  })

  it('a network failure is reported as its message', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
    const { result } = renderHook(() => useSSHConnections())
    let error: string | null = null
    await act(async () => {
      error = await result.current.remove('x')
    })
    expect(error).toBe('offline')
  })

  it('a refusal with a plain-text body is reported as its text', async () => {
    stubFetch({
      'DELETE /api/config/ssh-connections/x': () => ({
        ok: false,
        status: 403,
        json: () => Promise.reject(new Error('not json')),
        text: () => Promise.resolve('cross-site request refused\n'),
      }),
    })
    const { result } = renderHook(() => useSSHConnections())
    let error: string | null = null
    await act(async () => {
      error = await result.current.remove('x')
    })
    expect(error).toBe('cross-site request refused')
  })
})
