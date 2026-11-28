import { TaskDashboardPrefsSchema, type TaskDashboardPrefs } from '../schemas'

export const TASK_DASHBOARD_PREFS_KEY = 'panemux:task-dashboard-prefs'
export const DEFAULT_TASK_DASHBOARD_PREFS: TaskDashboardPrefs = {
  version: 1, viewMode: 'cards', groupMode: 'none', sort: { key: 'state', direction: 'asc' },
}

export function loadTaskDashboardPrefs(): TaskDashboardPrefs {
  try {
    const parsed = TaskDashboardPrefsSchema.safeParse(JSON.parse(window.localStorage.getItem(TASK_DASHBOARD_PREFS_KEY) ?? 'null'))
    if (parsed.success) return parsed.data
  } catch {
    // Invalid JSON or denied storage: retain working in-memory settings.
  }
  return TaskDashboardPrefsSchema.parse(DEFAULT_TASK_DASHBOARD_PREFS)
}

export function saveTaskDashboardPrefs(prefs: TaskDashboardPrefs): void {
  try {
    // Parse on write as well, excluding transient state from storage.
    window.localStorage.setItem(TASK_DASHBOARD_PREFS_KEY, JSON.stringify(TaskDashboardPrefsSchema.parse(prefs)))
  } catch {
    // Storage denied or full: settings still work for the lifetime of the page.
  }
}
