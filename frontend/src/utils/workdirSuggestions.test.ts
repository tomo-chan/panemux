import { describe, expect, it } from 'vitest'
import type { Task } from '../schemas'
import {
  filterWorkdirs,
  highlightMatch,
  isValidWorkdir,
  lastUsedLabel,
  recentWorkdirs,
  type RecentWorkdir,
} from './workdirSuggestions'

function task(fields: Partial<Task> & Pick<Task, 'id'>): Task {
  return { host: '', agent: 'claude', state: 'stop', location: { kind: 'none', attachable: false }, ...fields }
}

describe('isValidWorkdir', () => {
  it.each([
    ['/workspace/user/project', true],
    ['/', true],
    ['/workspace/user/my project', true],
    ['/workspace/user/#tag', true],
    ['/workspace/user/日本語', true],
    ['', false],
    ['relative/path', false],
    ['~/project', false],
    ['/workspace/a;b', false],
    ['/workspace/a|b', false],
    ['/workspace/a&b', false],
    ['/workspace/$HOME', false],
    ['/workspace/`id`', false],
    ["/workspace/it's", false],
    ['/workspace/"q"', false],
    ['/workspace/a<b', false],
    ['/workspace/a>b', false],
    ['/workspace/(a)', false],
    ['/workspace/[a]', false],
    ['/workspace/{a}', false],
    ['/workspace/a!', false],
    ['/workspace/a\\b', false],
    ['/workspace/a\nb', false],
    ['/workspace/a\tb', false],
    ['/workspace/a\u0000b', false],
    ['/workspace/a\u007fb', false],
  ])('%j is %s', (path, want) => {
    expect(isValidWorkdir(path)).toBe(want)
  })

  // A cwd comes from a host's conversation log and is checked on every board
  // update: a deep path the rule refuses must not backtrack exponentially. A
  // backtracking rule takes about a second on these; each further '/' doubles it.
  it.each([
    '/'.repeat(27) + ';',
    '/workspace/user/' + 'a/'.repeat(22) + 'notes (copy)',
  ])('refuses %j in linear time', (path) => {
    const started = performance.now()
    expect(isValidWorkdir(path)).toBe(false)
    expect(performance.now() - started).toBeLessThan(100)
  }, 1000)
})

describe('recentWorkdirs', () => {
  it('lists only the chosen host’s directories', () => {
    const tasks = [
      task({ id: 'a', host: '', cwd: '/workspace/user/local-only', status_since: '2026-10-09T10:00:00Z' }),
      task({ id: 'b', host: 'build-box', cwd: '/remote/home/demo/api', status_since: '2026-10-09T09:00:00Z' }),
    ]
    expect(recentWorkdirs(tasks, '').map((w) => w.path)).toEqual(['/workspace/user/local-only'])
    expect(recentWorkdirs(tasks, 'build-box').map((w) => w.path)).toEqual(['/remote/home/demo/api'])
    expect(recentWorkdirs(tasks, 'gpu-box')).toEqual([])
  })

  it('puts running tasks first, then stopped ones newest first, one row per directory', () => {
    const tasks = [
      task({ id: 'old', cwd: '/workspace/user/old', status_since: '2026-10-01T00:00:00Z' }),
      task({ id: 'new', cwd: '/workspace/user/new', status_since: '2026-10-08T00:00:00Z', agent: 'codex' }),
      task({ id: 'run', cwd: '/workspace/user/run', state: 'busy', status_since: '2026-09-30T00:00:00Z' }),
      task({ id: 'notime', cwd: '/workspace/user/notime' }),
      task({ id: 'dup-old', cwd: '/workspace/user/new', status_since: '2026-10-02T00:00:00Z' }),
    ]
    const want: RecentWorkdir[] = [
      { path: '/workspace/user/run', agent: 'claude', running: true, lastUsed: '2026-09-30T00:00:00Z' },
      { path: '/workspace/user/new', agent: 'codex', running: false, lastUsed: '2026-10-08T00:00:00Z' },
      { path: '/workspace/user/old', agent: 'claude', running: false, lastUsed: '2026-10-01T00:00:00Z' },
      { path: '/workspace/user/notime', agent: 'claude', running: false, lastUsed: undefined },
    ]
    expect(recentWorkdirs(tasks, '')).toEqual(want)
  })

  it('takes the agent of the running task when one runs in a directory a newer stopped task used', () => {
    const tasks = [
      task({ id: 'stopped', cwd: '/workspace/user/p', status_since: '2026-10-09T00:00:00Z', agent: 'claude' }),
      task({ id: 'running', cwd: '/workspace/user/p', state: 'wait', status_since: '2026-10-01T00:00:00Z', agent: 'codex' }),
    ]
    expect(recentWorkdirs(tasks, '')).toEqual([
      { path: '/workspace/user/p', agent: 'codex', running: true, lastUsed: '2026-10-01T00:00:00Z' },
    ])
  })

  it('orders running tasks by their latest change and ties by path', () => {
    const tasks = [
      task({ id: 'b', cwd: '/workspace/user/b', state: 'idle', status_since: '2026-10-09T00:00:00Z' }),
      task({ id: 'a', cwd: '/workspace/user/a', state: 'idle', status_since: '2026-10-09T00:00:00Z' }),
      task({ id: 'c', cwd: '/workspace/user/c', state: 'unknown', status_since: '2026-10-09T01:00:00Z' }),
    ]
    expect(recentWorkdirs(tasks, '').map((w) => w.path)).toEqual(['/workspace/user/c', '/workspace/user/a', '/workspace/user/b'])
  })

  it('leaves out tasks without a directory and directories the server would refuse', () => {
    const tasks = [
      task({ id: 'none' }),
      task({ id: 'empty', cwd: '' }),
      task({ id: 'rel', cwd: 'workspace/user/p' }),
      task({ id: 'meta', cwd: '/workspace/user/$(id)' }),
      task({ id: 'ctl', cwd: '/workspace/user/a\nb' }),
      task({ id: 'ok', cwd: '/workspace/user/ok' }),
    ]
    expect(recentWorkdirs(tasks, '').map((w) => w.path)).toEqual(['/workspace/user/ok'])
  })
})

describe('filterWorkdirs', () => {
  const list: RecentWorkdir[] = [
    { path: '/workspace/user/panemux', agent: 'claude', running: false },
    { path: '/workspace/user/Panemux-docs', agent: 'codex', running: false },
    { path: '/workspace/user/notes', agent: 'claude', running: false },
  ]

  it('keeps every directory, in order, for blank input', () => {
    expect(filterWorkdirs(list, '')).toEqual(list)
    expect(filterWorkdirs(list, '   ')).toEqual(list)
  })

  it('keeps the directories containing the typed text anywhere, ignoring case and surrounding space', () => {
    expect(filterWorkdirs(list, ' panemux ').map((w) => w.path)).toEqual([
      '/workspace/user/panemux',
      '/workspace/user/Panemux-docs',
    ])
    expect(filterWorkdirs(list, 'NOTES').map((w) => w.path)).toEqual(['/workspace/user/notes'])
    expect(filterWorkdirs(list, 'user/no').map((w) => w.path)).toEqual(['/workspace/user/notes'])
  })

  it('keeps nothing when nothing contains the text', () => {
    expect(filterWorkdirs(list, 'missing')).toEqual([])
  })
})

describe('highlightMatch', () => {
  it.each([
    ['/workspace/user/panemux', '', [{ text: '/workspace/user/panemux', match: false }]],
    ['/workspace/user/panemux', 'zzz', [{ text: '/workspace/user/panemux', match: false }]],
    [
      '/workspace/user/panemux',
      'PANE',
      [
        { text: '/workspace/user/', match: false },
        { text: 'pane', match: true },
        { text: 'mux', match: false },
      ],
    ],
    ['/workspace', ' /workspace ', [{ text: '/workspace', match: true }]],
    [
      '/a/a',
      'a',
      [
        { text: '/', match: false },
        { text: 'a', match: true },
        { text: '/a', match: false },
      ],
    ],
    ['/workspace/İstanbul', 'stan', [{ text: '/workspace/İstanbul', match: false }]],
  ])('splits %j on %j', (path, query, want) => {
    expect(highlightMatch(path, query)).toEqual(want)
  })
})

describe('lastUsedLabel', () => {
  const now = Date.parse('2026-10-09T12:00:00Z')

  it.each([
    [{ running: true, lastUsed: '2026-10-01T00:00:00Z' }, 'now'],
    [{ running: false, lastUsed: '2026-10-09T11:59:30Z' }, 'just now'],
    [{ running: false, lastUsed: '2026-10-09T10:00:00Z' }, '2h ago'],
    [{ running: false, lastUsed: '2026-10-06T12:00:00Z' }, '3d ago'],
    [{ running: false, lastUsed: undefined }, ''],
    [{ running: false, lastUsed: 'not a time' }, ''],
  ])('%j reads %j', (fields, want) => {
    expect(lastUsedLabel({ path: '/p', agent: 'claude', ...fields }, now)).toBe(want)
  })
})
