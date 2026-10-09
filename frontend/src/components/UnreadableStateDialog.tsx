import { useRef, useState } from 'react'
import { useModalKeyboard } from '../hooks/useModalKeyboard'
import type { TaskHost } from '../schemas'
import { hostLabel } from '../utils/taskBoard'
import {
  READ_STATE_FIELDS,
  UNREADABLE_DOC_URL,
  unreadableDetailsText,
  unreadableProcess,
  unreadableReasonLabel,
  unreadableRows,
  unreadableTitle,
} from '../utils/unreadableState'

export interface UnreadableStateDialogProps {
  isOpen: boolean
  hosts: TaskHost[]
  onClose: () => void
}

/**
 * The details behind the dashboard's unreadable-state warning (issue #313):
 * one row per Claude Code state file a host could not read, with why and the
 * process its name points to. Every value comes from the host and is shown
 * as text only.
 */
export default function UnreadableStateDialog({ isOpen, hosts, onClose }: UnreadableStateDialogProps) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const [copyStatus, setCopyStatus] = useState<string | null>(null)
  useModalKeyboard({ isOpen, dialogRef, onEscape: onClose })
  if (!isOpen) return null

  const rows = unreadableRows(hosts)
  const clipboard = typeof navigator === 'undefined' ? undefined : navigator.clipboard
  const copy = async () => {
    try {
      await clipboard?.writeText(unreadableDetailsText(rows))
      setCopyStatus('Copied')
    } catch (err) {
      setCopyStatus(`Could not copy: ${err instanceof Error ? err.message : String(err)}`)
    }
  }

  return (
    <div
      ref={dialogRef}
      id="td-unreadable-dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby="td-unreadable-title"
      className="td-modal td-unreadable-modal"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="td-modal-panel td-unreadable-panel">
        <div className="td-unreadable-head">
          <div>
            <h2 id="td-unreadable-title">{unreadableTitle(rows)}</h2>
            <p className="td-note">
              These files under ~/.claude/sessions on each host could not be read, so they are not on the board. A
              running claude process a file names is listed by its pid. Claude Code may have changed its state file
              format.
            </p>
          </div>
          <button type="button" className="td-btn td-unreadable-close" aria-label="Close" onClick={onClose}>
            ×
          </button>
        </div>
        <div className="td-hosts-table-wrap">
          <table className="td-hosts-table td-unreadable-table">
            <thead>
              <tr>
                <th scope="col">Host</th>
                <th scope="col">File</th>
                <th scope="col">Reason</th>
                <th scope="col">Process</th>
              </tr>
            </thead>
            <tbody>
              {/* Two files can share a bounded name, so the row's place is its key. */}
              {rows.map(({ host, file }, index) => {
                const process = unreadableProcess(file)
                return (
                  <tr key={index}>
                    <td>{hostLabel(host)}</td>
                    <td className="td-unreadable-file">{file.file}</td>
                    <td>
                      {unreadableReasonLabel(file.reason)}
                      {file.detail && <div className="td-faint">{file.detail}</div>}
                    </td>
                    <td className={file.pid === undefined ? 'td-faint' : undefined}>
                      {process.text}
                      <div className="td-faint">{process.note}</div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
        <div className="td-unreadable-foot">
          <p className="td-note">
            panemux reads{' '}
            {READ_STATE_FIELDS.map((field, i) => (
              <span key={field}>
                {i > 0 && ', '}
                <code>{field}</code>
              </span>
            ))}{' '}
            from each file. Compare a file with these fields, then follow{' '}
            <a href={UNREADABLE_DOC_URL} target="_blank" rel="noopener noreferrer">
              How to fix (docs/behavior/tasks.md)
            </a>
            .
          </p>
          {copyStatus && (
            <span role="status" className="td-faint">
              {copyStatus}
            </span>
          )}
          <button type="button" className="td-btn" disabled={!clipboard} onClick={() => void copy()}>
            Copy details
          </button>
        </div>
      </div>
    </div>
  )
}
