import { useRef, useState } from 'react'
import { z } from 'zod'
import { useBoardStatus } from '../hooks/useBoardStatus'
import type { BoardPaneRef } from './BoardDashboardPanel'
import { boardText } from '../utils/boardText'

const DeliverySchema = z.object({ delivered: z.array(z.string()) })
const FailureSchema = z.object({ error: z.string(), delivered: z.array(z.string()).optional() })

// This form calls the same authenticated relay API as MCP. The observed list
// is a convenience, never a recipient allowlist or delivery guarantee.
export function BoardMessageForm({ token, boardPanes }: { token: string; boardPanes: readonly BoardPaneRef[] }) {
  const { identities, error } = useBoardStatus({ enabled: true, token })
  const [recipients, setRecipients] = useState('')
  const [body, setBody] = useState('')
  const [pending, setPending] = useState(false)
  const sending = useRef(false)
  const [result, setResult] = useState<{ error: boolean; text: string } | null>(null)

  const send = async (event: React.FormEvent) => {
    event.preventDefault()
    if (sending.current) return
    const to = [...new Set(recipients.split(/[,\n]/).map(value => value.trim()).filter(Boolean))]
    sending.current = true
    setPending(true)
    setResult(null)
    try {
      const response = await fetch('/api/board/broadcast', {
        method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ to, body }),
      })
      const data: unknown = await response.json()
      if (response.ok) {
        const { delivered } = DeliverySchema.parse(data)
        setResult({ error: false, text: `Sent to: ${delivered.join(', ')}` })
      } else {
        const failure = FailureSchema.parse(data)
        if (response.status === 422) setResult({ error: true, text: `Not sent: ${failure.error}` })
        else if (response.status === 502 && failure.delivered) {
          const unsent = to.filter(address => !failure.delivered!.includes(address))
          setResult({ error: true, text: `Partially sent. Delivered: ${failure.delivered.join(', ') || 'none'}. Not sent: ${unsent.join(', ') || 'none'}. ${failure.error}` })
        } else if (response.status === 401 || response.status === 403) {
          setResult({ error: true, text: `Not sent: not authorized (${response.status}).` })
        } else setResult({ error: true, text: `Delivery unknown (${response.status}): ${failure.error}. Check history before retrying.` })
      }
    } catch {
      setResult({ error: true, text: 'Delivery unknown. Check history before retrying; some recipients may already have received the message.' })
    } finally { sending.current = false; setPending(false) }
  }

  return <form onSubmit={event => void send(event)} style={{ display: 'flex', flexDirection: 'column', gap: 8, minHeight: 0, overflowY: 'auto' }}>
    <p style={{ margin: 0, color: '#aaa', fontSize: 12 }}>Choose observed names or enter any name@local, name@ssh:host, or existing pane ID. Observation does not guarantee delivery.</p>
    {error && <div role="alert">{error} You can still enter a recipient.</div>}
    <label htmlFor="board-add-recipient">Add recipient</label>
    <select id="board-add-recipient" style={fieldStyle} value="" disabled={pending} onChange={event => {
      const address = event.target.value
      setRecipients(previous => previous ? `${previous}, ${address}` : address)
    }}>
      <option value="">Choose a name or pane…</option>
      {Object.keys(identities).sort().map(address => <option key={address} value={address}>{address} · observed</option>)}
      {boardPanes.map(pane => <option key={pane.id} value={pane.id}>{pane.id} · pane{pane.title ? ` · ${pane.title}` : ''}</option>)}
    </select>
    <label htmlFor="board-recipients">Recipients</label>
    <input id="board-recipients" style={fieldStyle} value={recipients} disabled={pending} onChange={event => setRecipients(event.target.value)} placeholder="worker@local, reviewer@ssh:build" />
    <label htmlFor="board-message-body">Message body</label>
    <textarea id="board-message-body" style={{ ...fieldStyle, minHeight: 110, resize: 'vertical' }} value={body} disabled={pending} onChange={event => setBody(event.target.value)} />
    <button type="submit" style={{ ...fieldStyle, background: '#0e639c', cursor: 'pointer' }} disabled={pending}>{pending ? 'Sending…' : 'Send message'}</button>
    {result && <div role={result.error ? 'alert' : 'status'} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', color: result.error ? '#f48771' : '#b5cea8' }}>{boardText(result.text)}</div>}
  </form>
}

const fieldStyle: React.CSSProperties = { background: '#3c3c3c', color: '#d4d4d4', border: '1px solid #555', borderRadius: 3, padding: '6px 10px', font: 'inherit', minWidth: 0, boxSizing: 'border-box' }
