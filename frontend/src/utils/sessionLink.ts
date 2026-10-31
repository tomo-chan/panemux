import type { Task } from '../schemas'

export type SessionKey = Pick<Task, 'host' | 'agent'> & { session_id: string }
type ParsedLink = { kind: 'none' | 'invalid' } | { kind: 'valid'; key: SessionKey }
const fields = ['session_host', 'session_agent', 'session_id'] as const
const agentID = /^[a-z][a-z0-9]*(-[a-z0-9]+)*$/

export function parseSessionLink(search: string): ParsedLink {
  const params = new URLSearchParams(search)
  if (!fields.some((field) => params.has(field))) return { kind: 'none' }
  try {
    // URLSearchParams replaces malformed UTF-8; validate before using it.
    decodeURIComponent(search.replace(/\+/g, ' '))
    if (fields.some((field) => params.getAll(field).length !== 1)) return { kind: 'invalid' }
    const [host, agent, session_id] = fields.map((field) => params.get(field)!)
    if (!agentID.test(agent) || !session_id) return { kind: 'invalid' }
    return { kind: 'valid', key: { host, agent, session_id } }
  } catch {
    return { kind: 'invalid' }
  }
}

export function sessionLinkUrl(base: string, task: Pick<Task, 'host' | 'agent' | 'session_id'>): string | null {
  if (!task.session_id || !agentID.test(task.agent)) return null
  const url = new URL(base)
  url.username = url.password = url.search = url.hash = ''
  for (const [field, value] of fields.map((field, i) => [field, [task.host, task.agent, task.session_id!][i]])) {
    url.searchParams.set(field, value)
  }
  return url.href
}

export function stripSessionLink(base: string): string {
  const url = new URL(base)
  fields.forEach((field) => url.searchParams.delete(field))
  return url.href
}

export function sameSession(a: Pick<Task, 'host' | 'agent' | 'session_id'>, b: SessionKey): boolean {
  return a.host === b.host && a.agent === b.agent && a.session_id === b.session_id
}
