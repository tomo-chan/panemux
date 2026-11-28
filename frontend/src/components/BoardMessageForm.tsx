import { useBoardMessage } from '../hooks/useBoardMessage'
import { useBoardStatus } from '../hooks/useBoardStatus'
import type { BoardPaneRef } from './BoardDashboardPanel'
import { boardText } from '../utils/boardText'

// This form calls the same authenticated relay API as MCP. The observed list
// is a convenience, never a recipient allowlist or delivery guarantee.
export function BoardMessageForm({ token, boardPanes, active = true, message }: { token: string; boardPanes: readonly BoardPaneRef[]; active?: boolean; message?: ReturnType<typeof useBoardMessage> }) {
  const { identities, error } = useBoardStatus({ enabled: active, token })
  const localMessage = useBoardMessage(token)
  const { recipients, setRecipients, body, setBody, pending, result, setResult, send } = message ?? localMessage

  return <form onSubmit={event => { event.preventDefault(); if (active) void send() }} style={{ display: 'flex', flexDirection: 'column', gap: 8, minHeight: 0, overflowY: 'auto' }}>
    <p style={{ margin: 0, color: '#aaa', fontSize: 12 }}>Choose observed names or enter any name@local, name@ssh:host, or existing pane ID. Observation does not guarantee delivery.</p>
    {error && <div role="alert">{error} You can still enter a recipient.</div>}
    <label htmlFor="board-add-recipient">Add recipient</label>
    <select id="board-add-recipient" style={fieldStyle} value="" disabled={pending || !active} onChange={event => {
      const address = event.target.value
      setResult(null)
      setRecipients(previous => previous ? `${previous}, ${address}` : address)
    }}>
      <option value="">Choose a name or pane…</option>
      {Object.keys(identities).sort().map(address => <option key={address} value={address}>{address} · observed</option>)}
      {boardPanes.map(pane => <option key={pane.id} value={pane.id}>{pane.id} · pane{pane.title ? ` · ${pane.title}` : ''}</option>)}
    </select>
    <label htmlFor="board-recipients">Recipients</label>
    <input id="board-recipients" style={fieldStyle} value={recipients} disabled={pending || !active} onChange={event => { setRecipients(event.target.value); setResult(null) }} placeholder="worker@local, reviewer@ssh:build" />
    <label htmlFor="board-message-body">Message body</label>
    <textarea id="board-message-body" style={{ ...fieldStyle, minHeight: 110, resize: 'vertical' }} value={body} disabled={pending || !active} onChange={event => { setBody(event.target.value); setResult(null) }} />
    <button type="submit" style={{ ...fieldStyle, background: '#0e639c', cursor: 'pointer' }} disabled={pending || !active}>{pending ? 'Sending…' : 'Send message'}</button>
    {result && <div role={result.error ? 'alert' : 'status'} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', color: result.error ? '#f48771' : '#b5cea8' }}>{boardText(result.text)}</div>}
  </form>
}

const fieldStyle: React.CSSProperties = { background: '#3c3c3c', color: '#d4d4d4', border: '1px solid #555', borderRadius: 3, padding: '6px 10px', font: 'inherit', minWidth: 0, boxSizing: 'border-box' }
