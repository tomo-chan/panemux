import React, { useEffect, useRef, useState } from 'react'
import { useModalKeyboard } from '../hooks/useModalKeyboard'
import type { TaskActionResult, TaskAgent, TaskLaunchInput } from '../hooks/useTasks'
import type { TaskHost, TaskLaunchResponse } from '../schemas'
import { hostLabel, parseLabelInput } from '../utils/taskBoard'
import { lastLabelToken, matchLabelSuggestions, toggleLabelInput } from '../utils/labelSuggestions'
import { LabelSuggestions } from './LabelSuggestions'

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
  onLaunch: (input: TaskLaunchInput) => Promise<TaskActionResult<TaskLaunchResponse>>
  /** Called with the started task, its host and agent; the dialog is then the caller's to close. */
  onLaunched: (launched: TaskLaunchResponse, host: string, agent: TaskAgent) => void
  onClose: () => void
}

export const NewTaskDialog: React.FC<NewTaskDialogProps> = ({
  isOpen,
  hosts,
  knownLabels = [],
  onLaunch,
  onLaunched,
  onClose,
}) => {
  const [host, setHost] = useState('')
  const [agent, setAgent] = useState<TaskAgent>('claude')
  const [cwd, setCwd] = useState('')
  const [labels, setLabels] = useState('')
  const [prompt, setPrompt] = useState('')
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const dialogRef = useRef<HTMLDivElement>(null)
  const cwdRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (isOpen) cwdRef.current?.focus()
  }, [isOpen])

  // A launch in flight cannot be dismissed out from under itself.
  useModalKeyboard({ isOpen, dialogRef, onEscape: starting ? undefined : onClose })

  if (!isOpen) return null

  // Filtered by what is typed after the last comma; an entered label stays.
  const enteredLabels = parseLabelInput(labels)
  const typedLabel = lastLabelToken(labels)
  const suggestedLabels = matchLabelSuggestions(knownLabels, typedLabel, { keep: enteredLabels })
  const noSuggestion =
    typedLabel !== '' &&
    knownLabels.length > 0 &&
    !knownLabels.includes(typedLabel) &&
    suggestedLabels.every((label) => enteredLabels.includes(label))

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
          <select value={host} onChange={(event) => setHost(event.target.value)} disabled={starting}>
            {hosts.map((h) => (
              <option key={h.name} value={h.name}>
                {hostLabel(h.name)}
                {h.status === 'error' ? ' (unreachable)' : ''}
              </option>
            ))}
          </select>
        </label>
        <label className="td-field">
          <span>Working directory</span>
          <input
            ref={cwdRef}
            className="td-mono"
            value={cwd}
            placeholder="/workspace/user/project"
            onChange={(event) => setCwd(event.target.value)}
            disabled={starting}
            spellCheck={false}
            autoComplete="off"
          />
        </label>
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
          onPick={(label) => setLabels((current) => toggleLabelInput(current, label))}
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
