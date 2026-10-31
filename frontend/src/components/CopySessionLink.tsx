import { useState } from 'react'
import type { Task } from '../schemas'
import { sessionLinkUrl } from '../utils/sessionLink'

export function CopySessionLink({ task }: { task: Pick<Task, 'host' | 'agent' | 'session_id'> }) {
  const [feedback, setFeedback] = useState<{ url: string; copied: boolean } | null>(null)
  const url = sessionLinkUrl(window.location.href, task)
  const current = feedback?.url === url ? feedback : null
  const copy = async () => {
    if (!url) return
    try {
      if (!navigator.clipboard) throw new Error('Clipboard unavailable')
      await navigator.clipboard.writeText(url)
      setFeedback({ url, copied: true })
    } catch {
      setFeedback({ url, copied: false })
    }
  }
  return (
    <span onClick={(event) => event.stopPropagation()}>
      <button type="button" className="td-btn td-btn-sm" disabled={!url} onClick={() => void copy()}>
        Copy session link
      </button>
      {!url && <span className="td-note">Session ID is not known yet</span>}
      {current?.copied && <span role="status">Copied</span>}
      {current && !current.copied && (
        <label className="td-note">Clipboard unavailable. Copy manually:
          <input aria-label="Session link" readOnly value={current.url} onFocus={(event) => event.target.select()} />
        </label>
      )}
    </span>
  )
}
