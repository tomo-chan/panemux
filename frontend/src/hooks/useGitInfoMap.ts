import { useCallback, useEffect, useState } from 'react'
import { GitInfo, GitInfoSchema } from '../schemas'

const GIT_INFO_POLL_INTERVAL_MS = 10000

export function useGitInfoMap(sessionIds: string[], enabled = true): Record<string, GitInfo> {
  const [gitInfoById, setGitInfoById] = useState<Record<string, GitInfo>>({})
  const [isVisible, setIsVisible] = useState(() => document.visibilityState === 'visible')

  const fetchGitInfo = useCallback(async () => {
    if (sessionIds.length === 0) {
      setGitInfoById({})
      return
    }

    try {
      const results = await Promise.all(
        sessionIds.map(async (sessionId) => {
          const res = await fetch(`/api/sessions/${sessionId}/git-info`)
          if (!res.ok) return [sessionId, { is_git: false }] as const
          const data = GitInfoSchema.parse(await res.json())
          return [sessionId, data] as const
        }),
      )

      setGitInfoById(Object.fromEntries(results))
    } catch {
      // Git metadata is best-effort for the dashboard.
    }
  }, [sessionIds])

  useEffect(() => {
    const handleVisibilityChange = () => {
      setIsVisible(document.visibilityState === 'visible')
    }

    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
  }, [])

  useEffect(() => {
    if (!enabled || !isVisible) return

    void fetchGitInfo()
    const interval = setInterval(() => {
      void fetchGitInfo()
    }, GIT_INFO_POLL_INTERVAL_MS)
    return () => clearInterval(interval)
  }, [enabled, fetchGitInfo, isVisible])

  useEffect(() => {
    setGitInfoById((current) => {
      const next = Object.fromEntries(sessionIds.flatMap((sessionId) => current[sessionId] ? [[sessionId, current[sessionId]]] : []))
      const currentKeys = Object.keys(current)
      if (currentKeys.length === Object.keys(next).length && currentKeys.every((key) => current[key] === next[key])) {
        return current
      }
      return next
    })
  }, [sessionIds])

  return gitInfoById
}
