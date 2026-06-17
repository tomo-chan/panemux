import { useState, useEffect, useCallback } from 'react'
import { GitInfo, GitInfoSchema } from '../schemas'

const GIT_INFO_POLL_INTERVAL_MS = 10000

export function useGitInfo(sessionId: string, enabled = true): GitInfo {
  const [gitInfo, setGitInfo] = useState<GitInfo>({ is_git: false })
  const [isVisible, setIsVisible] = useState(() => document.visibilityState === 'visible')

  const fetchGitInfo = useCallback(async () => {
    try {
      const res = await fetch(`/api/sessions/${sessionId}/git-info`)
      if (!res.ok) return
      const data = GitInfoSchema.parse(await res.json())
      setGitInfo(data)
    } catch {
      // ignore errors silently — git info is best-effort
    }
  }, [sessionId])

  useEffect(() => {
    const handleVisibilityChange = () => {
      setIsVisible(document.visibilityState === 'visible')
    }

    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
  }, [])

  useEffect(() => {
    if (!enabled || !isVisible) return

    fetchGitInfo()
    const interval = setInterval(fetchGitInfo, GIT_INFO_POLL_INTERVAL_MS)
    return () => clearInterval(interval)
  }, [enabled, fetchGitInfo, isVisible])

  return gitInfo
}
