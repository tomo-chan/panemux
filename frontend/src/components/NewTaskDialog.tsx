import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useModalKeyboard } from '../hooks/useModalKeyboard'
import type { TaskActionResult, TaskAgent, TaskLaunchInput } from '../hooks/useTasks'
import type { Task, TaskHost, TaskLaunchResponse } from '../schemas'
import { hostLabel, parseLabelInput } from '../utils/taskBoard'
import { lastLabelToken, matchLabelSuggestions, toggleLabelInput } from '../utils/labelSuggestions'
import { LabelSuggestions } from './LabelSuggestions'
import { recentWorkdirs } from '../utils/workdirSuggestions'
import { WorkdirCombobox } from './WorkdirCombobox'

// The task dashboard's New task form (issues #257 and #264): a host, a working
// directory, an agent, labels and the first instruction. Starting a task runs
// claude or codex in a detached tmux session on the host; no pane is opened.
// The server checks every field again — what this form checks is only what
// can be told without asking it.

export interface NewTaskDialogProps {
  isOpen: boolean
  hosts: TaskHost[]
  /** The labels used before (known_labels), offered under the Labels field. */
  knownLabels?: string[]
  /** The tasks on the board: the directories they ran in are offered under Working directory. */
  tasks?: Task[]
  onLaunch: (input: TaskLaunchInput) => Promise<TaskActionResult<TaskLaunchResponse>>
  /** Called with the started task, its host and agent; the dialog is then the caller's to close. */
  onLaunched: (launched: TaskLaunchResponse, host: string, agent: TaskAgent) => void
  onClose: () => void
}

export const NewTaskDialog: React.FC<NewTaskDialogProps> = ({
  isOpen,
  hosts,
  knownLabels = [],
  tasks,
  onLaunch,
  onLaunched,
  onClose,
}) => {
  const [host, setHost] = useState('')
  const [agent, setAgent] = useState<TaskAgent>('claude')
  const [cwd, setCwd] = useState('')
  const [cwdListOpen, setCwdListOpen] = useState(false)
  const [labels, setLabels] = useState('')
  const [prompt, setPrompt] = useState('')
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const dialogRef = useRef<HTMLDivElement>(null)
  const cwdRef = useRef<HTMLInputElement>(null)
  // The focus the dialog gives the field on opening does not open its list,
  // so Escape still closes the dialog straight away.
  const focusingOnOpen = useRef(false)
  const workdirs = useMemo(() => (isOpen ? recentWorkdirs(tasks ?? [], host) : []), [isOpen, tasks, host])

  useEffect(() => {
    if (!isOpen) {
      setCwdListOpen(false)
      return
    }
    focusingOnOpen.current = true
    cwdRef.current?.focus()
    focusingOnOpen.current = false
  }, [isOpen])

  // A launch in flight cannot be dismissed out from under itself. Escape
  // closes the directory list first, when it is open.
  const escape = cwdListOpen ? () => setCwdListOpen(false) : onClose
  useModalKeyboard({ isOpen, dialogRef, onEscape: starting ? undefined : escape })

  if (!isOpen) return null

  // Filtered by what is typed after the last comma; an entered label stays.
  const enteredLabels = parseLabelInput(labels)
  const typedLabel = lastLabelToken(labels)
  const suggestedLabels = matchLabelSuggestions(knownLabels, typedLabel, { keep: enteredLabels })
  // "No match" only when no known label contains the typed text, whether or
  // not the ones that do are already entered.
  const noSuggestion =
    typedLabel !== '' && knownLabels.length > 0 && matchLabelSuggestions(knownLabels, typedLabel).length === 0

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    const dir = cwd.trim()
    if (dir === '') {
      setError('Enter the working directory.')
      return
    }
    if (!dir.startsWith('/')) {
      setError('The working directory must be an absolute path.')
      return
    }
    if (prompt.trim() === '') {
      setError('Enter the first instruction.')
      return
    }
    setError(null)
    setStarting(true)
    const result = await onLaunch({ host, agent, cwd: dir, prompt, labels: parseLabelInput(labels) })
    setStarting(false)
    if (!result.ok) {
      setError(`Could not start: ${result.error}`)
      return
    }
    onLaunched(result.launched, host, agent)
  }

  return (
    <div
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-labelledby="td-new-title"
      className="td-modal"
      onClick={(event) => {
        if (event.target === event.currentTarget && !starting) onClose()
      }}
    >
      <form className="td-modal-panel" onSubmit={(event) => void submit(event)}>
        <h2 id="td-new-title">New task</h2>
        <p className="td-note">
          Starts the agent in a tmux session of its own on the host. No pane is opened; open the task from the
          dashboard when you want to watch it.
        </p>
        <label className="td-field">
          <span>Host</span>
          <select
            value={host}
            onChange={(event) => {
              // A directory means something else on another host.
              setHost(event.target.value)
              setCwd('')
            }}
            disabled={starting}
          >
            {hosts.map((h) => (
              <option key={h.name} value={h.name}>
                {hostLabel(h.name)}
                {h.status === 'error' ? ' (unreachable)' : ''}
              </option>
            ))}
          </select>
        </label>
        <WorkdirCombobox
          inputRef={cwdRef}
          value={cwd}
          onChange={setCwd}
          hostName={hostLabel(host)}
          suggestions={workdirs}
          open={cwdListOpen && !starting}
          onOpenChange={setCwdListOpen}
          openOnFocus={() => !focusingOnOpen.current}
          disabled={starting}
        />
        <label className="td-field">
          <span>Agent</span>
          <select value={agent} disabled={starting} onChange={(event) => setAgent(event.target.value as TaskAgent)}>
            <option value="claude">claude</option>
            <option value="codex">codex</option>
          </select>
        </label>
        {agent === 'codex' && (
          <p className="td-note">
            Codex chooses its session ID once it has started its session, so the labels are recorded once codex has
            started its session. If it stops at a start-up screen, open the task's pane to answer it.
          </p>
        )}
        <label className="td-field">
          <span>Labels</span>
          <input
            value={labels}
            placeholder="comma-separated, optional"
            onChange={(event) => setLabels(event.target.value)}
            disabled={starting}
            autoComplete="off"
          />
        </label>
        <LabelSuggestions
          labels={suggestedLabels}
          pressed={enteredLabels}
          onPick={(label) => setLabels((current) => toggleLabelInput(current, label, knownLabels))}
          message={noSuggestion ? `No label used before contains “${typedLabel}”. It is added as a new label.` : null}
          disabled={starting}
        />
        <label className="td-field">
          <span>First instruction</span>
          <textarea
            value={prompt}
            rows={6}
            onChange={(event) => setPrompt(event.target.value)}
            disabled={starting}
          />
        </label>
        {error && (
          <div role="alert" className="td-alert td-alert-inline">
            {error}
          </div>
        )}
        <div className="td-modal-actions">
          <button type="button" className="td-btn" onClick={onClose} disabled={starting}>
            Cancel
          </button>
          <button type="submit" className="td-btn td-btn-primary" disabled={starting}>
            {starting ? 'Starting…' : 'Start'}
          </button>
        </div>
      </form>
    </div>
  )
}
