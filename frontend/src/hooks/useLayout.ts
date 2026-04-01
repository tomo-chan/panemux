import { useCallback, useEffect, useRef, useState } from 'react'
import { DetectShellResponseSchema, DisplayConfig, DisplayConfigSchema, LayoutNode, LayoutNodeSchema, PaneConfig } from '../schemas'
import { generatePaneId, removePaneFromTree, splitPaneInTree, swapPanesInTree } from '../utils/layoutTree'

export function useLayout() {
  const [layout, setLayout] = useState<LayoutNode | null>(null)
  const [displayConfig, setDisplayConfig] = useState<DisplayConfig | null>(null)
  const [error, setError] = useState<string | null>(null)
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    fetch('/api/layout')
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json()
      })
      .then((data) => setLayout(LayoutNodeSchema.parse(data)))
      .catch((e) => setError(e.message))
  }, [])

  useEffect(() => {
    fetch('/api/display')
      .then((r) => {
        if (!r.ok) return undefined
        return r.json()
      })
      .then((data) => {
        if (data) setDisplayConfig(DisplayConfigSchema.parse(data))
      })
      .catch(() => { /* non-fatal */ })
  }, [])

  const updateSizes = useCallback((updatedLayout: LayoutNode) => {
    setLayout(updatedLayout)

    // Debounce save to server
    if (saveTimerRef.current) clearTimeout(saveTimerRef.current)
    saveTimerRef.current = setTimeout(() => {
      fetch('/api/layout', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(updatedLayout),
      }).catch(console.error)
    }, 500)
  }, [])

  const splitPane = useCallback(
    async (targetPaneId: string, direction: 'horizontal' | 'vertical') => {
      if (!layout) return

      // Detect login shell for the new pane; silently ignore errors
      let shell: string | undefined
      try {
        const r = await fetch('/api/detect-shell')
        if (r.ok) {
          shell = DetectShellResponseSchema.parse(await r.json()).shell
        }
      } catch {
        // non-fatal: backend will use its own default
      }

      const newPane: PaneConfig = { id: generatePaneId(), type: 'local', ...(shell ? { shell } : {}) }

      await fetch('/api/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newPane),
      }).catch(console.error)

      const newLayout = splitPaneInTree(layout, targetPaneId, direction, newPane)
      setLayout(newLayout)

      await fetch('/api/layout', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newLayout),
      }).catch(console.error)
    },
    [layout],
  )

  const closePane = useCallback(
    async (targetPaneId: string) => {
      if (!layout) return

      await fetch(`/api/sessions/${targetPaneId}`, { method: 'DELETE' }).catch(console.error)

      const newLayout = removePaneFromTree(layout, targetPaneId)
      setLayout(newLayout)

      if (newLayout) {
        await fetch('/api/layout', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(newLayout),
        }).catch(console.error)
      }
    },
    [layout],
  )

  const swapPanes = useCallback(
    async (paneIdA: string, paneIdB: string) => {
      if (!layout) return
      const newLayout = swapPanesInTree(layout, paneIdA, paneIdB)
      setLayout(newLayout)
      await fetch('/api/layout', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newLayout),
      }).catch(console.error)
    },
    [layout],
  )

  return { layout, displayConfig, error, updateSizes, splitPane, closePane, swapPanes }
}
