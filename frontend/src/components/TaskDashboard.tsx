import React, { useEffect, useMemo, useRef, useState } from 'react'
import type { Task, TaskHost, TaskLaunchResponse, Workspace } from '../schemas'
import type { TasksState } from '../hooks/useTasks'
import { TASKS_POLL_INTERVAL_MS } from '../hooks/useTasks'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'
import { NewTaskDialog } from './NewTaskDialog'
import {
  TASK_STATE_LABELS,
  allLabels,
  canRecord,
  canResume,
  columnForTask,
  filterTasks,
  findTaskPane,
  formatElapsed,
  groupIntoLanes,
  hostLabel,
  labelColor,
  runningCount,
  taskOpenAction,
  taskTitle,
  visibleColumns,
} from '../utils/taskBoard'
import type { LaneMode, TaskOpenAction, TaskPaneRef } from '../utils/taskBoard'

// Layer 1 of issue #252: every agent session on every host, as a kanban by
// state, with a detail panel on the right. Besides reading, it records what a
// person says about a task — done, and its labels (issue #256) — through
// tasksState.saveRecord, and starts new tasks and resumes stopped ones
// (issue #257) through tasksState.launch and tasksState.resume. Opening a
// task is App's job (onOpenTask), because that means creating or focusing a
// pane.

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

export interface TaskDashboardProps {
  tasksState: TasksState
  workspaces: Workspace[]
  onOpenTask: (task: Task, action: TaskOpenAction) => void
  onShowWorkspaces: () => void
  /** The layer-switching shortcut, as shown and as aria-keyshortcuts spells it. */
  shortcut?: { label: string; aria: string }
  /** Clock for elapsed times; injectable for tests. */
  now?: () => number
}

type StateVars = React.CSSProperties & Record<'--td-sc', string>

function stateStyle(state: Task['state']): StateVars {
  return { '--td-sc': STATE_COLORS[state] }
}

export const TaskDashboard: React.FC<TaskDashboardProps> = ({
  tasksState,
  workspaces,
  onOpenTask,
  onShowWorkspaces,
  shortcut,
  now = Date.now,
}) => {
  const { data, error, loading, updatedAt, refresh, reconnect, saveRecord, launch, resume } = tasksState
  const [query, setQuery] = useState('')
  const [laneMode, setLaneMode] = useState<LaneMode>('none')
  const [hostFilter, setHostFilter] = useState(ALL_HOSTS)
  const [labelFilter, setLabelFilter] = useState(ALL_LABELS)
  const [showDone, setShowDone] = useState(false)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [newTaskOpen, setNewTaskOpen] = useState(false)
  // A task that was started but is not listed yet: claude writes the state
  // the collection reads only once it is running. It is selected when it
  // appears, unless another task was selected meanwhile.
  const [pendingLaunch, setPendingLaunch] = useState<{ id: string; message: string } | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [resumingId, setResumingId] = useState<string | null>(null)
  const nowMs = useTicker(now)
  const rootRef = useRef<HTMLElement>(null)

  // Focus moves into the dashboard when it appears, so typing does not keep
  // going to the terminal underneath it.
  useEffect(() => {
    rootRef.current?.focus({ preventScroll: true })
  }, [])

  const tasks = useMemo(() => data?.tasks ?? [], [data])
  const hosts = data?.hosts ?? []
  const labels = useMemo(() => allLabels(tasks), [tasks])
  // A label no task carries any more falls back to every label.
  const activeLabel = labelFilter !== ALL_LABELS && labels.includes(labelFilter) ? labelFilter : null
  const visible = useMemo(
    () => filterTasks(tasks, { query, host: hostFilter === ALL_HOSTS ? null : hostFilter, label: activeLabel }),
    [activeLabel, hostFilter, query, tasks],
  )
  const panes = useMemo(() => {
    const byTask = new Map<string, TaskPaneRef | null>()
    for (const task of tasks) byTask.set(task.id, findTaskPane(task, workspaces))
    return byTask
  }, [tasks, workspaces])
  const selected = tasks.find((task) => task.id === selectedId) ?? null

  if (pendingLaunch && tasks.some((task) => task.id === pendingLaunch.id)) {
    setPendingLaunch(null)
    setSelectedId(pendingLaunch.id)
    setDetailOpen(true)
  }

  const select = (task: Task) => {
    setPendingLaunch(null)
    setSelectedId(task.id)
    setDetailOpen(true)
  }
  const launched = (result: TaskLaunchResponse, host: string) => {
    setNewTaskOpen(false)
    setActionError(
      result.records_error ? `The task started, but its labels could not be saved: ${result.records_error}` : null,
    )
    setPendingLaunch({
      id: result.id,
      message: `Started tmux ${result.tmux_session} on ${hostLabel(host)}. It is selected here once claude has started.`,
    })
  }
  const resumeTask = async (task: Task) => {
    setResumingId(task.id)
    setActionError(null)
    const result = await resume(task)
    setResumingId((current) => (current === task.id ? null : current))
    if (!result.ok) {
      setActionError(`Could not resume ${taskTitle(task)}: ${result.error}`)
      return
    }
    select(task)
  }
  const open = (task: Task) => onOpenTask(task, taskOpenAction(task, panes.get(task.id) ?? null))

  return (
    <section
      ref={rootRef}
      tabIndex={-1}
      className="td-root"
      aria-label="Task dashboard"
      style={{ '--td-font': TERMINAL_FONT_FAMILY, '--td-mono': TERMINAL_FONT_FAMILY } as React.CSSProperties}
    >
      <header className="td-top">
        <h1>Tasks</h1>
        <ul className="td-hosts" aria-label="Hosts">
          {hosts.map((host) => (
            <HostChip key={host.name} host={host} running={runningCount(tasks, host.name)} onReconnect={reconnect} />
          ))}
        </ul>
        <span className="td-spacer" />
        <span className="td-updated" aria-live="polite">
          {updatedLabel(loading, updatedAt, nowMs)}
        </span>
        <button type="button" className="td-btn" onClick={() => void refresh()} disabled={loading}>
          Refresh
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

      <div className="td-tools">
        <input
          type="search"
          placeholder="Filter by directory, branch, PR or session"
          aria-label="Filter tasks"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        <label>
          Rows
          <select aria-label="Split rows by" value={laneMode} onChange={(event) => setLaneMode(event.target.value as LaneMode)}>
            <option value="none">None</option>
            <option value="host">Host</option>
            <option value="label">Label</option>
            <option value="repo">Repository</option>
          </select>
        </label>
        <label>
          Host
          <select aria-label="Show host" value={hostFilter} onChange={(event) => setHostFilter(event.target.value)}>
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
          <select aria-label="Show label" value={activeLabel ?? ALL_LABELS} onChange={(event) => setLabelFilter(event.target.value)}>
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
            aria-label="Show Done column"
            checked={showDone}
            onChange={(event) => setShowDone(event.target.checked)}
          />
          Done column
        </label>
      </div>

      <div className="td-main">
        <div className="td-board">
          <div className="td-cols">
            {visibleColumns(showDone).map((column) => {
              const inColumn = visible.filter((task) => columnForTask(task) === column.id)
              return (
                <section key={column.id} className="td-col" data-column={column.id} aria-label={column.title}>
                  <h2 className="td-colh">
                    <span className="td-colh-dot" style={{ background: COLUMN_COLORS[column.id] }} />
                    {column.title}
                    <span className="td-colh-count">{inColumn.length}</span>
                    {column.hint && <span className="td-colh-hint">{column.hint}</span>}
                  </h2>
                  <div className="td-colbody">
                    {inColumn.length === 0 && <div className="td-empty">None</div>}
                    {groupIntoLanes(inColumn, laneMode).map((lane) => (
                      <React.Fragment key={lane.key}>
                        {laneMode !== 'none' && <h3 className="td-lane">{lane.title}</h3>}
                        {lane.tasks.map((task) => (
                          <TaskCard
                            key={task.id}
                            task={task}
                            pane={panes.get(task.id) ?? null}
                            selected={task.id === selectedId}
                            nowMs={nowMs}
                            onSelect={select}
                            onOpen={open}
                            onResume={(t) => void resumeTask(t)}
                            resuming={resumingId === task.id}
                          />
                        ))}
                      </React.Fragment>
                    ))}
                  </div>
                </section>
              )
            })}
          </div>
        </div>
        <TaskDetail
          task={selected}
          pane={selected ? panes.get(selected.id) ?? null : null}
          open={detailOpen}
          nowMs={nowMs}
          onOpen={open}
          onResume={(t) => void resumeTask(t)}
          resuming={selected !== null && resumingId === selected.id}
          onSaveRecord={saveRecord}
          showDone={showDone}
          onClose={() => setDetailOpen(false)}
        />
      </div>
      <NewTaskDialog
        isOpen={newTaskOpen}
        hosts={hosts}
        onLaunch={launch}
        onLaunched={launched}
        onClose={() => setNewTaskOpen(false)}
      />
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
}

const HostChip: React.FC<HostChipProps> = ({ host, running, onReconnect }) => {
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
  return (
    <li className="td-host" data-status={host.status} title={host.error}>
      <span className="td-host-dot" aria-hidden="true" />
      <b>{label}</b>{' '}
      <span className="td-host-detail">{detail}</span>
      {host.status === 'error' && host.name !== '' && (
        <button type="button" className="td-btn td-btn-sm" onClick={() => void onReconnect(host.name)}>
          Reconnect
        </button>
      )}
    </li>
  )
}

interface TaskCardProps {
  task: Task
  pane: TaskPaneRef | null
  selected: boolean
  nowMs: number
  onSelect: (task: Task) => void
  onOpen: (task: Task) => void
  onResume: (task: Task) => void
  resuming: boolean
}

const TaskCard: React.FC<TaskCardProps> = ({ task, pane, selected, nowMs, onSelect, onOpen, onResume, resuming }) => {
  const action = taskOpenAction(task, pane)
  const age = formatElapsed(task.status_since, nowMs)
  return (
    // The card as a whole is a pointer target; the title button is the
    // keyboard one. Making the card itself a button would nest the links and
    // the open button inside it.
    <article
      className="td-card"
      data-testid={`task-card-${task.id}`}
      data-selected={selected}
      style={stateStyle(task.state)}
      onClick={(event) => {
        if ((event.target as HTMLElement).closest('a, button:not(.td-card-title)')) return
        onSelect(task)
      }}
    >
      <div className="td-meta">
        <span className="td-meta-host">{hostLabel(task.host)}</span>
        <span>{task.agent}</span>
        {task.done && <span className="td-done-badge">Done</span>}
        {age && (
          <span className="td-meta-age" title={`${TASK_STATE_LABELS[task.state]} for ${age}`}>
            {age}
          </span>
        )}
      </div>
      <button type="button" className="td-card-title" aria-pressed={selected} onClick={() => onSelect(task)}>
        {taskTitle(task)}
      </button>
      {task.cwd && <div className="td-card-cwd" title={task.cwd}>{task.cwd}</div>}
      {task.state === 'wait' && (
        <div className="td-why">
          {task.waiting_for || 'waiting'} · open the pane to respond
        </div>
      )}
      <GitLinks task={task} />
      <LabelChips labels={task.labels ?? []} />
      <div className="td-foot">
        <span className="td-where" data-miss={action.kind !== 'goto'}>
          {whereLabel(task, pane)}
        </span>
        <OpenButton task={task} action={action} onOpen={onOpen} small />
        <ResumeButton task={task} resuming={resuming} onResume={onResume} small />
      </div>
    </article>
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
    </div>
  )
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

function whereLabel(task: Task, pane: TaskPaneRef | null): string {
  if (pane) return `pane ${pane.paneTitle} · ${pane.workspaceTitle}`
  switch (task.location.kind) {
    case 'tmux':
      return task.location.attachable
        ? `tmux ${task.location.tmux_session} · no pane`
        : `tmux ${task.location.tmux_session} · cannot attach`
    case 'outside':
      return 'outside tmux · cannot open in a pane'
    default:
      return 'not running'
  }
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

// Resume runs `claude --resume` for a stopped claude task in a new tmux
// session on its host (issue #257). It opens no pane; Open does, once the
// task is running.
interface ResumeButtonProps {
  task: Task
  resuming: boolean
  onResume: (task: Task) => void
  small?: boolean
}

const ResumeButton: React.FC<ResumeButtonProps> = ({ task, resuming, onResume, small = false }) => {
  if (!canResume(task)) return null
  return (
    <button
      type="button"
      className={small ? 'td-btn td-btn-sm' : 'td-btn td-btn-primary'}
      aria-label={`Resume: ${taskTitle(task)}`}
      disabled={resuming}
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
  onSaveRecord: TasksState['saveRecord']
  /** Whether the Done column is on screen, for what Mark done says will happen. */
  showDone: boolean
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
  onSaveRecord,
  showDone,
  onClose,
}) => {
  const [confirmingDone, setConfirmingDone] = useState(false)
  const [labelInput, setLabelInput] = useState('')
  // The task whose save is in flight. A save belongs to the task it was made
  // for: selecting another task while it runs neither disables the other
  // task's controls nor shows the result there.
  const [savingId, setSavingId] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const shownId = useRef(task?.id)
  shownId.current = task?.id
  // What was typed or asked belongs to the task it was typed for.
  const [editingId, setEditingId] = useState(task?.id)
  if (editingId !== task?.id) {
    setEditingId(task?.id)
    setConfirmingDone(false)
    setLabelInput('')
    setSaveError(null)
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
  const age = formatElapsed(task.status_since, nowMs)
  const started = formatElapsed(task.started_at, nowMs)
  const recordable = canRecord(task)
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
  const markDone = async () => {
    if (await save({ done: true, labels })) setConfirmingDone(false)
  }
  const addLabel = async (event: React.FormEvent) => {
    event.preventDefault()
    const label = labelInput.trim()
    if (!label) return
    if (await save({ done, labels: [...labels, label] })) setLabelInput('')
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
          <OpenButton task={task} action={action} onOpen={onOpen} />
          <ResumeButton task={task} resuming={resuming} onResume={onResume} />
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
        {saveError && (
          <div role="alert" className="td-alert td-alert-inline">
            Could not save: {saveError}
          </div>
        )}
      </div>
      <div className="td-dbody">
        <StateNote task={task} />
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
              <form className="td-add-label" onSubmit={(event) => void addLabel(event)}>
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
            </>
          ) : (
            <p className="td-note">Only a task with a session ID can be marked done or labeled.</p>
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
              <span>{task.pid !== undefined ? `${task.agent} pid ${task.pid}` : 'no process'}</span>
            </li>
            <li data-on={task.location.kind === 'tmux'}>
              <span className="td-k">Runs in</span>
              <span>
                {task.location.kind === 'tmux'
                  ? `tmux ${task.location.tmux_session}`
                  : task.location.kind === 'outside'
                    ? 'outside tmux'
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
  return "Mark this task done? It moves to the Done column, which is hidden until 'Done column' is checked."
}

const StateNote: React.FC<{ task: Task }> = ({ task }) => {
  let note: string | null = null
  switch (task.state) {
    case 'run':
      note = `${task.agent} reports no detailed state; the dashboard only knows it is running.`
      break
    case 'unknown':
      note = task.session_id
        ? 'Claude Code reported a status the dashboard does not recognize.'
        : 'The session state file under ~/.claude/sessions could not be read or has an unexpected format.'
      break
    case 'stop':
      note =
        'No running process handles this session. A host restart, a crash and a normal exit all look the same.'
      break
  }
  return note ? <p className="td-note">{note}</p> : null
}
