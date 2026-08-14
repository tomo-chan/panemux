import { useEffect, useState } from 'react'
import { BoardSessionTokenResponseSchema } from '../schemas'

export interface BoardSession {
  token: string
  commandCenterEnabled: boolean
}

const DEFAULT_BOARD_SESSION: BoardSession = { token: '', commandCenterEnabled: false }

// useBoardSessionToken fetches the bearer token panemux generated or was
// configured with, so the dashboard can authenticate its own
// /api/board/* requests and the /ws/board-command connection. See
// GetBoardSessionToken's own doc comment (internal/api/board.go) for why
// this endpoint exists and is deliberately unauthenticated itself.
export function useBoardSessionToken(): BoardSession {
  const [session, setSession] = useState<BoardSession>(DEFAULT_BOARD_SESSION)

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const res = await fetch('/api/session-token')
        if (!res.ok) return
        const data = BoardSessionTokenResponseSchema.parse(await res.json())
        if (!cancelled) {
          setSession({ token: data.token, commandCenterEnabled: data.command_center_enabled })
        }
      } catch {
        // Best-effort: the command center simply stays unavailable.
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  return session
}
