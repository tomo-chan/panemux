// Working directory suggestions (issue #311): the directories the tasks on
// the board ran in, offered under the New task form's Working directory field
// for the chosen host. Nothing is stored — a directory leaves the suggestions
// when its last task leaves the board.

import type { Task } from '../schemas'
import { formatElapsed } from './taskBoard'

/** One suggested directory: the newest task that ran in it decides the rest. */
export interface RecentWorkdir {
  path: string
  agent: string
  running: boolean
  /** status_since of that task: when a stopped task's log last changed. */
  lastUsed?: string
}

// The server's rule for a task's working directory (session.validRemotePath):
// an absolute path with no shell metacharacters and no control characters. A
// directory it would refuse is not suggested.
// eslint-disable-next-line no-control-regex
const VALID_WORKDIR = /^(\/[^;|&$`'"<>()[\]{}!\\\x00-\x1f\x7f]*)+$/

export function isValidWorkdir(path: string): boolean {
  return VALID_WORKDIR.test(path)
}

function sinceMillis(w: RecentWorkdir): number {
  const at = w.lastUsed ? Date.parse(w.lastUsed) : Number.NaN
  return Number.isNaN(at) ? Number.NEGATIVE_INFINITY : at
}

/** Running beats stopped; between two of the same kind the later change wins. */
function newer(a: RecentWorkdir, b: RecentWorkdir): number {
  if (a.running !== b.running) return a.running ? -1 : 1
  return sinceMillis(b) - sinceMillis(a)
}

/**
 * The directories host's tasks ran in, once each: those of running tasks
 * first, then stopped ones, most recently used first, ties by path.
 */
export function recentWorkdirs(tasks: Task[], host: string): RecentWorkdir[] {
  const byPath = new Map<string, RecentWorkdir>()
  for (const task of tasks) {
    if (task.host !== host || !task.cwd || !isValidWorkdir(task.cwd)) continue
    const candidate: RecentWorkdir = {
      path: task.cwd,
      agent: task.agent,
      running: task.state !== 'stop',
      lastUsed: task.status_since,
    }
    const seen = byPath.get(task.cwd)
    if (!seen || newer(candidate, seen) < 0) byPath.set(task.cwd, candidate)
  }
  return [...byPath.values()].sort((a, b) => newer(a, b) || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))
}

/** The directories containing the typed text, ignoring case and surrounding space, in order. */
export function filterWorkdirs(list: RecentWorkdir[], query: string): RecentWorkdir[] {
  const q = query.trim().toLowerCase()
  if (q === '') return list
  return list.filter((w) => w.path.toLowerCase().includes(q))
}

/**
 * The path split around the first place the typed text appears, ignoring case.
 * A path whose lower case is not the same length (İ becomes two code units)
 * would be split in the wrong place, so it is not highlighted.
 */
export function highlightMatch(path: string, query: string): { text: string; match: boolean }[] {
  const q = query.trim().toLowerCase()
  const lower = path.toLowerCase()
  const at = q === '' || lower.length !== path.length ? -1 : lower.indexOf(q)
  if (at < 0) return [{ text: path, match: false }]
  const parts = [
    { text: path.slice(0, at), match: false },
    { text: path.slice(at, at + q.length), match: true },
    { text: path.slice(at + q.length), match: false },
  ]
  return parts.filter((part) => part.text !== '')
}

/** "now" for a running task, "2h ago" for a stopped one, "" when its time is unknown. */
export function lastUsedLabel(w: RecentWorkdir, now: number): string {
  if (w.running) return 'now'
  const age = formatElapsed(w.lastUsed, now)
  if (age === '' || age === 'just now') return age
  return `${age} ago`
}
