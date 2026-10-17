import React, { useState } from 'react'
import { CODEX_PERMISSION_WAIT } from '../utils/taskBoard'

interface PaneApprovalNoticeProps {
  /** How many tasks of the pane wait on a codex permission request: at least one. */
  waits: number
  /** Marks the one task's request answered, answering why it failed or null. */
  onInputGiven: () => Promise<string | null>
  /** Opens the task dashboard on the first of several tasks. */
  onReview?: () => void
}

const barStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  flexWrap: 'wrap',
  gap: '4px 8px',
  flexShrink: 0,
  padding: '3px 10px',
  fontSize: '12px',
  color: '#f0d49c',
  backgroundColor: 'rgba(226, 184, 107, 0.1)',
  borderBottom: '1px solid rgba(226, 184, 107, 0.45)',
}

const buttonStyle: React.CSSProperties = {
  padding: '1px 10px',
  backgroundColor: '#3f3f46',
  color: '#d4d4d4',
  border: '1px solid #52525b',
  borderRadius: '4px',
  fontSize: '12px',
  cursor: 'pointer',
  flexShrink: 0,
}

/**
 * The one-line strip a pane shows, between its header and its terminal,
 * while a task it runs waits on a codex permission request (issue #404), in
 * the Tasks detail's words. It handles no key, so every key typed in the pane
 * stays the terminal's; only Input given or the end of the wait removes it.
 */
export const PaneApprovalNotice: React.FC<PaneApprovalNoticeProps> = ({ waits, onInputGiven, onReview }) => {
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const giveInput = async () => {
    setSaving(true)
    const failure = await onInputGiven()
    setSaving(false)
    setError(failure)
  }

  return (
    <div role="status" aria-label="Codex permission request" style={barStyle}>
      <span style={{ flex: 1, minWidth: 0 }}>
        Waiting for input: {CODEX_PERMISSION_WAIT}
        {waits > 1 && ` · ${waits} tasks`}
      </span>
      {waits > 1 ? (
        <button type="button" style={buttonStyle} onClick={onReview}>
          Review in Tasks
        </button>
      ) : (
        <button type="button" style={buttonStyle} disabled={saving} onClick={() => void giveInput()}>
          Input given
        </button>
      )}
      {error && (
        <span role="alert" style={{ flexBasis: '100%', color: '#f48771' }}>
          Could not mark the input given: {error}
        </span>
      )}
    </div>
  )
}
