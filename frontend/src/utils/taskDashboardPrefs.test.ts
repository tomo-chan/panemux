import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_TASK_DASHBOARD_PREFS, loadTaskDashboardPrefs, saveTaskDashboardPrefs, TASK_DASHBOARD_PREFS_KEY } from './taskDashboardPrefs'

beforeEach(() => window.localStorage.clear())
afterEach(() => { vi.restoreAllMocks(); window.localStorage.clear() })

describe('task dashboard preferences', () => {
  it('defaults to Cards, None and ascending State without storage', () => {
    expect(loadTaskDashboardPrefs()).toEqual({ version: 1, viewMode: 'cards', groupMode: 'none', sort: { key: 'state', direction: 'asc' } })
  })

  it('round trips all preferences and strips transient state', () => {
    const prefs = { version: 1 as const, viewMode: 'table' as const, groupMode: 'repo' as const, sort: { key: 'elapsed' as const, direction: 'desc' as const } }
    saveTaskDashboardPrefs({ ...prefs, query: 'private' } as typeof prefs)
    expect(loadTaskDashboardPrefs()).toEqual(prefs)
    expect(JSON.parse(window.localStorage.getItem(TASK_DASHBOARD_PREFS_KEY)!)).toEqual(prefs)
  })

  it.each([
    'broken', 'null', '[]', '{}',
    JSON.stringify({ ...DEFAULT_TASK_DASHBOARD_PREFS, version: 2 }),
    JSON.stringify({ ...DEFAULT_TASK_DASHBOARD_PREFS, viewMode: 'grid' }),
    JSON.stringify({ ...DEFAULT_TASK_DASHBOARD_PREFS, groupMode: 'session-group' }),
    JSON.stringify({ ...DEFAULT_TASK_DASHBOARD_PREFS, sort: { key: 'labels', direction: 'asc' } }),
    JSON.stringify({ ...DEFAULT_TASK_DASHBOARD_PREFS, sort: { key: 'state', direction: 'up' } }),
  ])('falls back on invalid stored value %s', (raw) => {
    window.localStorage.setItem(TASK_DASHBOARD_PREFS_KEY, raw)
    expect(loadTaskDashboardPrefs()).toEqual(DEFAULT_TASK_DASHBOARD_PREFS)
  })

  it('survives denied access, read errors and quota exhaustion', () => {
    vi.spyOn(window, 'localStorage', 'get').mockImplementation(() => { throw new Error('denied') })
    expect(loadTaskDashboardPrefs()).toEqual(DEFAULT_TASK_DASHBOARD_PREFS)
    expect(() => saveTaskDashboardPrefs(DEFAULT_TASK_DASHBOARD_PREFS)).not.toThrow()
    vi.restoreAllMocks()
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('read denied') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('quota exceeded') })
    expect(loadTaskDashboardPrefs()).toEqual(DEFAULT_TASK_DASHBOARD_PREFS)
    expect(() => saveTaskDashboardPrefs(DEFAULT_TASK_DASHBOARD_PREFS)).not.toThrow()
  })
})
