import { useCallback, useEffect, useState } from 'react'
import { SessionInfo, SessionInfoListSchema } from '../schemas'

const SESSIONS_POLL_INTERVAL_MS = 10000

export function useSessionsOverview(enabled = true): Record<string, SessionInfo> {
  const [sessionsById, setSessionsById] = useState<Record<string, SessionInfo>>({})
  const [isVisible, setIsVisible] = useState(() => document.visibilityState === 'visible')

  const fetchSessions = useCallback(async () => {
    try {
      const res = await fetch('/api/sessions')
      if (!res.ok) return
      const sessions = SessionInfoListSchema.parse(await res.json())
      setSessionsById(Object.fromEntries(sessions.map((session) => [session.id, session])))
    } catch {
      // Session polling is best-effort for the dashboard.
    }
  }, [])

  useEffect(() => {
    const handleVisibilityChange = () => {
      setIsVisible(document.visibilityState === 'visible')
    }

    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
  }, [])

  useEffect(() => {
    if (!enabled || !isVisible) return

    void fetchSessions()
    const interval = setInterval(() => {
      void fetchSessions()
    }, SESSIONS_POLL_INTERVAL_MS)
    return () => clearInterval(interval)
  }, [enabled, fetchSessions, isVisible])

  return sessionsById
}
