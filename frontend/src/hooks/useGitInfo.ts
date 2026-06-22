import { useState, useEffect, useCallback } from 'react'
import { GitInfo, GitInfoSchema } from '../schemas'

const GIT_INFO_STALE_MS = 30000
const GIT_INFO_RECHECK_THROTTLE_MS = 5000

interface GitInfoCacheEntry {
  gitInfo: GitInfo
  lastFetchedAt: number
  lastCheckedAt: number
  inFlight: Promise<void> | null
}

const DEFAULT_GIT_INFO: GitInfo = { is_git: false }
const gitInfoCache = new Map<string, GitInfoCacheEntry>()
const gitInfoListeners = new Map<string, Set<(gitInfo: GitInfo) => void>>()

export interface UseGitInfoResult {
  gitInfo: GitInfo
  refreshIfStale: () => Promise<void>
  refreshNow: () => Promise<void>
}

export function useGitInfo(sessionId: string, enabled = true): UseGitInfoResult {
  const [gitInfo, setGitInfo] = useState<GitInfo>(() => getOrCreateCacheEntry(sessionId).gitInfo)
  const [isVisible, setIsVisible] = useState(() => document.visibilityState === 'visible')

  const refreshNow = useCallback(async () => {
    const cacheEntry = getOrCreateCacheEntry(sessionId)
    if (cacheEntry.inFlight) {
      await cacheEntry.inFlight
      return
    }

    cacheEntry.lastCheckedAt = Date.now()
    const request = (async () => {
      try {
        const res = await fetch(`/api/sessions/${sessionId}/git-info`)
        if (!res.ok) return
        const data = GitInfoSchema.parse(await res.json())
        cacheEntry.gitInfo = data
        cacheEntry.lastFetchedAt = Date.now()
        setGitInfo(data)
        notifyGitInfoListeners(sessionId, data)
      } catch {
        // ignore errors silently — git info is best-effort
      } finally {
        cacheEntry.inFlight = null
      }
    })()

    cacheEntry.inFlight = request
    await request
  }, [sessionId])

  const refreshIfStale = useCallback(async () => {
    const cacheEntry = getOrCreateCacheEntry(sessionId)
    const now = Date.now()
    if (
      cacheEntry.lastFetchedAt > 0 &&
      now-cacheEntry.lastFetchedAt < GIT_INFO_STALE_MS
    ) {
      return
    }
    if (
      cacheEntry.lastCheckedAt > 0 &&
      now-cacheEntry.lastCheckedAt < GIT_INFO_RECHECK_THROTTLE_MS
    ) {
      return
    }

    await refreshNow()
  }, [refreshNow, sessionId])

  useEffect(() => {
    const handleVisibilityChange = () => {
      setIsVisible(document.visibilityState === 'visible')
    }

    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
  }, [])

  useEffect(() => {
    if (!enabled || !isVisible) return
    void refreshIfStale()
  }, [enabled, isVisible, refreshIfStale])

  useEffect(() => {
    setGitInfo(getOrCreateCacheEntry(sessionId).gitInfo)
  }, [sessionId])

  return { gitInfo, refreshIfStale, refreshNow }
}

export function useGitInfoSnapshotMap(sessionIds: string[]): Record<string, GitInfo> {
  const sessionIdsKey = sessionIds.join('\u0000')
  const [gitInfoById, setGitInfoById] = useState<Record<string, GitInfo>>(() => {
    const next: Record<string, GitInfo> = {}
    for (const sessionId of sessionIds) {
      next[sessionId] = getOrCreateCacheEntry(sessionId).gitInfo
    }
    return next
  })

  useEffect(() => {
    const next: Record<string, GitInfo> = {}
    for (const sessionId of sessionIds) {
      next[sessionId] = getOrCreateCacheEntry(sessionId).gitInfo
    }
    setGitInfoById((current) => {
      if (
        Object.keys(current).length === sessionIds.length &&
        sessionIds.every((sessionId) => current[sessionId] === next[sessionId])
      ) {
        return current
      }
      return next
    })

    const unsubscribers = sessionIds.map((sessionId) => subscribeToGitInfo(sessionId, (gitInfo) => {
      setGitInfoById((current) => {
        if (current[sessionId] === gitInfo) return current
        return { ...current, [sessionId]: gitInfo }
      })
    }))

    return () => {
      for (const unsubscribe of unsubscribers) unsubscribe()
    }
  }, [sessionIdsKey])

  return gitInfoById
}

function getOrCreateCacheEntry(sessionId: string): GitInfoCacheEntry {
  const existing = gitInfoCache.get(sessionId)
  if (existing) return existing

  const created: GitInfoCacheEntry = {
    gitInfo: DEFAULT_GIT_INFO,
    lastFetchedAt: 0,
    lastCheckedAt: 0,
    inFlight: null,
  }
  gitInfoCache.set(sessionId, created)
  return created
}

function subscribeToGitInfo(sessionId: string, listener: (gitInfo: GitInfo) => void): () => void {
  const listeners = gitInfoListeners.get(sessionId) ?? new Set<(gitInfo: GitInfo) => void>()
  listeners.add(listener)
  gitInfoListeners.set(sessionId, listeners)

  return () => {
    const currentListeners = gitInfoListeners.get(sessionId)
    if (!currentListeners) return
    currentListeners.delete(listener)
    if (currentListeners.size === 0) {
      gitInfoListeners.delete(sessionId)
    }
  }
}

function notifyGitInfoListeners(sessionId: string, gitInfo: GitInfo) {
  const listeners = gitInfoListeners.get(sessionId)
  if (!listeners) return
  for (const listener of listeners) {
    listener(gitInfo)
  }
}

export function __resetGitInfoCacheForTests() {
  gitInfoCache.clear()
  gitInfoListeners.clear()
}
