import type { LayoutChild, PaneConfig, Task, TaskRecord, TaskState, TaskSummary, Workspace } from '../schemas'

// The task dashboard's pure logic: which column a task sits in, how the board
// is filtered and split into lanes, and how a task maps onto a pane. Kept out
// of the components so it can be tested without rendering anything.

export type TaskColumnId = 'wait' | 'busy' | 'idle' | 'other' | 'stop' | 'done'

export interface TaskColumn {
  id: TaskColumnId
  title: string
  /** The states whose tasks sit here. Done holds none: see columnForTask. */
  states: TaskState[]
  hint?: string
}

// Left to right in the order a person should look at them: what needs them
// first, what is stopped last.
export const TASK_COLUMNS: TaskColumn[] = [
  { id: 'wait', title: 'Waiting for input', states: ['wait'], hint: 'Needs you' },
  { id: 'busy', title: 'Working', states: ['busy'] },
  { id: 'idle', title: 'Idle', states: ['idle'], hint: 'Ready for the next instruction' },
  { id: 'other', title: 'Running / unknown', states: ['run', 'unknown'], hint: 'No detailed state' },
  { id: 'stop', title: 'Stopped', states: ['stop'], hint: 'Not running' },
  { id: 'done', title: 'Done', states: [], hint: 'Marked done' },
]

export const TASK_STATE_LABELS: Record<TaskState, string> = {
  wait: 'Waiting for input',
  busy: 'Working',
  idle: 'Idle',
  run: 'Running',
  unknown: 'Unknown',
  stop: 'Stopped',
}

export function columnForState(state: TaskState): TaskColumnId {
  return TASK_COLUMNS.find((column) => column.states.includes(state))?.id ?? 'other'
}

/**
 * The column a task sits in. A task marked done sits in Done only while it is
 * stopped: one that is running again shows the state it is in (issue #256).
 */
export function columnForTask(task: Task): TaskColumnId {
  if (task.done && task.state === 'stop') return 'done'
  return columnForState(task.state)
}

/** The columns on screen. Done is shown only when asked for. */
export function visibleColumns(showDone: boolean): TaskColumn[] {
  return TASK_COLUMNS.filter((column) => showDone || column.id !== 'done')
}

/**
 * Whether a task can be marked done or labeled. Records are keyed by session
 * id; a pid is reused once its process exits, so a task known only by one
 * (codex, an unreadable state file) has nothing stable to carry a record.
 */
export function canRecord(task: Task): boolean {
  return Boolean(task.session_id)
}

// The only session IDs the server resumes: `claude --resume` also accepts a
// session title, so anything else is refused there (issue #257).
const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

/**
 * Whether the dashboard offers Resume: a stopped claude task with a session
 * ID. Done does not matter; a done task can be resumed and stays done.
 */
export function canResume(task: Task): boolean {
  return task.agent === 'claude' && task.state === 'stop' && UUID_PATTERN.test(task.session_id ?? '')
}

/** Whether a task can be summarized: a claude task with a session ID (issue #258). */
export function canSummarize(task: Task): boolean {
  return task.agent === 'claude' && Boolean(task.session_id)
}

/** The work a summary says comes next, and how much remains in all. */
export function summaryNext(summary: TaskSummary | undefined): { next: string; left: number } | null {
  const remaining = summary?.remaining ?? []
  if (remaining.length === 0) return null
  return { next: remaining[0], left: remaining.length }
}

/**
 * Whether selecting a task asks the server to summarize it. Running tasks are
 * summarized by the poll; a stopped one only when asked, which selecting it
 * does while it has no current summary. A failure is retried with the
 * Summarize button, not by selecting the task again.
 */
export function summaryRequestOnSelect(task: Task, summariesEnabled: boolean): boolean {
  if (!summariesEnabled || task.state !== 'stop' || !canSummarize(task)) return false
  return task.summary === undefined || (task.summary.state === 'ready' && task.summary.outdated === true)
}

/** The labels typed into the New task form, comma-separated. */
export function parseLabelInput(text: string): string[] {
  return [...new Set(text.split(',').map((label) => label.trim()).filter((label) => label !== ''))]
}

/** Every label on the given tasks, once each, sorted. */
export function allLabels(tasks: Task[]): string[] {
  return [...new Set(tasks.flatMap((task) => task.labels ?? []))].sort((a, b) => a.localeCompare(b))
}

// Label colors, from the dashboard mock in issue #252.
const LABEL_COLORS = ['#569cd6', '#4ec9b0', '#9cdcfe', '#d7a26b', '#b48ead', '#c678dd', '#8a9199', '#e06c6c']

/** A label's color: always the same for the same label. */
export function labelColor(label: string): string {
  let hash = 0
  for (const char of label) hash = (hash * 31 + (char.codePointAt(0) ?? 0)) >>> 0
  return LABEL_COLORS[hash % LABEL_COLORS.length]
}

/** The tasks with the record a PUT /api/tasks/records answered applied to the task it names. */
export function applyTaskRecord(tasks: Task[], record: TaskRecord): Task[] {
  return tasks.map((task) =>
    task.host === record.host && task.agent === record.agent && task.session_id === record.session_id
      ? { ...task, done: record.done, labels: record.labels }
      : task,
  )
}

/** A task's host as shown to a person: '' is the panemux host itself. */
export function hostLabel(host: string): string {
  return host === '' ? 'Local' : host
}

/**
 * The card title. Until tasks carry a summary, the working directory's last
 * segment is the most recognizable name a task has.
 */
export function taskTitle(task: Task): string {
  if (task.cwd) {
    const segments = task.cwd.split('/').filter(Boolean)
    return segments.length > 0 ? segments[segments.length - 1] : task.cwd
  }
  if (task.session_id) return `Session ${task.session_id.slice(0, 8)}`
  if (task.state === 'unknown') return 'Unreadable session state'
  return `${task.agent} session`
}

export interface TaskFilter {
  query: string
  /** A host name to keep, or null for every host. */
  host: string | null
  /** A label a task must carry, or null for every task. */
  label: string | null
}

export function filterTasks(tasks: Task[], filter: TaskFilter): Task[] {
  const query = filter.query.trim().toLowerCase().replace(/^#/, '')
  return tasks.filter((task) => {
    if (filter.host !== null && task.host !== filter.host) return false
    if (filter.label !== null && !(task.labels ?? []).includes(filter.label)) return false
    if (!query) return true
    const haystack = [
      taskTitle(task),
      task.cwd,
      task.git?.branch,
      task.git?.pr_number !== undefined ? String(task.git.pr_number) : undefined,
      task.session_id,
    ]
    return haystack.some((value) => value?.toLowerCase().includes(query))
  })
}

export type LaneMode = 'none' | 'host' | 'label' | 'repo'

// The keys of the lanes that collect "everything else". A control character
// keeps them apart from any repository or label, which cannot contain one,
// so a label named "No label" is a lane of its own.
const NO_REPO_LANE = '\u0000no-repo'
const NO_LABEL_LANE = '\u0000no-label'

// A Map rather than an object literal: a label or repository may be named
// __proto__, constructor or toString, and an object would answer for those
// from Object.prototype.
const CATCH_ALL_TITLES = new Map<string, string>([
  [NO_REPO_LANE, 'Not in a Git repository'],
  [NO_LABEL_LANE, 'No label'],
])

/** The heading a lane is shown under. */
export function laneTitle(key: string): string {
  return CATCH_ALL_TITLES.get(key) ?? key
}

/**
 * The lanes a task belongs in. It is a list because a task with two labels
 * appears in both label lanes.
 */
export function laneKeys(task: Task, mode: LaneMode): string[] {
  switch (mode) {
    case 'host':
      return [hostLabel(task.host)]
    case 'label':
      return task.labels && task.labels.length > 0 ? [...task.labels] : [NO_LABEL_LANE]
    case 'repo':
      return [task.git?.repo || NO_REPO_LANE]
    default:
      return ['']
  }
}

export interface TaskLane {
  key: string
  title: string
  tasks: Task[]
}

export function groupIntoLanes(tasks: Task[], mode: LaneMode): TaskLane[] {
  const lanes = new Map<string, Task[]>()
  for (const task of tasks) {
    for (const key of laneKeys(task, mode)) {
      const lane = lanes.get(key)
      if (lane) lane.push(task)
      else lanes.set(key, [task])
    }
  }
  return [...lanes.entries()]
    .sort(([a], [b]) => {
      // Lanes that collect "everything else" sort after every named lane.
      const aLast = CATCH_ALL_TITLES.has(a)
      const bLast = CATCH_ALL_TITLES.has(b)
      if (aLast !== bLast) return aLast ? 1 : -1
      return a.localeCompare(b)
    })
    .map(([key, laneTasks]) => ({ key, title: laneTitle(key), tasks: laneTasks }))
}

export interface TaskPaneRef {
  paneId: string
  paneTitle: string
  workspaceId: string
  workspaceTitle: string
}

/**
 * The pane already attached to a task's tmux session: a `tmux` pane for the
 * panemux host, an `ssh_tmux` pane on the same connection for a remote one.
 * Computed here from the workspaces the dashboard already holds, so a pane
 * the dashboard has just created is found before the next collection.
 */
export function findTaskPane(task: Task, workspaces: Workspace[]): TaskPaneRef | null {
  const session = task.location.kind === 'tmux' ? task.location.tmux_session : undefined
  if (!session) return null

  for (const workspace of workspaces) {
    const found = findPane(workspace.layout.children, (pane) => {
      if (pane.tmux_session !== session) return false
      if (task.host === '') return pane.type === 'tmux'
      return pane.type === 'ssh_tmux' && pane.connection === task.host
    })
    if (found) {
      return {
        paneId: found.id,
        paneTitle: found.title || found.id,
        workspaceId: workspace.id,
        workspaceTitle: workspace.title,
      }
    }
  }
  return null
}

function findPane(children: LayoutChild[], match: (pane: PaneConfig) => boolean): PaneConfig | null {
  for (const child of children) {
    if (child.pane && match(child.pane)) return child.pane
    const nested = child.children ? findPane(child.children, match) : null
    if (nested) return nested
  }
  return null
}

export type TaskOpenAction =
  | { kind: 'goto'; pane: TaskPaneRef }
  | { kind: 'open' }
  | { kind: 'unavailable'; reason: string }

/** What selecting "open" on a task does, or why it cannot. */
export function taskOpenAction(task: Task, pane: TaskPaneRef | null): TaskOpenAction {
  if (pane) return { kind: 'goto', pane }
  switch (task.location.kind) {
    case 'tmux':
      return task.location.attachable
        ? { kind: 'open' }
        : { kind: 'unavailable', reason: 'tmux session name cannot be attached from a pane' }
    case 'outside':
      return { kind: 'unavailable', reason: 'running outside tmux' }
    default:
      return { kind: 'unavailable', reason: 'not running' }
  }
}

/** The pane that attaches to a task's tmux session, or null if none can. */
export function paneConfigForTask(task: Task, paneId: string): PaneConfig | null {
  const session = task.location.tmux_session
  if (task.location.kind !== 'tmux' || !task.location.attachable || !session) return null
  if (task.host === '') {
    return { id: paneId, type: 'tmux', tmux_session: session, title: session }
  }
  return { id: paneId, type: 'ssh_tmux', connection: task.host, tmux_session: session, title: session }
}

/** Whether a state comes from a running process. */
export function isLiveState(state: TaskState): boolean {
  return state === 'busy' || state === 'wait' || state === 'idle' || state === 'run'
}

export function runningCount(tasks: Task[], host: string): number {
  return tasks.filter((task) => task.host === host && isLiveState(task.state)).length
}

export function waitingCount(tasks: Task[]): number {
  return tasks.filter((task) => task.state === 'wait').length
}

/** A compact age, such as "3m" or "2h"; "" when there is no time to show. */
export function formatElapsed(since: string | undefined, now: number): string {
  if (!since) return ''
  const at = Date.parse(since)
  if (Number.isNaN(at)) return ''
  const seconds = Math.max(0, Math.floor((now - at) / 1000))
  if (seconds < 60) return 'just now'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`
  return `${Math.floor(hours / 24)}d`
}

// The layer-switching key used until GET /api/display reports the configured
// one. It matches the server's own default for display.task_dashboard_shortcut.
export const DEFAULT_TASK_DASHBOARD_SHORTCUT = 'S'

/** Whether a keydown is Cmd/Ctrl+Shift+<letter>, the form of every global shortcut. */
export function isShortcut(event: KeyboardEvent, letter: string): boolean {
  return (event.metaKey || event.ctrlKey) && event.shiftKey && event.key.toLowerCase() === letter.toLowerCase()
}

/** A shortcut as the platform writes it: ⌘⇧S on macOS, Ctrl+Shift+S elsewhere. */
export function formatShortcut(letter: string, isMac: boolean): string {
  const key = letter.toUpperCase()
  return isMac ? `⌘⇧${key}` : `Ctrl+Shift+${key}`
}
