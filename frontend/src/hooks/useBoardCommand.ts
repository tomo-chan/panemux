import { useCallback, useEffect, useRef, useState } from 'react'
import type { Dispatch, SetStateAction } from 'react'
import { BoardCommandFrameSchema } from '../schemas'
import type { BoardCommandFrame } from '../schemas'

export interface BoardCommandTurn {
  id: number
  prompt: string
  lines: unknown[]
  error: string | null
  busy: boolean
  done: boolean
}

interface UseBoardCommandOptions {
  enabled: boolean
  token: string
}

export interface UseBoardCommandResult {
  connected: boolean
  turns: BoardCommandTurn[]
  pending: boolean
  sendPrompt: (prompt: string) => void
}

let nextTurnId = 1

// useBoardCommand drives the command center chat used by the Spotlight
// palette: one WS /ws/board-command connection per mount, open only while
// enabled (the palette is expected to pass its own open/closed state), the
// bearer token as a WebSocket subprotocol per BoardCommandHandler's own
// contract (internal/ws/board_command.go) since browsers cannot set an
// Authorization header on a WebSocket upgrade. See docs/agent-board.md's
// "API and streaming" section for the line/error/done/busy frame protocol.
export function useBoardCommand({ enabled, token }: UseBoardCommandOptions): UseBoardCommandResult {
  const wsRef = useRef<WebSocket | null>(null)
  const [connected, setConnected] = useState(false)
  const [turns, setTurns] = useState<BoardCommandTurn[]>([])
  const [pending, setPending] = useState(false)

  useEffect(() => {
    if (!enabled || !token) return

    const ws = new WebSocket(buildBoardCommandWSURL(), [token])
    wsRef.current = ws

    ws.onopen = () => setConnected(true)
    ws.onclose = () => setConnected(false)
    ws.onerror = () => ws.close()
    ws.onmessage = (event) => {
      if (typeof event.data !== 'string') return
      const frame = parseFrame(event.data)
      if (!frame) return
      applyFrame(setTurns, frame)
      if (frame.type !== 'line') setPending(false)
    }

    return () => {
      ws.onclose = null
      ws.onerror = null
      ws.close()
      wsRef.current = null
      setConnected(false)
    }
  }, [enabled, token])

  const sendPrompt = useCallback((prompt: string) => {
    const ws = wsRef.current
    if (!ws || ws.readyState !== WebSocket.OPEN) return
    setTurns((prev) => [...prev, { id: nextTurnId++, prompt, lines: [], error: null, busy: false, done: false }])
    setPending(true)
    ws.send(JSON.stringify({ prompt }))
  }, [])

  return { connected, turns, pending, sendPrompt }
}

function parseFrame(data: string): BoardCommandFrame | null {
  try {
    const parsed = BoardCommandFrameSchema.safeParse(JSON.parse(data))
    return parsed.success ? parsed.data : null
  } catch {
    return null
  }
}

function applyFrame(setTurns: Dispatch<SetStateAction<BoardCommandTurn[]>>, frame: BoardCommandFrame) {
  setTurns((prev) => {
    if (prev.length === 0) return prev
    const last = prev[prev.length - 1]
    const updated: BoardCommandTurn = { ...last }
    switch (frame.type) {
      case 'line':
        updated.lines = [...updated.lines, frame.raw]
        break
      case 'error':
        updated.error = frame.message
        updated.done = true
        break
      case 'done':
        updated.done = true
        break
      case 'busy':
        updated.busy = true
        updated.done = true
        break
    }
    return [...prev.slice(0, -1), updated]
  })
}

function buildBoardCommandWSURL(): string {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${location.host}/ws/board-command`
}
