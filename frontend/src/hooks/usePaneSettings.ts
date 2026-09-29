import { useState, useEffect, useCallback } from 'react'
import { SSHConnectionsResponseSchema, DetectShellResponseSchema, DirectoryBrowserResponseSchema } from '../schemas'
import type { LayoutNode, PaneConfig, SSHConfigHost } from '../schemas'
import { replacePaneInTree } from '../utils/layoutTree'

// sessionFields are the PaneConfig fields the running session is built from.
// A change to any of them means the session has to be recreated to take
// effect; a change to anything else does not.
const sessionFields = ['type', 'shell', 'connection', 'tmux_session', 'cwd'] as const

function needsRestart(before: PaneConfig | null, after: PaneConfig): boolean {
  if (!before) return true
  return sessionFields.some((field) => before[field] !== after[field])
}

export function usePaneSettings(
  layout: LayoutNode | null,
  onLayoutChange: (layout: LayoutNode) => void,
) {
  const [isOpen, setIsOpen] = useState(false)
  const [currentPane, setCurrentPane] = useState<PaneConfig | null>(null)
  const [sshConnectionNames, setSshConnectionNames] = useState<string[]>([])
  const [saveError, setSaveError] = useState<string | null>(null)
  const [isSaving, setIsSaving] = useState(false)

  useEffect(() => {
    fetch('/api/ssh-connections')
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`HTTP ${r.status}`))))
      .then((data) => setSshConnectionNames(SSHConnectionsResponseSchema.parse(data).names))
      .catch(() => {
        // Non-fatal; dropdown will be empty
      })
  }, [])

  const openSettings = useCallback((pane: PaneConfig) => {
    setSaveError(null)
    setCurrentPane(pane)
    setIsOpen(true)
  }, [])

  const closeSettings = useCallback(() => {
    setIsOpen(false)
    setCurrentPane(null)
    setSaveError(null)
  }, [])

  const saveSettings = useCallback(
    async (updated: PaneConfig) => {
      if (!layout) return
      const newLayout = replacePaneInTree(layout, updated)
      setIsSaving(true)
      try {
        const r = await fetch('/api/layout', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(newLayout),
        })
        if (!r.ok) {
          const body = await r.json().catch(() => ({}))
          setSaveError((body as { error?: string }).error ?? `HTTP ${r.status}`)
          return
        }
        onLayoutChange(newLayout)
        setIsOpen(false)
        setCurrentPane(null)
        setSaveError(null)
        // Restarting kills whatever is running in the pane, so it happens
        // only when the session itself would come back different. Renaming a
        // pane, toggling its chrome, or putting it on the agent board are
        // config-only changes; restarting for those threw away the user's
        // shell, and — because bootstrap eligibility is keyed on the session
        // object — made every settings save re-write agmsg's onboarding
        // instruction into the pane.
        if (needsRestart(currentPane, updated)) {
          // Best-effort; failure is non-fatal.
          fetch(`/api/sessions/${updated.id}/restart`, { method: 'POST' }).catch(() => {})
        }
      } finally {
        setIsSaving(false)
      }
    },
    [layout, onLayoutChange],
  )

  const addSSHConfigHost = useCallback(async (host: SSHConfigHost): Promise<string> => {
    const r = await fetch('/api/ssh-config/hosts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(host),
    })
    if (!r.ok) {
      const body = await r.json().catch(() => ({}))
      throw new Error((body as { error?: string }).error ?? `HTTP ${r.status}`)
    }
    // Refresh the ssh connection names list
    const data = await fetch('/api/ssh-connections').then((res) => res.json())
    setSshConnectionNames(SSHConnectionsResponseSchema.parse(data).names)
    return host.name
  }, [])

  const detectShell = useCallback(
    async (type: PaneConfig['type'], connection?: string): Promise<string> => {
      const url = type === 'local' || !connection
        ? '/api/detect-shell'
        : `/api/detect-shell?connection=${encodeURIComponent(connection)}`
      const r = await fetch(url)
      if (!r.ok) {
        const body = await r.json().catch(() => ({}))
        throw new Error((body as { error?: string }).error ?? `HTTP ${r.status}`)
      }
      const data = await r.json()
      return DetectShellResponseSchema.parse(data).shell
    },
    [],
  )

  const browseDirectories = useCallback(
    async (
      type: PaneConfig['type'],
      connection?: string,
      path = '',
      showHidden = false,
    ) => {
      const params = new URLSearchParams()
      if (path) params.set('path', path)
      params.set('show_hidden', String(showHidden))
      if ((type === 'ssh' || type === 'ssh_tmux') && connection) {
        params.set('connection', connection)
      }
      const r = await fetch(`/api/directories?${params.toString()}`)
      if (!r.ok) {
        const body = await r.json().catch(() => ({}))
        throw new Error((body as { error?: string }).error ?? `HTTP ${r.status}`)
      }
      return DirectoryBrowserResponseSchema.parse(await r.json())
    },
    [],
  )

  return { isOpen, currentPane, sshConnectionNames, saveError, isSaving, openSettings, closeSettings, saveSettings, addSSHConfigHost, detectShell, browseDirectories }
}
