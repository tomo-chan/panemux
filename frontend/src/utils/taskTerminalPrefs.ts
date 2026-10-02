// Whether the task dashboard's Type in pane popup (issue #284) is maximized.
// It carries over to the next popup, so answering several tasks in a row
// does not mean widening each one. It is a per-browser display preference,
// kept in localStorage; a browser that refuses storage opens it unmaximized.
const MAXIMIZED_KEY = 'panemux:task-terminal-maximized'

export function loadTaskTerminalMaximized(): boolean {
  try {
    return window.localStorage.getItem(MAXIMIZED_KEY) === 'true'
  } catch {
    return false
  }
}

export function saveTaskTerminalMaximized(maximized: boolean) {
  try {
    window.localStorage.setItem(MAXIMIZED_KEY, String(maximized))
  } catch {
    // Storage is unavailable or full: the preference lasts this popup only.
  }
}
