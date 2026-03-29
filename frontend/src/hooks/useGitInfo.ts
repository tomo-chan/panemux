import { useState, useEffect, useCallback } from 'react'
import { GitInfo, GitInfoSchema } from '../schemas'

export function useGitInfo(sessionId: string): GitInfo {
  const [gitInfo, setGitInfo] = useState<GitInfo>({ is_git: false })

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
    fetchGitInfo()
    const interval = setInterval(fetchGitInfo, 5000)
    return () => clearInterval(interval)
  }, [fetchGitInfo])

  return gitInfo
}
