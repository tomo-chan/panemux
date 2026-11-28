import { useRef, useState } from 'react'
import { BoardBroadcastResponseSchema, BoardBroadcastFailureSchema } from '../schemas'

// Owned by the palette, so closing a form cannot discard an unresolved send
// or its eventual delivery result. No network retry is performed.
export function useBoardMessage(token: string) {
  const [recipients, setRecipients] = useState('')
  const [body, setBody] = useState('')
  const [pending, setPending] = useState(false)
  const sending = useRef(false)
  const [result, setResult] = useState<{ error: boolean; text: string } | null>(null)

  const send = async () => {
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
      if (response.status === 401 || response.status === 403) {
        setResult({ error: true, text: `Not sent: not authorized (${response.status}).` })
        return
      }
      const data: unknown = await response.json()
      if (response.ok) {
        const { delivered } = BoardBroadcastResponseSchema.parse(data)
        setResult({ error: false, text: `Sent to: ${delivered.join(', ')}` })
      } else {
        const failure = BoardBroadcastFailureSchema.parse(data)
        if (response.status === 422) setResult({ error: true, text: `Not sent: ${failure.error}` })
        else if (response.status === 502 && failure.delivered) {
          const delivered = failure.delivered
          const unsent = to.filter(address => !delivered.includes(address))
          const outcome = delivered.length ? 'Partially sent' : 'Send failed'
          setResult({ error: true, text: `${outcome}. Delivered: ${delivered.join(', ') || 'none'}. Not sent: ${unsent.join(', ') || 'none'}. ${failure.error}` })
        } else setResult({ error: true, text: `Delivery unknown (${response.status}): ${failure.error}. Check history before retrying.` })
      }
    } catch {
      setResult({ error: true, text: 'Delivery unknown. Check history before retrying; some recipients may already have received the message.' })
    } finally { sending.current = false; setPending(false) }
  }

  return { recipients, setRecipients, body, setBody, pending, result, setResult, send }
}
