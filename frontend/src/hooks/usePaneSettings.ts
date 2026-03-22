import { useState, useEffect, useCallback } from 'react'
import { SSHConnectionsResponseSchema } from '../schemas'
import type { LayoutNode, PaneConfig } from '../schemas'
import { replacePaneInTree } from '../utils/layoutTree'

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
        // Restart is best-effort; failure is non-fatal
        fetch(`/api/sessions/${updated.id}/restart`, { method: 'POST' }).catch(() => {})
      } finally {
        setIsSaving(false)
      }
    },
    [layout, onLayoutChange],
  )

  return { isOpen, currentPane, sshConnectionNames, saveError, isSaving, openSettings, closeSettings, saveSettings }
}
