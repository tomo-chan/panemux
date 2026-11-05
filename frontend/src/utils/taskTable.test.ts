import { describe, expect, it } from 'vitest'
import type { Task, TaskSort } from '../schemas'
import { elapsedSeconds, formatElapsed, groupIntoLanes, listedTasks } from './taskBoard'
import { nextTaskSort, sortTaskGroups, sortTasks } from './taskTable'

const NOW = Date.parse('2026-11-01T12:00:00Z')
const task = (id: string, overrides: Partial<Task> = {}): Task => ({
  id, host: '', agent: 'claude', state: 'busy',
  location: { kind: 'none', attachable: false }, ...overrides,
})
const ids = (tasks: Task[]) => tasks.map((t) => t.id)
const sort = (tasks: Task[], key: TaskSort['key'], direction: TaskSort['direction'] = 'asc') =>
  ids(sortTasks(tasks, { key, direction }, NOW, [{ id: 'claude', display_name: 'Zed', capabilities: [], notes: {} }]))

describe('task table sorting', () => {
  it.each(['session', 'host', 'agent', 'repository', 'branch'] as const)('uses natural case-insensitive %s values and fixed id ties', (key) => {
    const value = (id: string, text: string) => task(id, {
      cwd: `/workspace/${text}`, host: text, agent: text, git: { repo: text, branch: text },
    })
    const tasks = [value('z', 'Item2'), value('a', 'item2'), value('b', 'item10')]
    expect(sort(tasks, key)).toEqual(['a', 'z', 'b'])
    expect(sort(tasks, key, 'desc')).toEqual(['b', 'a', 'z'])
    expect(ids(tasks)).toEqual(['z', 'a', 'b'])
  })

  it.each(['repository', 'branch'] as const)('keeps missing %s last in both directions', (key) => {
    const tasks = [task('z'), task('b', { git: { repo: 'repo2', branch: 'branch2' } }), task('a', { git: { repo: 'repo1', branch: 'branch1' } }), task('y', { git: { repo: '', branch: '' } })]
    expect(sort(tasks, key)).toEqual(['a', 'b', 'y', 'z'])
    expect(sort(tasks, key, 'desc')).toEqual(['b', 'a', 'y', 'z'])
  })

  it('sorts display titles, Local and registered agent names with unknown agent ids as fallback', () => {
    expect(sort([task('a', { cwd: '/workspace/zeta' }), task('b', { session_id: '123456789' })], 'session')).toEqual(['b', 'a'])
    expect(sort([task('a'), task('b', { host: 'build-box' })], 'host')).toEqual(['b', 'a'])
    expect(sort([task('a'), task('b', { agent: 'cursor' })], 'agent')).toEqual(['b', 'a'])
    expect(sort([task('z', { agent: '' }), task('a', { agent: 'cursor' })], 'agent', 'desc')).toEqual(['a', 'z'])
  })

  it('uses board state order, stable ties for run/unknown, and the stopped-only Done rule', () => {
    const tasks = [task('done', { state: 'stop', done: true }), task('stop', { state: 'stop' }), task('z-run', { state: 'run' }), task('a-unknown', { state: 'unknown' }), task('idle', { state: 'idle' }), task('busy', { done: true }), task('wait', { state: 'wait', done: true })]
    expect(sort(tasks, 'state')).toEqual(['wait', 'busy', 'idle', 'a-unknown', 'z-run', 'stop', 'done'])
    expect(sort(tasks, 'state', 'desc')).toEqual(['done', 'stop', 'a-unknown', 'z-run', 'idle', 'busy', 'wait'])
  })

  it('compares elapsed numbers using one clock, with unknowns last and future times clamped', () => {
    const tasks = [task('z-missing', { started_at: '2026-11-01T01:00:00Z' }), task('y-invalid', { status_since: 'invalid' }), task('hours', { status_since: '2026-11-01T10:00:00Z' }), task('minutes', { status_since: '2026-11-01T11:48:00Z' }), task('future', { status_since: '2026-11-02T12:00:00Z' })]
    expect(sort(tasks, 'elapsed')).toEqual(['future', 'minutes', 'hours', 'y-invalid', 'z-missing'])
    expect(sort(tasks, 'elapsed', 'desc')).toEqual(['hours', 'minutes', 'future', 'y-invalid', 'z-missing'])
    expect(formatElapsed(tasks[2].status_since, NOW)).toBe('2h')
    expect(formatElapsed(tasks[3].status_since, NOW)).toBe('12m')
    expect(elapsedSeconds(undefined, NOW)).toBeNull()
    expect(elapsedSeconds('invalid', NOW)).toBeNull()
    expect(elapsedSeconds(tasks[4].status_since, NOW)).toBe(0)
    expect(formatElapsed(tasks[4].status_since, NOW)).toBe('just now')
  })

  it('selects initial directions and toggles only the current key', () => {
    for (const key of ['session', 'state', 'host', 'agent', 'repository', 'branch'] as const) {
      expect(nextTaskSort({ key: 'elapsed', direction: 'asc' }, key)).toEqual({ key, direction: 'asc' })
      expect(nextTaskSort({ key, direction: 'asc' }, key)).toEqual({ key, direction: 'desc' })
      expect(nextTaskSort({ key, direction: 'desc' }, key)).toEqual({ key, direction: 'asc' })
    }
    expect(nextTaskSort({ key: 'state', direction: 'asc' }, 'elapsed')).toEqual({ key: 'elapsed', direction: 'desc' })
  })

  it.each(['label', 'repo'] as const)('preserves %s group keys, order, catch-all and duplicated rows without mutation', (mode) => {
    const tasks = [task('z', { labels: ['No label', 'review'], git: { repo: 'Not in a Git repository' } }), task('a', { labels: ['review'], git: { repo: 'Not in a Git repository' } }), task('missing')]
    const groups = groupIntoLanes(tasks, mode)
    const before = structuredClone(groups)
    const sorted = sortTaskGroups(groups, { key: 'state', direction: 'asc' }, NOW)
    expect(sorted.map((g) => g.key)).toEqual(groups.map((g) => g.key))
    expect(sorted.at(-1)?.tasks[0].id).toBe('missing')
    expect(sorted.find((g) => g.tasks.length === 2)?.tasks.map((t) => t.id)).toEqual(['a', 'z'])
    expect(groups).toEqual(before)
  })
})

describe('shared listed tasks', () => {
  it('combines filters and Show done before grouping without hiding restarted done tasks', () => {
    const tasks = [task('done', { state: 'stop', done: true, labels: ['review'] }), task('running', { done: true, labels: ['review'] }), task('other', { host: 'box', labels: ['review'] }), task('unlabeled')]
    const filter = { query: '', host: '', label: 'review' }
    expect(ids(listedTasks(tasks, filter, false))).toEqual(['running'])
    expect(ids(listedTasks(tasks, filter, true))).toEqual(['done', 'running'])
    expect(listedTasks(tasks, { ...filter, query: 'absent' }, true)).toEqual([])
    expect(ids(tasks)).toEqual(['done', 'running', 'other', 'unlabeled'])
  })
})
