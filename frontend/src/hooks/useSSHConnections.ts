import { useCallback, useState } from 'react'
import { SSHConfigHostsResponseSchema, SSHConnectionEntriesResponseSchema } from '../schemas'
import type { SSHConnectionEntry, SSHConnectionRequest } from '../schemas'

// The task dashboard's hosts: config.yaml's ssh_connections (issue #272),
// managed through /api/config/ssh-connections. This is not ~/.ssh/config,
// which the pane settings' Add SSH Host writes; the ~/.ssh/config host names
// are read here only to offer them as names for an entry.

const BASE = '/api/config/ssh-connections'

export interface SSHConnectionsState {
  /** The entries, sorted by name; null until the first load succeeds. */
  connections: SSHConnectionEntry[] | null
  /** The Host names in ~/.ssh/config; empty when it could not be read. */
  sshConfigNames: string[]
  loading: boolean
  /** Why the last load failed, or null. */
  error: string | null
  load: () => Promise<void>
  /** Each of these resolves to why it failed, or null, and reloads on success. */
  create: (request: SSHConnectionRequest) => Promise<string | null>
  update: (name: string, request: SSHConnectionRequest) => Promise<string | null>
  remove: (name: string) => Promise<string | null>
}

// failureReason is the server's reason for a refusal: the `error` of a JSON
// body, the text of a plain one, or the status.
async function failureReason(res: Response): Promise<string> {
  const text = (await res.text()).trim()
  try {
    const body = JSON.parse(text) as { error?: unknown }
    if (typeof body.error === 'string' && body.error !== '') return body.error
  } catch {
    // Not JSON: http.Error answers in plain text.
  }
  return text || `HTTP ${res.status}`
}

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

export function useSSHConnections(): SSHConnectionsState {
  const [connections, setConnections] = useState<SSHConnectionEntry[] | null>(null)
  const [sshConfigNames, setSSHConfigNames] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [entries, hosts] = await Promise.all([fetch(BASE), fetch('/api/ssh-config/hosts')])
      if (!entries.ok) {
        setError(await failureReason(entries))
      } else {
        const parsed = SSHConnectionEntriesResponseSchema.safeParse(await entries.json())
        if (parsed.success) {
          setConnections(parsed.data.connections)
          setError(null)
        } else {
          setError('unexpected response from the server')
        }
      }
      const parsedHosts = hosts.ok ? SSHConfigHostsResponseSchema.safeParse(await hosts.json()) : null
      setSSHConfigNames(parsedHosts?.success ? parsedHosts.data.hosts.map((h) => h.name) : [])
    } catch (err) {
      setError(messageOf(err))
    } finally {
      setLoading(false)
    }
  }, [])

  const send = useCallback(
    async (method: string, path: string, body?: SSHConnectionRequest): Promise<string | null> => {
      try {
        const res = await fetch(path, {
          method,
          ...(body ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : {}),
        })
        if (!res.ok) return await failureReason(res)
      } catch (err) {
        return messageOf(err)
      }
      await load()
      return null
    },
    [load],
  )

  const create = useCallback((request: SSHConnectionRequest) => send('POST', BASE, request), [send])
  const update = useCallback(
    (name: string, request: SSHConnectionRequest) => send('PUT', `${BASE}/${encodeURIComponent(name)}`, request),
    [send],
  )
  const remove = useCallback((name: string) => send('DELETE', `${BASE}/${encodeURIComponent(name)}`), [send])

  return { connections, sshConfigNames, loading, error, load, create, update, remove }
}
