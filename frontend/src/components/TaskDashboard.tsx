import React, { useCallback, useId, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { Task, TaskDashboardPrefs, TaskSort, TaskHost, TaskIssueLink, TaskLaunchResponse, Workspace } from '../schemas'
import type { HostTerminalType, TasksState } from '../hooks/useTasks'
import { TASKS_POLL_INTERVAL_MS } from '../hooks/useTasks'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'
import { NewTaskDialog } from './NewTaskDialog'
import { LabelSuggestions } from './LabelSuggestions'
import { DashboardHostsDialog } from './DashboardHostsDialog'
import { useSSHConnections } from '../hooks/useSSHConnections'
import type { SSHConnectionsState } from '../hooks/useSSHConnections'
import { useTaskInput } from '../hooks/useTaskInput'
import type { TaskInputOrigin } from '../hooks/useTaskInput'
import { TaskInputPopup } from './TaskInputPopup'
import { HostConnectDialog } from './HostConnectDialog'
import { codexHookTool } from './codexHookTool'
import { HostTools } from './HostTools'
import { agmsgTool } from './agmsgTool'
import { HostTerminalPopup } from './HostTerminalPopup'
import { useHostTerminal } from '../hooks/useHostTerminal'
import { loadTaskTerminalMaximized, saveTaskTerminalMaximized } from '../utils/taskTerminalPrefs'
import {
  TASK_COLUMNS,
  TASK_STATE_LABELS,
  allLabels,
  canRecord,
  actionUnavailableReason,
  canResume,
  isKnownAgent,
  taskAction,
  canSummarize,
  columnForTask,
  filterTasks,
  listedTasks,
  findLaunchedTask,
  findTaskPane,
  isPermissionWait,
  formatElapsed,
  groupIntoLanes,
  hostLabel,
  labelColor,
  runningCount,
  summaryNext,
  summaryRequestOnSelect,
  taskInputAction,
  taskOpenAction,
  taskTitle,
  boardGroups,
} from '../utils/taskBoard'
import type { TaskBoardGroup, LaneMode, LaunchedTaskRef, TaskInputAction, TaskOpenAction, TaskPaneRef } from '../utils/taskBoard'
import { nextTaskSort, sortTaskGroups } from '../utils/taskTable'
import { matchLabelSuggestions } from '../utils/labelSuggestions'
import { unreadableRows } from '../utils/unreadableState'
import UnreadableStateDialog from './UnreadableStateDialog'

// Layer 1 of issue #252: every agent session on every host, as a kanban by
// state, with a detail panel on the right. Besides reading, it records what a
// person says about a task — done, and its labels (issue #256) — through
// tasksState.saveRecord, and starts new tasks and resumes stopped ones
// (issue #257) through tasksState.launch and tasksState.resume, and shows
// what `claude -p` made of each task's conversation (issue #258), asking for
// a stopped task's summary through tasksState.requestSummary. Opening a
// task is App's job (onOpenTask), because that means creating or focusing a
// pane. Typing into a task without leaving the board (issue #284) is the
// dashboard's own: TaskInputPopup over tasksState.attach.

const STATE_COLORS: Record<Task['state'], string> = {
  wait: '#e2b86b',
  busy: '#4ec9b0',
  idle: '#8fa6c4',
  run: '#8fa6c4',
  unknown: '#c586c0',
  stop: '#80858d',
}

const COLUMN_COLORS: Record<string, string> = {
  wait: STATE_COLORS.wait,
  busy: STATE_COLORS.busy,
  idle: STATE_COLORS.idle,
  other: STATE_COLORS.unknown,
  stop: STATE_COLORS.stop,
  done: '#7fae6a',
}

// A host select value no ssh_connections key can collide with; '' is the
// panemux host, so it cannot stand for "every host".
const ALL_HOSTS = '\u0000all'
// Likewise for the label select; a label cannot contain a control character.
const ALL_LABELS = '\u0000all'

// Below this width the Type in pane popup is a full sheet from the start.
const NARROW_QUERY = '(max-width: 720px)'

/** Board settings held by App for the lifetime of this page. */
export interface TaskDashboardViewState extends Pick<TaskDashboardPrefs, 'viewMode' | 'sort'> {
  query: string
  laneMode: LaneMode
  hostFilter: string
  labelFilter: string
  showDone: boolean
}

export const INITIAL_TASK_DASHBOARD_VIEW_STATE: TaskDashboardViewState = {
  query: '',
  viewMode: 'cards',
  sort: { key: 'state', direction: 'asc' },
  laneMode: 'none',
  hostFilter: ALL_HOSTS,
  labelFilter: ALL_LABELS,
  showDone: false,
}

export interface TaskDashboardProps {
  viewState: TaskDashboardViewState
  onViewStateChange: React.Dispatch<React.SetStateAction<TaskDashboardViewState>>
  agentBoardEnabled?: boolean
  tasksState: TasksState
  /**
   * The hosts the dashboard collects from (ssh_connections), for the Hosts…
   * dialog. Injectable for tests; the dashboard manages them itself otherwise.
   */
  hostsState?: SSHConnectionsState
  workspaces: Workspace[]
  onOpenTask: (task: Task, action: TaskOpenAction) => void
  /**
   * Adds a new pane on a host (issue #314): App's job, like onOpenTask. Every
   * call is a new pane, whatever panes the host already has.
   */
  onOpenHost?: (host: string, type: HostTerminalType, tmuxSession: string | undefined) => void
  onShowWorkspaces: () => void
  /** The layer-switching shortcut, as shown and as aria-keyshortcuts spells it. */
  shortcut?: { label: string; aria: string }
  /** Clock for elapsed times; injectable for tests. */
  now?: () => number
  /**
   * A task to select and highlight, from a browser notification of a task
   * no pane shows (issue #279). seq tells one request from the next.
   */
  focusRequest?: { taskId: string; seq: number } | null
  /** Called once the requested task is selected, so the request is not repeated. */
  onFocusRequestHandled?: () => void
  /**
   * The tasks the dashboard lists, after its filters: a wait the dashboard
   * lists is one the person can see, and is not notified.
   */
  onListedTasksChange?: (taskIds: ReadonlySet<string>) => void
}

// How long a task reached from a notification stays highlighted.
const TASK_CARD_FLASH_MS = 1800

type StateVars = React.CSSProperties & Record<'--td-sc', string>

function stateStyle(state: Task['state']): StateVars {
  return { '--td-sc': STATE_COLORS[state] }
}

export const TaskDashboard: React.FC<TaskDashboardProps> = ({
  tasksState,
  viewState,
  onViewStateChange,
  agentBoardEnabled = false,
  hostsState,
  workspaces,
  onOpenTask,
  onOpenHost,
  onShowWorkspaces,
  shortcut,
  now = Date.now,
  focusRequest = null,
  onFocusRequestHandled,
  onListedTasksChange,
}) => {
  const { data, error, loading, updatedAt, refresh, reconnect, saveRecord, launch, resume, requestSummary, inputGiven } =
    tasksState
  const { query, laneMode, hostFilter, labelFilter, showDone, viewMode, sort } = viewState
  const updateViewState = <K extends keyof TaskDashboardViewState>(key: K, value: TaskDashboardViewState[K]) => {
    onViewStateChange((previous) => ({ ...previous, [key]: value }))
  }
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [newTaskOpen, setNewTaskOpen] = useState(false)
  const [hostsOpen, setHostsOpen] = useState(false)
  // The host whose connection menu is open (issue #314), one at a time.
  const [hostMenu, setHostMenu] = useState<string | null>(null)
  const [unreadableOpen, setUnreadableOpen] = useState(false)
  const [flashRequest, setFlashRequest] = useState<TaskDashboardProps['focusRequest']>(null)
  const flashTaskId = flashRequest?.taskId ?? null
  const ownHostsState = useSSHConnections()
  const hostsDialogState = hostsState ?? ownHostsState
  // A task that was started but is not listed yet: claude writes the state
  // the collection reads only once it is running, and codex has no session
  // until it has its first instruction. It is selected when it appears,
  // unless another task was selected meanwhile; a codex task is selected as
  // its process first and again once its session is listed.
  const [pendingLaunch, setPendingLaunch] = useState<{ ref: LaunchedTaskRef; message: string } | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  // Every task whose resume is in flight. Each is tracked on its own, so
  // resuming one task never re-enables another's Resume mid-request.
  const [resumingIds, setResumingIds] = useState<ReadonlySet<string>>(() => new Set())
  const nowMs = useTicker(now)
  const rootRef = useRef<HTMLElement>(null)
  const topRef = useRef<HTMLElement>(null)
  const popupRef = useRef<HTMLDivElement>(null)
  const input = useTaskInput(tasksState.attach, tasksState.detach)
  const hostInput = useHostTerminal(tasksState.hostTerminal, tasksState.closeHostTerminal)
  // Focus goes back to the chip the popup was opened from — or, if a poll
  // replaced it, to that host's chip as it is now, and failing that to the
  // dashboard.
  const closeHostInput = () => {
    const closed = hostInput.close()
    if (!closed) return
    const element = closed.originElement?.isConnected
      ? closed.originElement
      : rootRef.current?.querySelector<HTMLElement>(`[data-host-chip="${CSS.escape(closed.host)}"]`)
    ;(element ?? rootRef.current)?.focus({ preventScroll: true })
  }
  const [maximized, setMaximized] = useState(loadTaskTerminalMaximized)
  const narrow = useMediaQuery(NARROW_QUERY)
  const [topOffset, setTopOffset] = useState(0)
  const inputOpen = input.session !== null

  // Focus moves into the dashboard when it appears, so typing does not keep
  // going to the terminal underneath it.
  useEffect(() => {
    rootRef.current?.focus({ preventScroll: true })
  }, [])

  const tasks = useMemo(() => data?.tasks ?? [], [data])
  const summariesEnabled = data?.summaries_enabled ?? false
  const hosts = data?.hosts ?? []
  const agents = data?.agents
  const unreadableCount = unreadableRows(hosts).length

  // The connection menu goes with its host when a poll finds the host gone
  // or no longer reachable, so it never reopens by itself when the host
  // comes back. Focus that was in the menu moves to the host's Reconnect
  // button, or to the dashboard while the host is connecting.
  const menuHost = hostMenu === null ? undefined : hosts.find((host) => host.name === hostMenu)
  const menuGone = hostMenu !== null && !(menuHost && hostHasMenu(menuHost))
  useEffect(() => {
    if (!menuGone || hostMenu === null) return
    setHostMenu(null)
    if (document.activeElement && document.activeElement !== document.body) return
    const reconnect = rootRef.current?.querySelector<HTMLElement>(`[data-reconnect="${CSS.escape(hostMenu)}"]`)
    ;(reconnect ?? rootRef.current)?.focus({ preventScroll: true })
  }, [menuGone, hostMenu])
  // Once the files are gone the details are closed, so they do not open by
  // themselves when a file becomes unreadable again.
  if (unreadableOpen && unreadableCount === 0) setUnreadableOpen(false)
  const labels = useMemo(() => allLabels(tasks), [tasks])
  // The labels used before, for the label inputs only: the label filter
  // offers the labels on the board. None are offered while the record file
  // cannot be read; a label can still be typed.
  const knownLabels = useMemo(() => (data?.records_error ? [] : data?.known_labels ?? []), [data])
  // Keep unavailable choices so they return after a temporary loading/error
  // state or when a host/label reappears; display and filter as All meanwhile.
  const activeHost = hosts.some((host) => host.name === hostFilter) ? hostFilter : null
  const activeLabel = labelFilter !== ALL_LABELS && labels.includes(labelFilter) ? labelFilter : null
  const visible = useMemo(
    () => listedTasks(tasks, { query, host: activeHost, label: activeLabel }, showDone),
    [activeHost, activeLabel, query, tasks, showDone],
  )
  const groups = useMemo(() => groupIntoLanes(visible, laneMode), [visible, laneMode])
  const cardGroups = useMemo(() => boardGroups(groups, showDone, laneMode), [groups, showDone, laneMode])
  const tableGroups = useMemo(
    () => sortTaskGroups(groups, sort, now(), agents),
    [groups, sort, agents, now],
  )
  useEffect(() => {
    if (!onListedTasksChange) return
    onListedTasksChange(new Set(visible.map((task) => task.id)))
  }, [onListedTasksChange, visible])
  useEffect(() => () => onListedTasksChange?.(new Set()), [onListedTasksChange])
  const panes = useMemo(() => {
    const byTask = new Map<string, TaskPaneRef | null>()
    for (const task of tasks) byTask.set(task.id, findTaskPane(task, workspaces))
    return byTask
  }, [tasks, workspaces])
  const selected = tasks.find((task) => task.id === selectedId) ?? null

  const launchedTask = pendingLaunch ? findLaunchedTask(tasks, pendingLaunch.ref) : null
  if (launchedTask) {
    if (launchedTask.settled) setPendingLaunch(null)
    if (selectedId !== launchedTask.task.id) {
      setSelectedId(launchedTask.task.id)
      setDetailOpen(true)
    }
  }

  const select = (task: Task) => {
    setPendingLaunch(null)
    setSelectedId(task.id)
    setDetailOpen(true)
  }
  // A notification's task is selected once it is collected, with every
  // filter that would hide it cleared: reaching it is the point.
  const handledFocusSeq = useRef<number | null>(null)
  useEffect(() => {
    if (!focusRequest || handledFocusSeq.current === focusRequest.seq) return
    const target = tasks.find((task) => task.id === focusRequest.taskId)
    if (!target) return
    handledFocusSeq.current = focusRequest.seq
    const filter = { query, host: activeHost, label: activeLabel }
    if (filterTasks([target], filter).length === 0) {
      onViewStateChange((previous) => ({ ...previous, query: '', hostFilter: ALL_HOSTS, labelFilter: ALL_LABELS }))
    }
    setPendingLaunch(null)
    setSelectedId(target.id)
    setDetailOpen(true)
    setFlashRequest({ taskId: target.id, seq: focusRequest.seq })
    onFocusRequestHandled?.()
  }, [activeHost, activeLabel, focusRequest, onFocusRequestHandled, onViewStateChange, query, tasks])

  const focusedRequestSeq = useRef<number | null>(null)
  useEffect(() => {
    if (!flashRequest || focusedRequestSeq.current === flashRequest.seq) return
    focusedRequestSeq.current = flashRequest.seq
    const target = rootRef.current
      ?.querySelector<HTMLElement>(`[data-task-view="${viewMode}"] [data-task-id="${CSS.escape(flashRequest.taskId)}"]`)
    target?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
    // Only a notification jump measures the variable header band; row heights
    // remain entirely CSS-driven, including after card content changes.
    const band = target?.closest('.td-group')?.querySelector('.td-group-band')
    const board = target?.closest<HTMLElement>('.td-board')
    if (target && band && board) {
      const overlap = band.getBoundingClientRect().bottom - target.getBoundingClientRect().top
      if (overlap > 0) board.scrollTop -= overlap
    }
    target?.querySelector<HTMLElement>('[data-task-select]')?.focus({ preventScroll: true })
  }, [flashRequest, viewMode])
  useEffect(() => {
    if (!flashRequest) return
    const timeoutId = window.setTimeout(() => setFlashRequest(null), TASK_CARD_FLASH_MS)
    return () => window.clearTimeout(timeoutId)
  }, [flashRequest])

  // A person selecting a task: a stopped one is summarized only when asked.
  const pick = (task: Task) => {
    select(task)
    if (summaryRequestOnSelect(task, summariesEnabled)) void requestSummary(task)
  }
  const launched = (result: TaskLaunchResponse, host: string, agent: string) => {
    setNewTaskOpen(false)
    setActionError(
      result.records_error ? `The task started, but its labels could not be saved: ${result.records_error}` : null,
    )
    const labelsNote = result.pending_labels?.length ? `; its labels are recorded once ${agent} has started its session` : ''
    setPendingLaunch({
      ref: { host, agent, id: result.id, tmux_session: result.tmux_session },
      message: `Started tmux ${result.tmux_session} on ${hostLabel(host)}. It is selected here once ${agent} has started${labelsNote}.`,
    })
  }
  const resumeTask = async (task: Task) => {
    setResumingIds((current) => new Set(current).add(task.id))
    setActionError(null)
    const result = await resume(task)
    setResumingIds((current) => {
      const next = new Set(current)
      next.delete(task.id)
      return next
    })
    if (!result.ok) {
      setActionError(`Could not resume ${taskTitle(task)}: ${result.error}`)
      return
    }
    select(task)
  }
  const open = (task: Task) => onOpenTask(task, taskOpenAction(task, panes.get(task.id) ?? null))

  const inputTaskId = input.session?.task.id ?? null
  const inputPhase = input.session?.attach.phase ?? null
  const typeIn = (task: Task, origin: TaskInputOrigin, element: HTMLElement) => input.open(task, origin, element)
  const inputMark = (task: Task): TaskInputMark =>
    task.id !== inputTaskId ? null : inputPhase === 'connecting' ? 'connecting' : 'typing'

  // Focus goes back to the button the popup was opened from — or, if a poll
  // moved its card and the button went with it, to the same task's button
  // wherever it is now, and failing that to the dashboard.
  const focusAfterClose = useRef<{ taskId: string; origin: TaskInputOrigin; element: HTMLElement | null } | null>(null)
  const closeInput = useCallback(() => {
    const closed = input.close()
    if (closed) focusAfterClose.current = { taskId: closed.task.id, origin: closed.origin, element: closed.originElement }
  }, [input])
  useEffect(() => {
    const target = focusAfterClose.current
    if (inputOpen || !target) return
    focusAfterClose.current = null
    const fallback = rootRef.current?.querySelector<HTMLElement>(
      `${target.origin === 'card' ? `[data-task-view="${viewMode}"] ` : ''}[data-type-in="${CSS.escape(target.taskId)}"][data-origin="${target.origin}"]`,
    )
    const selection = rootRef.current?.querySelector<HTMLElement>(
      `[data-task-view="${viewMode}"] [data-task-id="${CSS.escape(target.taskId)}"] [data-task-select]`,
    )
    const element = target.element?.isConnected && !target.element.closest('[hidden]') ? target.element : fallback ?? selection
    ;(element ?? rootRef.current?.querySelector<HTMLElement>(`[data-task-view="${viewMode}"]`) ?? rootRef.current)?.focus({ preventScroll: true })
  }, [inputOpen, viewMode])

  // Everything but the popup is inert while it is open — the top bar, the
  // filters, the board and the detail panel — so no key and no click reaches
  // them. Their selection, filters and scroll position are left as they were.
  useEffect(() => {
    const root = rootRef.current
    if (!inputOpen || !root) return
    const made: Element[] = []
    for (const child of Array.from(root.children)) {
      if (child === popupRef.current || child.hasAttribute('inert')) continue
      child.setAttribute('inert', '')
      made.push(child)
    }
    return () => made.forEach((child) => child.removeAttribute('inert'))
  }, [inputOpen])

  // A maximized popup covers everything below the top bar and leaves the bar
  // in view, so it needs to know where the bar ends.
  useLayoutEffect(() => {
    if (!inputOpen) return
    const measure = () => {
      const root = rootRef.current
      const top = topRef.current
      if (root && top) setTopOffset(top.getBoundingClientRect().bottom - root.getBoundingClientRect().top)
    }
    measure()
    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [inputOpen])

  const toggleMaximized = useCallback(() => {
    setMaximized((current) => {
      saveTaskTerminalMaximized(!current)
      return !current
    })
  }, [])

  const inputTask = input.session ? tasks.find((task) => task.id === input.session!.task.id) ?? null : null
  const shownInputTask = inputTask ?? input.session?.task ?? null
  const inputColumn = inputTask ? columnForTask(inputTask) : null
  const movedTo =
    inputColumn && input.session && inputColumn !== input.session.column
      ? TASK_COLUMNS.find((column) => column.id === inputColumn)?.title ?? null
      : null

  return (
    <section
      ref={rootRef}
      tabIndex={-1}
      className="td-root"
      aria-label="Task dashboard"
      style={{ '--td-font': TERMINAL_FONT_FAMILY, '--td-mono': TERMINAL_FONT_FAMILY } as React.CSSProperties}
    >
      <header ref={topRef} className="td-top">
        <h1>Tasks</h1>
        <ul className="td-hosts" aria-label="Hosts">
          {hosts.map((host) => (
            <HostChip
              key={host.name}
              host={host}
              running={runningCount(tasks, host.name)}
              onReconnect={reconnect}
              menu={{
                open: hostMenu === host.name,
                onToggle: () => setHostMenu((current) => (current === host.name ? null : host.name)),
                onCancel: () => setHostMenu(null),
                onOpen: (type, tmuxSession) => {
                  setHostMenu(null)
                  onOpenHost?.(host.name, type, tmuxSession)
                },
                onTypeIn: (type, tmuxSession, chip) => {
                  setHostMenu(null)
                  hostInput.open(host.name, type, tmuxSession, chip)
                },
                sessionName: tasksState.hostSessionName,
                agmsgEnabled: agentBoardEnabled,
                codexHook: tasksState.codexHook,
                installCodexHook: tasksState.installCodexHook,
                setCodexHookAuto: tasksState.setCodexHookAuto,
              }}
            />
          ))}
        </ul>
        <span className="td-spacer" />
        <div className="td-top-actions">
          {unreadableCount > 0 && (
            <button
              type="button"
              className="td-btn td-btn-warn"
              aria-expanded={unreadableOpen}
              aria-controls="td-unreadable-dialog"
              aria-label={`${unreadableCount} unreadable session state file${unreadableCount === 1 ? '' : 's'} — ${unreadableOpen ? 'hide' : 'show'} details`}
              onClick={() => setUnreadableOpen((open) => !open)}
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M12 3 2 21h20L12 3z" />
                <path d="M12 10v5" />
                <path d="M12 18h.01" />
              </svg>
              {unreadableCount} unreadable
            </button>
          )}
          <span className="td-updated" aria-live="polite">
            {updatedLabel(loading, updatedAt, nowMs)}
          </span>
          <button type="button" className="td-btn" onClick={() => void refresh()} disabled={loading}>
            Refresh
          </button>
          <button type="button" className="td-btn" onClick={() => setHostsOpen(true)}>
            Hosts…
          </button>
          <button type="button" className="td-btn td-btn-primary" onClick={() => setNewTaskOpen(true)}>
            New task
          </button>
          <button type="button" className="td-btn" onClick={onShowWorkspaces} aria-keyshortcuts={shortcut?.aria}>
            Workspaces
            {shortcut && (
              <span className="td-kbd" aria-hidden="true">
                {shortcut.label}
              </span>
            )}
          </button>
        </div>
      </header>

      {error && (
        <div role="alert" className="td-alert">
          Failed to update tasks: {error}
        </div>
      )}
      {actionError && (
        <div role="alert" className="td-alert">
          {actionError}
        </div>
      )}
      {pendingLaunch && (
        <div role="status" className="td-notice">
          {pendingLaunch.message}
        </div>
      )}
      {data?.records_error && (
        <div role="alert" className="td-alert">
          Done and labels could not be loaded: {data.records_error}
        </div>
      )}
      {data?.summaries_error && (
        <div role="alert" className="td-alert">
          Summaries will not be kept after panemux restarts: {data.summaries_error}
        </div>
      )}

      <div className="td-tools">
        <input
          type="search"
          placeholder="Filter by directory, branch, PR or session"
          aria-label="Filter tasks"
          value={query}
          onChange={(event) => updateViewState('query', event.target.value)}
        />
        <label>
          Group
          <select aria-label="Group tasks by" value={laneMode} onChange={(event) => updateViewState('laneMode', event.target.value as LaneMode)}>
            <option value="none">None</option>
            <option value="host">Host</option>
            <option value="label">Label</option>
            <option value="repo">Repository</option>
          </select>
        </label>
        <label>
          Host
          <select aria-label="Show host" value={activeHost ?? ALL_HOSTS} onChange={(event) => updateViewState('hostFilter', event.target.value)}>
            <option value={ALL_HOSTS}>All</option>
            {hosts.map((host) => (
              <option key={host.name} value={host.name}>
                {hostLabel(host.name)}
              </option>
            ))}
          </select>
        </label>
        <label>
          Label
          <select aria-label="Show label" value={activeLabel ?? ALL_LABELS} onChange={(event) => updateViewState('labelFilter', event.target.value)}>
            <option value={ALL_LABELS}>All</option>
            {labels.map((label) => (
              <option key={label} value={label}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label>
          <input
            type="checkbox"
            aria-label="Show done"
            checked={showDone}
            onChange={(event) => updateViewState('showDone', event.target.checked)}
          />
          Show done
        </label>
        <div className="td-view-switch" role="group" aria-label="Task view">
          {(['cards', 'table'] as const).map((mode) => (
            <button type="button" className="td-btn" key={mode} aria-pressed={viewMode === mode}
              onClick={() => updateViewState('viewMode', mode)}>
              {mode === 'cards' ? 'Cards' : 'Table'}
            </button>
          ))}
        </div>
      </div>

      <div className="td-main">
        {/* The board scrolls sideways when its columns are wider than the
            space left, and with no task card nothing in it takes focus, so the
            board is a tab stop of its own for scrolling it from the keyboard. */}
        <div className="td-board" role="region" aria-label="Task board" tabIndex={0} data-task-view="cards" hidden={viewMode !== 'cards'}>
          <TaskBoard groups={cardGroups} grouped={laneMode !== 'none'} renderCard={(task) => (
            <TaskCard key={task.id} task={task} pane={panes.get(task.id) ?? null}
              selected={task.id === selectedId} flashing={task.id === flashTaskId}
              nowMs={nowMs} onSelect={pick} onOpen={open} onResume={(t) => void resumeTask(t)}
              resuming={resumingIds.has(task.id)} onTypeIn={typeIn} inputMark={inputMark(task)}
              knownAgent={isKnownAgent(agents, task.agent)} />
          )} />
        </div>
        <div className="td-board td-table-scroll" role="region" aria-label="Task table" tabIndex={0} data-task-view="table" hidden={viewMode !== 'table'}>
          <table className="td-table" aria-label="Tasks">
            <thead><tr>
              {TABLE_COLUMNS.map(({ key, title }) => (
                <th key={key} scope="col" aria-sort={sort.key === key ? (sort.direction === 'asc' ? 'ascending' : 'descending') : undefined}>
                  <button type="button" onClick={() => updateViewState('sort', nextTaskSort(sort, key))}
                    aria-label={`Sort by ${title}${sort.key === key ? `, ${sort.direction === 'asc' ? 'ascending' : 'descending'}` : ''}`}>
                    {title}{sort.key === key && <span aria-hidden="true"> {sort.direction === 'asc' ? '↑' : '↓'}</span>}
                  </button>
                </th>
              ))}
              <th scope="col">Labels</th><th scope="col">Actions</th>
            </tr></thead>
            {tableGroups.length === 0 && <tbody><tr><td colSpan={9} className="td-empty">No matching sessions</td></tr></tbody>}
            {tableGroups.map((group) => (
              <tbody key={group.key}>
                {laneMode !== 'none' && <tr className="td-table-group"><th scope="rowgroup" colSpan={9}>{group.title} · {group.tasks.length} {group.tasks.length === 1 ? 'session' : 'sessions'}</th></tr>}
                {group.tasks.map((task) => (
                  <TaskTableRow key={task.id} task={task} pane={panes.get(task.id) ?? null}
                    selected={task.id === selectedId} flashing={task.id === flashTaskId} nowMs={nowMs}
                    onSelect={pick} onOpen={open} onResume={(t) => void resumeTask(t)} resuming={resumingIds.has(task.id)}
                    onTypeIn={typeIn} inputMark={inputMark(task)} knownAgent={isKnownAgent(agents, task.agent)}
                    agentName={agents?.find((agent) => agent.id === task.agent)?.display_name} />
                ))}
              </tbody>
            ))}
          </table>
        </div>
        <TaskDetail
          task={selected}
          knownAgent={selected ? isKnownAgent(agents, selected.agent) : true}
          pane={selected ? panes.get(selected.id) ?? null : null}
          open={detailOpen}
          nowMs={nowMs}
          onOpen={open}
          onResume={(t) => void resumeTask(t)}
          resuming={selected !== null && resumingIds.has(selected.id)}
          onTypeIn={typeIn}
          inputMark={selected ? inputMark(selected) : null}
          onSaveRecord={saveRecord}
          knownLabels={knownLabels}
          onRequestSummary={requestSummary}
          onInputGiven={inputGiven}
          summariesEnabled={summariesEnabled}
          showDone={showDone}
          onClose={() => setDetailOpen(false)}
        />
      </div>
      <NewTaskDialog
        isOpen={newTaskOpen}
        hosts={hosts}
        agents={agents}
        knownLabels={knownLabels}
        tasks={tasks}
        onLaunch={launch}
        onLaunched={launched}
        onClose={() => setNewTaskOpen(false)}
      />
      <UnreadableStateDialog
        isOpen={unreadableOpen}
        hosts={hosts}
        onClose={() => setUnreadableOpen(false)}
      />
      <DashboardHostsDialog
        isOpen={hostsOpen}
        state={hostsDialogState}
        onChanged={() => void refresh()}
        onClose={() => setHostsOpen(false)}
      />
      {input.session && shownInputTask && (
        <div ref={popupRef} className="td-input-host">
          <TaskInputPopup
            task={shownInputTask}
            listed={inputTask !== null}
            movedTo={movedTo}
            pane={panes.get(shownInputTask.id) ?? null}
            attach={input.session.attach}
            attempt={input.session.attempt}
            terminal={input.session.terminal}
            onTerminalStatus={input.setTerminal}
            maximized={maximized}
            sheet={narrow}
            topOffset={topOffset}
            onToggleMaximize={toggleMaximized}
            onClose={closeInput}
            onRetry={input.retry}
            onOpenTask={(task) => {
              focusAfterClose.current = null
              input.close()
              open(task)
            }}
            onInputGiven={inputGiven}
            stateStyle={stateStyle(shownInputTask.state)}
          />
        </div>
      )}
      {hostInput.session && (
        <div className="td-input-host">
          <HostTerminalPopup
            session={hostInput.session}
            onTerminalStatus={hostInput.setTerminal}
            maximized={maximized}
            sheet={narrow}
            topOffset={topOffset}
            onToggleMaximize={toggleMaximized}
            onClose={closeHostInput}
            onRetry={() => void hostInput.retry()}
          />
        </div>
      )}
    </section>
  )
}

// Re-renders every few seconds so elapsed times and "updated … ago" advance
// between collections.
function useTicker(now: () => number): number {
  const [nowMs, setNowMs] = useState(now)
  useEffect(() => {
    const interval = setInterval(() => setNowMs(now()), 5000)
    return () => clearInterval(interval)
  }, [now])
  return nowMs
}

// Whether a media query matches, following changes. A browser (or jsdom)
// without matchMedia counts as not matching.
function useMediaQuery(query: string): boolean {
  const read = () => (typeof window.matchMedia === 'function' ? window.matchMedia(query).matches : false)
  const [matches, setMatches] = useState(read)
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const list = window.matchMedia(query)
    const update = () => setMatches(list.matches)
    update()
    list.addEventListener?.('change', update)
    return () => list.removeEventListener?.('change', update)
  }, [query])
  return matches
}

function updatedLabel(loading: boolean, updatedAt: number | null, nowMs: number): string {
  if (loading && updatedAt === null) return 'Loading…'
  if (updatedAt === null) return 'Not loaded yet'
  const age = formatElapsed(new Date(updatedAt).toISOString(), nowMs)
  const interval = `every ${TASKS_POLL_INTERVAL_MS / 1000}s while shown`
  return `${loading ? 'Updating… · ' : ''}Updated ${age === 'just now' ? 'just now' : `${age} ago`} · ${interval}`
}

interface HostChipProps {
  host: TaskHost
  running: number
  onReconnect: (host: string) => Promise<void>
  /** Opens the connection menu (issue #314); absent, the chip opens nothing. */
  menu?: HostChipMenu
}

interface HostChipMenu {
  open: boolean
  onToggle: () => void
  /** Closed without opening anything: focus goes back to the chip. */
  onCancel: () => void
  onOpen: (type: HostTerminalType, tmuxSession: string | undefined) => void
  onTypeIn: (type: HostTerminalType, tmuxSession: string | undefined, chip: HTMLElement | null) => void
  sessionName: TasksState['hostSessionName']
  agmsgEnabled?: boolean
  codexHook: TasksState['codexHook']
  installCodexHook: TasksState['installCodexHook']
  setCodexHookAuto: TasksState['setCodexHookAuto']
}

// A reachable host has a menu; one that is connecting or failing has
// nothing to open or check yet. Only a remote one opens a terminal: the
// panemux host is not a connection, and its menu has the Codex input
// notifications item alone (issue #396).
function hostHasMenu(host: TaskHost): boolean {
  return host.status === 'ok'
}

const HostChip: React.FC<HostChipProps> = ({ host, running, onReconnect, menu }) => {
  const chipRef = useRef<HTMLButtonElement>(null)
  const label = hostLabel(host.name)
  let detail: string
  switch (host.status) {
    case 'ok':
      detail = `${running} running`
      break
    case 'connecting':
      detail = 'connecting…'
      break
    default:
      detail = host.error ?? 'unreachable'
  }
  const content = (
    <>
      <span className="td-host-dot" aria-hidden="true" />
      <b>{label}</b>{' '}
      <span className="td-host-detail">{detail}</span>
    </>
  )
  if (menu && hostHasMenu(host)) {
    const terminal = host.name !== ''
    const cancel = () => {
      menu.onCancel()
      chipRef.current?.focus()
    }
    return (
      <li className="td-host-item">
        <button
          ref={chipRef}
          type="button"
          className="td-host td-host-button"
          data-status={host.status}
          data-host-chip={host.name}
          aria-haspopup="dialog"
          aria-expanded={menu.open}
          aria-label={terminal ? `Open a terminal on ${host.name}` : `Codex input notifications on ${label}`}
          onClick={menu.onToggle}
        >
          {content}
          {terminal && (
            <svg className="td-host-icon" width="12" height="12" viewBox="0 0 16 16" aria-hidden="true">
              <path d="M2.5 4.5l4 3.5-4 3.5M8 12h5.5" />
            </svg>
          )}
        </button>
        {menu.open && (
          <HostConnectDialog
            host={terminal ? host.name : label}
            sessionName={menu.sessionName}
            anchorRef={chipRef}
            onCancel={cancel}
            onOpen={menu.onOpen}
            onTypeIn={(type, tmuxSession) => menu.onTypeIn(type, tmuxSession, chipRef.current)}
            terminal={terminal}
            codexHook={
              <HostTools
                host={host.name}
                label={label}
                tools={[codexHookTool({ load: menu.codexHook, install: menu.installCodexHook, setAuto: menu.setCodexHookAuto }), ...(menu.agmsgEnabled ? [agmsgTool()] : [])]}
                alone={!terminal}
              />
            }
          />
        )}
      </li>
    )
  }
  return (
    <li className="td-host" data-status={host.status} title={host.error}>
      {content}
      {host.status === 'error' && host.name !== '' && (
        <button
          type="button"
          className="td-btn td-btn-sm"
          data-reconnect={host.name}
          onClick={() => void onReconnect(host.name)}
        >
          Reconnect
        </button>
      )}
    </li>
  )
}

/** Group sections constrain each sticky header band to its own card row. */
const TaskBoard: React.FC<{
  groups: TaskBoardGroup[]
  grouped: boolean
  renderCard: (task: Task) => React.ReactNode
}> = ({ groups, grouped, renderCard }) => {
  const prefix = useId()
  if (groups.length === 0) return <div className="td-empty">No tasks</div>
  const heading = (column: TaskBoardGroup['columns'][number]) => <>
    <span className="td-colh-dot" style={{ background: COLUMN_COLORS[column.id] }} />
    {column.title}<span className="td-colh-count">{column.tasks.length}</span>
    {column.hint && <span className="td-colh-hint">{column.hint}</span>}
  </>
  return <div className={grouped ? 'td-groups' : 'td-single'} style={{ '--td-column-count': groups[0].columns.length } as React.CSSProperties}>
    {groups.map((group, index) => {
      const groupId = `${prefix}-${index}`
      const bodies = <div className={grouped ? 'td-group-columns' : 'td-cols'}>
        {group.columns.map(column => <section key={column.id} className="td-col" data-column={column.id}
          aria-label={grouped ? `${group.title} — ${column.title}` : column.title}>
          {!grouped && <h2 className="td-colh">{heading(column)}</h2>}
          <div className="td-colbody">
            {column.tasks.length === 0 && <div className="td-empty">None</div>}
            {column.tasks.map(renderCard)}
          </div>
        </section>)}
      </div>
      return grouped ? <section className="td-group" key={group.key} aria-labelledby={groupId}>
        <div className="td-group-band">
          <h2 className="td-group-title" id={groupId}><span>{group.title} · {group.tasks.length} {group.tasks.length === 1 ? 'task' : 'tasks'}</span></h2>
          <div className="td-group-columns">
            {group.columns.map(column => <h3 key={column.id} className="td-colh">{heading(column)}</h3>)}
          </div>
        </div>
        {bodies}
      </section> : <React.Fragment key={group.key}>{bodies}</React.Fragment>
    })}
  </div>
}

interface TaskCardProps {
  task: Task
  pane: TaskPaneRef | null
  selected: boolean
  /** Highlighted briefly after a notification led here. */
  flashing?: boolean
  nowMs: number
  onSelect: (task: Task) => void
  onOpen: (task: Task) => void
  onResume: (task: Task) => void
  resuming: boolean
  onTypeIn: TypeInHandler
  inputMark: TaskInputMark
  /** Whether the registry knows the task's agent (issue #376). */
  knownAgent: boolean
}

const TaskCard: React.FC<TaskCardProps> = ({
  task, pane, selected, flashing = false, nowMs, onSelect, onOpen, onResume, resuming, onTypeIn, inputMark, knownAgent,
}) => {
  const action = taskOpenAction(task, pane)
  const inputAction = taskInputAction(task, pane)
  const age = formatElapsed(task.status_since, nowMs)
  return (
    // The card as a whole is a pointer target; the title button is the
    // keyboard one. Making the card itself a button would nest the links and
    // the open button inside it.
    <article
      className={flashing ? 'td-card td-card-flash' : 'td-card'}
      data-testid={`task-card-${task.id}`}
      data-task-id={task.id}
      data-selected={selected}
      style={stateStyle(task.state)}
      onClick={(event) => {
        // The title button selects on its own; handling its click here too
        // would select twice, and ask for a stopped task's summary twice.
        if ((event.target as HTMLElement).closest('a, button')) return
        onSelect(task)
      }}
    >
      <div className="td-meta">
        <span className="td-meta-host">{hostLabel(task.host)}</span>
        <span>{agentLabel(task.agent, knownAgent)}</span>
        {task.done && <span className="td-done-badge">Done</span>}
        {!task.done && task.summary?.done_candidate && (
          <span className="td-candidate-badge" title="The summary finds no work left">
            Done?
          </span>
        )}
        {inputMark === 'typing' && <span className="td-typing-badge">Typing</span>}
        {age && (
          <span className="td-meta-age" title={`${TASK_STATE_LABELS[task.state]} for ${age}`}>
            {age}
          </span>
        )}
      </div>
      <button type="button" className="td-card-title" data-task-select aria-pressed={selected} onClick={() => onSelect(task)}>
        {taskTitle(task)}
      </button>
      {task.cwd && <div className="td-card-cwd" title={task.cwd}>{task.cwd}</div>}
      {task.state === 'wait' ? (
        <div className="td-why">
          {task.waiting_for || 'waiting'} · open the pane to respond
        </div>
      ) : (
        task.summary?.text && (
          <div className="td-summary" data-outdated={task.summary.outdated ?? false}>
            {task.summary.text}
          </div>
        )
      )}
      <NextWork task={task} />
      <GitLinks task={task} />
      <LabelChips labels={task.labels ?? []} />
      <div className="td-foot">
        <span className="td-where" data-miss={action.kind !== 'goto'}>
          {whereLabel(task, pane)}
        </span>
        <TypeInButton task={task} action={inputAction} mark={inputMark} origin="card" onTypeIn={onTypeIn} small />
        <OpenButton task={task} action={action} onOpen={onOpen} small />
        <ResumeButton task={task} resuming={resuming} onResume={onResume} small />
      </div>
    </article>
  )
}

const TABLE_COLUMNS: { key: TaskSort['key']; title: string }[] = [
  { key: 'session', title: 'Session' }, { key: 'state', title: 'State' },
  { key: 'elapsed', title: 'Elapsed' }, { key: 'host', title: 'Host' },
  { key: 'agent', title: 'Agent' }, { key: 'repository', title: 'Repository' },
  { key: 'branch', title: 'Branch' },
]

const TaskTableRow: React.FC<TaskCardProps & { agentName?: string }> = ({
  task, pane, selected, flashing, nowMs, onSelect, onOpen, onResume, resuming, onTypeIn, inputMark, knownAgent, agentName,
}) => {
  const action = taskOpenAction(task, pane)
  const column = columnForTask(task)
  const summary = task.state === 'wait' ? task.waiting_for || 'waiting' : task.summary?.text
  return (
    <tr data-testid={`task-row-${task.id}`} data-task-id={task.id} data-selected={selected}
      className={flashing ? 'td-card-flash' : undefined}>
      <td className="td-table-session">
        <button type="button" className="td-card-title" data-task-select aria-pressed={selected} onClick={() => onSelect(task)}>{taskTitle(task)}</button>
        {summary && <div className="td-summary" title={summary} data-outdated={task.state !== 'wait' && (task.summary?.outdated ?? false)}>{summary}</div>}
        {inputMark === 'typing' && <span className="td-typing-badge">Typing</span>}
      </td>
      <td><span className="td-table-state" style={{ color: COLUMN_COLORS[column] }}>{TASK_COLUMNS.find((item) => item.id === column)?.title}</span></td>
      <td>{formatElapsed(task.status_since, nowMs) || '—'}</td>
      <td>{hostLabel(task.host)}</td>
      <td>{knownAgent ? agentName || task.agent : agentLabel(task.agent, false)}</td>
      <td>
        {task.git?.repo || '—'}
        {task.git?.pr_url && task.git.pr_number !== undefined && (
          <a className="td-table-pr" href={task.git.pr_url} target="_blank" rel="noopener noreferrer">PR #{task.git.pr_number}</a>
        )}
      </td>
      <td className="td-table-branch" title={task.git?.branch}>{task.git?.branch || '—'}</td>
      <td><LabelChips labels={task.labels ?? []} /></td>
      <td><div className="td-table-actions">
        {action.kind === 'unavailable' && <span className="td-where">{whereLabel(task, pane)}</span>}
        <TypeInButton task={task} action={taskInputAction(task, pane)} mark={inputMark} origin="card" onTypeIn={onTypeIn} small />
        <OpenButton task={task} action={action} onOpen={onOpen} small />
        <ResumeButton task={task} resuming={resuming} onResume={onResume} small />
      </div></td>
    </tr>
  )
}

// The first remaining item of a task's summary and how many remain in all.
const NextWork: React.FC<{ task: Task }> = ({ task }) => {
  const next = summaryNext(task.summary)
  if (!next) return null
  return (
    <div className="td-next" data-testid="task-next" title={next.next}>
      <span className="td-k">Next:</span> {next.next} · {next.left} left
    </div>
  )
}

const GitLinks: React.FC<{ task: Task }> = ({ task }) => {
  const git = task.git
  if (!git || (!git.repo && !git.branch)) return null
  return (
    <div className="td-links">
      <span className="td-branch">
        {git.repo}
        {git.branch && ` ⎇ ${git.branch}`}
      </span>
      {git.pr_url && git.pr_number !== undefined && (
        <a href={git.pr_url} target="_blank" rel="noopener noreferrer">
          PR #{git.pr_number}
        </a>
      )}
      {git.issues?.map((issue) => (
        <a key={issue.url} href={issue.url} target="_blank" rel="noopener noreferrer">
          Issue {issueLabel(issue, git.pr_url)}
        </a>
      ))}
      {git.autolinks?.map((link) => (
        <a key={link.text} href={link.url} target="_blank" rel="noopener noreferrer">
          {link.text}
        </a>
      ))}
    </div>
  )
}

/**
 * `#252` for an issue in the pull request's own repository, and
 * `owner/name#9` for one in another — the pull request can close either.
 */
function issueLabel(issue: TaskIssueLink, prUrl: string | undefined): string {
  const sameRepoPrefix = prUrl?.replace(/\/pull\/\d+$/, '/issues/')
  if (sameRepoPrefix && sameRepoPrefix !== prUrl && issue.url.startsWith(sameRepoPrefix)) {
    return `#${issue.number}`
  }
  return issue.repo ? `${issue.repo}#${issue.number}` : `#${issue.number}`
}

interface LabelChipsProps {
  labels: string[]
  /** Offers a remove button on each label when given. */
  onRemove?: (label: string) => void
  disabled?: boolean
}

const LabelChips: React.FC<LabelChipsProps> = ({ labels, onRemove, disabled = false }) => {
  if (labels.length === 0) return null
  return (
    <ul className="td-labels" aria-label="Labels">
      {labels.map((label) => (
        <li key={label} className="td-label" style={{ '--td-lc': labelColor(label) } as React.CSSProperties}>
          {label}
          {onRemove && (
            <button
              type="button"
              aria-label={`Remove label ${label}`}
              disabled={disabled}
              onClick={() => onRemove(label)}
            >
              ×
            </button>
          )}
        </li>
      ))}
    </ul>
  )
}

// The agent as a card or the detail names it, saying so when the registry
// does not know it (issue #376).
function agentLabel(agent: string, known: boolean): string {
  return known ? agent : `${agent} (unknown agent)`
}

function whereLabel(task: Task, pane: TaskPaneRef | null): string {
  if (pane) return `pane ${pane.paneTitle} · ${pane.workspaceTitle}`
  switch (task.location.kind) {
    case 'tmux':
      return task.location.attachable
        ? `tmux ${task.location.tmux_session} · no pane`
        : `tmux ${task.location.tmux_session} · cannot attach`
    case 'outside':
      return task.location.pane_id ? 'outside tmux · its pane is in no workspace' : 'outside tmux · not in a panemux pane'
    case 'daemon':
      return "codex's shared daemon · cannot open in a pane"
    default:
      return 'not running'
  }
}

// Type in pane (issue #284): a terminal on the task's tmux session in a popup
// over the board. Pressed, it shows Connecting… and takes no second press, so
// one press is one attach.
type TaskInputMark = 'connecting' | 'typing' | null
type TypeInHandler = (task: Task, origin: TaskInputOrigin, element: HTMLElement) => void

interface TypeInButtonProps {
  task: Task
  action: TaskInputAction | null
  mark: TaskInputMark
  origin: TaskInputOrigin
  onTypeIn: TypeInHandler
  small?: boolean
}

const TypeInButton: React.FC<TypeInButtonProps> = ({ task, action, mark, origin, onTypeIn, small = false }) => {
  if (action?.kind !== 'available') return null
  return (
    <button
      type="button"
      className={small ? 'td-btn td-btn-sm td-btn-input' : 'td-btn td-btn-primary'}
      aria-label={`Type in pane: ${taskTitle(task)}`}
      data-type-in={task.id}
      data-origin={origin}
      disabled={mark === 'connecting'}
      onClick={(event) => onTypeIn(task, origin, event.currentTarget)}
    >
      {mark === 'connecting' ? 'Connecting…' : 'Type in pane'}
    </button>
  )
}

interface OpenButtonProps {
  task: Task
  action: TaskOpenAction
  onOpen: (task: Task) => void
  small?: boolean
}

const OpenButton: React.FC<OpenButtonProps> = ({ task, action, onOpen, small = false }) => {
  if (action.kind === 'unavailable') return null
  const label = action.kind === 'goto' ? 'Go to pane' : 'Open'
  return (
    <button
      type="button"
      className={small ? 'td-btn td-btn-sm' : 'td-btn td-btn-primary'}
      aria-label={`${label}: ${taskTitle(task)}`}
      onClick={() => onOpen(task)}
    >
      {label}
    </button>
  )
}

// Resume starts a stopped task again in a new tmux session on its host
// (issues #257 and #264), through its agent's resume adapter. It opens no
// pane; Open does, once the task is running. The button is there when the
// task offers resume, disabled when the server says it is unavailable
// (issue #376); the detail says why.
interface ResumeButtonProps {
  task: Task
  resuming: boolean
  onResume: (task: Task) => void
  small?: boolean
}

const ResumeButton: React.FC<ResumeButtonProps> = ({ task, resuming, onResume, small = false }) => {
  const resume = taskAction(task, 'resume')
  if (!resume) return null
  return (
    <button
      type="button"
      className={small ? 'td-btn td-btn-sm' : 'td-btn td-btn-primary'}
      aria-label={`Resume: ${taskTitle(task)}`}
      disabled={resuming || !canResume(task)}
      onClick={() => onResume(task)}
    >
      {resuming ? 'Resuming…' : 'Resume'}
    </button>
  )
}

interface TaskDetailProps {
  task: Task | null
  pane: TaskPaneRef | null
  open: boolean
  nowMs: number
  onOpen: (task: Task) => void
  onResume: (task: Task) => void
  resuming: boolean
  onTypeIn: TypeInHandler
  inputMark: TaskInputMark
  onSaveRecord: TasksState['saveRecord']
  /** The labels used before (known_labels), offered under Add a label. */
  knownLabels: string[]
  onRequestSummary: TasksState['requestSummary']
  onInputGiven: TasksState['inputGiven']
  summariesEnabled: boolean
  /** Whether the Done column is on screen, for what Mark done says will happen. */
  showDone: boolean
  /** Whether the registry knows the task's agent (issue #376). */
  knownAgent: boolean
  onClose: () => void
}

const TaskDetail: React.FC<TaskDetailProps> = ({
  task,
  pane,
  open,
  nowMs,
  onOpen,
  onResume,
  resuming,
  onTypeIn,
  inputMark,
  onSaveRecord,
  knownLabels,
  onRequestSummary,
  onInputGiven,
  summariesEnabled,
  showDone,
  knownAgent,
  onClose,
}) => {
  const resumeAction = task ? taskAction(task, 'resume') : null
  const [confirmingDone, setConfirmingDone] = useState(false)
  const [labelInput, setLabelInput] = useState('')
  // The task whose save is in flight. A save belongs to the task it was made
  // for: selecting another task while it runs neither disables the other
  // task's controls nor shows the result there.
  const [savingId, setSavingId] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [inputError, setInputError] = useState<string | null>(null)
  const shownId = useRef(task?.id)
  shownId.current = task?.id
  // What was typed or asked belongs to the task it was typed for.
  const [editingId, setEditingId] = useState(task?.id)
  if (editingId !== task?.id) {
    setEditingId(task?.id)
    setConfirmingDone(false)
    setLabelInput('')
    setSaveError(null)
    setInputError(null)
  }

  if (!task) {
    return (
      <aside className="td-detail" data-open={open} aria-label="Task details">
        <div className="td-dbody">
          <p className="td-note">Select a task to see its details.</p>
        </div>
      </aside>
    )
  }

  const action = taskOpenAction(task, pane)
  const inputAction = taskInputAction(task, pane)
  const age = formatElapsed(task.status_since, nowMs)
  const started = formatElapsed(task.started_at, nowMs)
  // The server records nothing for an agent its registry does not know.
  const recordable = canRecord(task) && knownAgent
  const labels = task.labels ?? []
  const done = task.done ?? false
  const saving = savingId === task.id

  // Resolves to whether the save succeeded and the task is still the one shown.
  const save = async (record: { done: boolean; labels: string[] }): Promise<boolean> => {
    const id = task.id
    setSavingId(id)
    const failure = await onSaveRecord(task, record)
    setSavingId((current) => (current === id ? null : current))
    if (shownId.current !== id) return false
    setSaveError(failure)
    return failure === null
  }
  const giveInput = async () => {
    const id = task.id
    setSavingId(id)
    const failure = await onInputGiven(task)
    setSavingId((current) => (current === id ? null : current))
    if (shownId.current === id) setInputError(failure)
  }
  const markDone = async () => {
    if (await save({ done: true, labels })) setConfirmingDone(false)
  }
  const addLabel = async (label: string) => {
    if (await save({ done, labels: [...labels, label] })) setLabelInput('')
  }
  const submitLabel = (event: React.FormEvent) => {
    event.preventDefault()
    const label = labelInput.trim()
    if (label) void addLabel(label)
  }
  const unusedLabels = knownLabels.filter((label) => !labels.includes(label))
  const suggestedLabels = matchLabelSuggestions(unusedLabels, labelInput)
  let suggestionMessage: string | null = null
  if (knownLabels.length > 0 && unusedLabels.length === 0) {
    suggestionMessage = 'Every label used before is on this task.'
  } else if (labelInput.trim() !== '' && suggestedLabels.length === 0) {
    suggestionMessage = `No label used before contains “${labelInput.trim()}”. Add records it as a new label.`
  }

  return (
    <aside className="td-detail" data-open={open} aria-label="Task details">
      <div className="td-dhead">
        <div className="td-dhead-row">
          <span className="td-pill" style={stateStyle(task.state)}>
            {TASK_STATE_LABELS[task.state]}
          </span>
          <span className="td-note">
            {hostLabel(task.host)} · {task.agent}
            {started && ` · started ${started === 'just now' ? 'just now' : `${started} ago`}`}
          </span>
          <button type="button" className="td-close" aria-label="Close task details" onClick={onClose}>
            ×
          </button>
        </div>
        <h2>{taskTitle(task)}</h2>
        {task.state === 'wait' && (
          <div className="td-wait-box">
            Waiting for input: <span className="td-mono">{task.waiting_for || 'unknown reason'}</span>
            {age && ` · ${age === 'just now' ? 'just now' : `for ${age}`}`}
          </div>
        )}
        <div className="td-actions">
          <TypeInButton task={task} action={inputAction} mark={inputMark} origin="detail" onTypeIn={onTypeIn} />
          <OpenButton task={task} action={action} onOpen={onOpen} />
          <ResumeButton task={task} resuming={resuming} onResume={onResume} />
          {isPermissionWait(task) && (
            <button type="button" className="td-btn" disabled={saving} onClick={() => void giveInput()}>
              Input given
            </button>
          )}
          {recordable && !done && (
            <button type="button" className="td-btn" disabled={saving} onClick={() => setConfirmingDone(true)}>
              Mark done
            </button>
          )}
          {recordable && done && (
            <button type="button" className="td-btn" disabled={saving} onClick={() => void save({ done: false, labels })}>
              Mark not done
            </button>
          )}
          {action.kind === 'unavailable' && !canResume(task) && (
            <p className="td-note">Cannot open in a pane: {action.reason}.</p>
          )}
          {resumeAction?.status === 'unavailable' && (
            <p className="td-note">Resume is not available: {actionUnavailableReason(resumeAction)}.</p>
          )}
          {!knownAgent && (
            <p className="td-note">
              panemux does not know the agent {task.agent}, so the dashboard offers only its pane on its tasks.
            </p>
          )}
          {inputAction?.kind === 'unavailable' && (
            <p className="td-note">Type in pane is not available: {inputAction.reason}.</p>
          )}
        </div>
        {confirmingDone && !done && (
          <div className="td-confirm">
            <span>{markDoneQuestion(task, showDone)}</span>
            <button
              type="button"
              className="td-btn td-btn-sm td-btn-primary"
              aria-label="Confirm: mark done"
              disabled={saving}
              onClick={() => void markDone()}
            >
              Mark done
            </button>
            <button type="button" className="td-btn td-btn-sm" onClick={() => setConfirmingDone(false)}>
              Cancel
            </button>
          </div>
        )}
        {inputError && (
          <div role="alert" className="td-alert td-alert-inline">
            Could not mark the input given: {inputError}
          </div>
        )}
        {saveError && (
          <div role="alert" className="td-alert td-alert-inline">
            Could not save: {saveError}
          </div>
        )}
        {recordable && !done && task.summary?.done_candidate && (
          <p className="td-candidate">The summary finds no work left: a candidate for Mark done.</p>
        )}
      </div>
      <div className="td-dbody">
        <StateNote task={task} />
        <WorkSection task={task} enabled={summariesEnabled} onRequest={onRequestSummary} />
        {task.git && (task.git.repo || task.git.branch) && (
          <section className="td-sec">
            <h3>Links</h3>
            <dl className="td-kv">
              <dt>Repository</dt>
              <dd>
                {task.git.repo_url ? (
                  <a href={task.git.repo_url} target="_blank" rel="noopener noreferrer">
                    {task.git.repo || task.git.repo_url}
                  </a>
                ) : (
                  task.git.repo
                )}
              </dd>
              {task.git.branch && (
                <>
                  <dt>Branch</dt>
                  <dd className="td-mono">{task.git.branch}</dd>
                </>
              )}
              <dt>Pull request</dt>
              <dd>
                {task.git.pr_url && task.git.pr_number !== undefined ? (
                  <a href={task.git.pr_url} target="_blank" rel="noopener noreferrer">
                    #{task.git.pr_number}
                  </a>
                ) : (
                  'none for this branch'
                )}
              </dd>
              {task.git.issues && task.git.issues.length > 0 && (
                <>
                  <dt>Issues</dt>
                  <dd>
                    <ul className="td-linklist">
                      {task.git.issues.map((issue) => (
                        <li key={issue.url}>
                          <a href={issue.url} target="_blank" rel="noopener noreferrer">
                            {issueLabel(issue, task.git?.pr_url)}
                          </a>
                        </li>
                      ))}
                    </ul>
                    <span className="td-src">closed by the pull request</span>
                  </dd>
                </>
              )}
              {task.git.autolinks && task.git.autolinks.length > 0 && (
                <>
                  <dt>References</dt>
                  <dd>
                    <ul className="td-linklist">
                      {task.git.autolinks.map((link) => (
                        <li key={link.text}>
                          <a href={link.url} target="_blank" rel="noopener noreferrer">
                            {link.text}
                          </a>
                        </li>
                      ))}
                    </ul>
                    <span className="td-src">from the branch name or pull request title</span>
                  </dd>
                </>
              )}
            </dl>
          </section>
        )}
        <section className="td-sec">
          <h3>Labels</h3>
          {recordable ? (
            <>
              {labels.length > 0 ? (
                <LabelChips
                  labels={labels}
                  disabled={saving}
                  onRemove={(label) => void save({ done, labels: labels.filter((l) => l !== label) })}
                />
              ) : (
                <p className="td-note">No labels.</p>
              )}
              <form className="td-add-label" onSubmit={submitLabel}>
                <input
                  aria-label="Add a label"
                  placeholder="Add a label"
                  value={labelInput}
                  onChange={(event) => setLabelInput(event.target.value)}
                />
                <button type="submit" className="td-btn td-btn-sm" disabled={saving || labelInput.trim() === ''}>
                  Add
                </button>
              </form>
              <LabelSuggestions
                key={task.id}
                labels={suggestedLabels}
                onPick={(label) => void addLabel(label)}
                message={suggestionMessage}
                disabled={saving}
              />
            </>
          ) : knownAgent ? (
            <p className="td-note">Only a task with a session ID can be marked done or labeled.</p>
          ) : (
            <p className="td-note">panemux does not know the agent {task.agent}, so its tasks cannot be marked done or labeled.</p>
          )}
        </section>
        <section className="td-sec">
          <h3>Chain</h3>
          <ul className="td-chain">
            <li data-on="true">
              <span className="td-k">Task</span>
              <span className="td-mono">{task.session_id ?? task.id}</span>
            </li>
            <li data-on={task.pid !== undefined}>
              <span className="td-k">Agent</span>
              <span>
                {task.pid !== undefined ? `${agentLabel(task.agent, knownAgent)} pid ${task.pid}` : 'no process'}
              </span>
            </li>
            <li data-on={task.location.kind === 'tmux'}>
              <span className="td-k">Runs in</span>
              <span>
                {task.location.kind === 'tmux'
                  ? `tmux ${task.location.tmux_session}`
                  : task.location.kind === 'outside'
                    ? 'outside tmux'
                    : task.location.kind === 'daemon'
                      ? "codex's shared daemon"
                      : 'nowhere'}
              </span>
            </li>
            <li data-on={pane !== null}>
              <span className="td-k">Pane</span>
              <span>{pane ? pane.paneTitle : action.kind === 'open' ? 'none yet (Open creates one)' : 'none'}</span>
            </li>
            {pane && (
              <li data-on="true">
                <span className="td-k">Workspace</span>
                <span>{pane.workspaceTitle}</span>
              </li>
            )}
          </ul>
        </section>
        <section className="td-sec">
          <h3>Location</h3>
          <dl className="td-kv">
            <dt>Directory</dt>
            <dd className="td-mono">{task.cwd || 'unknown'}</dd>
            <dt>Session</dt>
            <dd className="td-mono">{task.session_id || 'unknown'}</dd>
          </dl>
        </section>
      </div>
    </aside>
  )
}

// What Mark done asks, saying where the task will be afterwards: a running
// task stays where it is until it stops, and a stopped one leaves the board
// while the Done column is hidden.
function markDoneQuestion(task: Task, showDone: boolean): string {
  if (task.state !== 'stop') {
    return 'Mark this task done? It stays in its column while it runs, and moves to Done when it stops.'
  }
  if (showDone) return 'Mark this task done? It moves to the Done column.'
  return "Mark this task done? It moves to the Done column, which is hidden until 'Show done' is checked."
}

// What the task's state means: the note its agent's collect adapter gives
// (state_note, issue #376), or what a stopped task means for every agent.
const StateNote: React.FC<{ task: Task }> = ({ task }) => {
  let note = task.state_note ?? null
  if (!note && task.state === 'stop') {
    note = 'No running process handles this session. A host restart, a crash and a normal exit all look the same.'
  }
  return note ? <p className="td-note">{note}</p> : null
}

interface WorkSectionProps {
  task: Task
  enabled: boolean
  onRequest: TasksState['requestSummary']
}

// What the task is doing and what is left, from its summary (issue #258).
const WorkSection: React.FC<WorkSectionProps> = ({ task, enabled, onRequest }) => {
  // A failed request belongs to the task it was made for.
  const [requestError, setRequestError] = useState<{ id: string; message: string } | null>(null)
  const summary = task.summary
  const summaryAction = taskAction(task, 'summary')
  let body: React.ReactNode
  if (!enabled) {
    body = (
      <p className="td-note">
        Summaries are off. Set task_dashboard.summary.enabled in config.yaml to have each agent on this host summarize
        each task&apos;s conversation.
      </p>
    )
  } else if (!summaryAction) {
    body = <p className="td-note">{task.agent} does not summarize its tasks.</p>
  } else {
    // A log that could not be read reads the same until it changes, and the
    // server does not read it again before then: the button stays, disabled,
    // until the log has changed.
    const unreadable = summary?.state === 'unreadable' && !summary.outdated
    // Unavailable (no session ID or log yet, issue #376): the button stays, disabled, with the reason.
    const unavailable = !canSummarize(task)
    const retry = summary?.state !== 'pending' && (!summary?.text || summary.outdated || summary.state === 'error')
    const request = async () => {
      const failure = await onRequest(task)
      setRequestError(failure ? { id: task.id, message: failure } : null)
    }
    body = (
      <>
        {summary?.text && <p className="td-summary-text">{summary.text}</p>}
        <SummaryStatus task={task} />
        {summary?.remaining && summary.remaining.length > 0 && (
          <>
            <h4>Remaining</h4>
            <ol className="td-remaining">
              {summary.remaining.map((item, i) => (
                <li key={i}>{item}</li>
              ))}
            </ol>
          </>
        )}
        {summary?.text && summary.unexpected_model && (
          <p className="td-note" data-testid="task-summary-model">
            Summarized by {summary.unexpected_model}, not Haiku, at many times the cost: this account may not use Haiku.
          </p>
        )}
        {summary?.state === 'ready' && summary.text && !summary.outdated && (summary.remaining ?? []).length === 0 && (
          <p className="td-note">No remaining work found.</p>
        )}
        {retry && (
          <button
            type="button"
            className="td-btn td-btn-sm"
            disabled={unreadable || unavailable}
            title={unreadable ? 'The conversation log cannot be read, so it cannot be summarized.' : undefined}
            onClick={() => void request()}
          >
            {summary?.text || summary?.state === 'error' ? 'Summarize again' : 'Summarize'}
          </button>
        )}
        {unavailable && (
          <p className="td-note">Summarize is not available: {actionUnavailableReason(summaryAction)}.</p>
        )}
        {requestError?.id === task.id && (
          <div role="alert" className="td-alert td-alert-inline">
            Could not ask for a summary: {requestError.message}
          </div>
        )}
      </>
    )
  }
  return (
    <section className="td-sec" aria-label="Work">
      <h3>Work</h3>
      {body}
    </section>
  )
}

const SummaryStatus: React.FC<{ task: Task }> = ({ task }) => {
  const summary = task.summary
  let note: string | null = null
  switch (summary?.state) {
    case undefined:
      note = task.state === 'busy' ? 'Summarized when it stops working.' : 'Not summarized yet.'
      break
    case 'pending':
      note = 'Summarizing…'
      break
    case 'error':
      note = `Could not summarize: ${summary.error ?? 'unknown error'}`
      break
    case 'unreadable':
      note = 'The conversation log has no messages the dashboard can read; its format may have changed.'
      break
  }
  if (summary?.outdated && summary.state !== 'pending') {
    const since = summary.state === 'unreadable' ? 'since it could not be read.' : 'since this summary.'
    note = [note, `The conversation has changed ${since}`].filter(Boolean).join(' ')
  }
  return note ? <p className="td-note">{note}</p> : null
}
