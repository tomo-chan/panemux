import { useCallback, useEffect, useRef, useState } from 'react'
import { DetectShellResponseSchema, DisplayConfig, DisplayConfigSchema, LayoutNode, PaneConfig, WorkspacesResponse, WorkspacesResponseSchema } from '../schemas'
import { findPaneById, generatePaneId, generateTmuxSessionName, removePaneFromTree, splitPaneInTree, swapPanesInTree } from '../utils/layoutTree'

export function useLayout() {
  const [layout, setLayout] = useState<LayoutNode | null>(null)
  const [workspaces, setWorkspaces] = useState<WorkspacesResponse | null>(null)
  const [displayConfig, setDisplayConfig] = useState<DisplayConfig | null>(null)
  const [error, setError] = useState<string | null>(null)
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    fetch('/api/workspaces')
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json()
      })
      .then((data) => {
        const parsed = WorkspacesResponseSchema.parse(data)
        setWorkspaces(parsed)
        const active = parsed.items.find((workspace) => workspace.id === parsed.active) ?? parsed.items[0]
        setLayout(active.layout)
      })
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
    setWorkspaces((current) => current ? replaceActiveWorkspaceLayout(current, updatedLayout) : current)

    // Debounce save to server
    if (saveTimerRef.current) clearTimeout(saveTimerRef.current)
    saveTimerRef.current = setTimeout(() => {
      const workspaceID = workspaces?.active
      fetch(workspaceID ? `/api/workspaces/${encodeURIComponent(workspaceID)}/layout` : '/api/layout', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(updatedLayout),
      }).catch(console.error)
    }, 500)
  }, [workspaces?.active])

  const setActiveWorkspace = useCallback(async (workspaceID: string) => {
    if (!workspaces || workspaceID === workspaces.active) return
    const target = workspaces.items.find((workspace) => workspace.id === workspaceID)
    if (!target) return

    setWorkspaces({ ...workspaces, active: workspaceID })
    setLayout(target.layout)
    await fetch('/api/workspaces/active', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: workspaceID }),
    }).catch(console.error)
  }, [workspaces])

  const addWorkspace = useCallback(async () => {
    try {
      setError(null)
      const response = await fetch('/api/workspaces', { method: 'POST' })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const parsed = WorkspacesResponseSchema.parse(await response.json())
      setWorkspaces(parsed)
      const active = parsed.items.find((workspace) => workspace.id === parsed.active) ?? parsed.items[0]
      setLayout(active.layout)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add workspace')
    }
  }, [])

  const deleteWorkspace = useCallback(async (workspaceID: string) => {
    try {
      setError(null)
      const response = await fetch(`/api/workspaces/${encodeURIComponent(workspaceID)}`, { method: 'DELETE' })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const parsed = WorkspacesResponseSchema.parse(await response.json())
      setWorkspaces(parsed)
      const active = parsed.items.find((workspace) => workspace.id === parsed.active) ?? parsed.items[0]
      setLayout(active.layout)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete workspace')
    }
  }, [])

  const splitPane = useCallback(
    async (targetPaneId: string, direction: 'horizontal' | 'vertical') => {
      if (!layout) return
      const sourcePane = findPaneById(layout, targetPaneId)
      const newPane: PaneConfig = {
        ...(sourcePane ? {
          type: sourcePane.type,
          ...(sourcePane.shell !== undefined && { shell: sourcePane.shell }),
          ...(sourcePane.cwd !== undefined && { cwd: sourcePane.cwd }),
          ...(sourcePane.connection !== undefined && { connection: sourcePane.connection }),
          ...((sourcePane.type === 'tmux' || sourcePane.type === 'ssh_tmux') && { tmux_session: generateTmuxSessionName(sourcePane.tmux_session ?? 'session') }),
          ...(sourcePane.show_header !== undefined && { show_header: sourcePane.show_header }),
          ...(sourcePane.show_status_bar !== undefined && { show_status_bar: sourcePane.show_status_bar }),
        } : { type: 'local' }),
        id: generatePaneId(),
      }

      if (newPane.type === 'local' && newPane.shell === undefined) {
        try {
          const r = await fetch('/api/detect-shell')
          if (r.ok) {
            newPane.shell = DetectShellResponseSchema.parse(await r.json()).shell
          }
        } catch {
          // non-fatal: backend will use its own default
        }
      }

      await fetch('/api/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newPane),
      }).catch(console.error)

      const newLayout = splitPaneInTree(layout, targetPaneId, direction, newPane)
      setLayout(newLayout)
      setWorkspaces((current) => current ? replaceActiveWorkspaceLayout(current, newLayout) : current)

      await fetch(workspaces?.active ? `/api/workspaces/${encodeURIComponent(workspaces.active)}/layout` : '/api/layout', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newLayout),
      }).catch(console.error)
    },
    [layout, workspaces?.active],
  )

  const closePane = useCallback(
    async (targetPaneId: string) => {
      if (!layout) return

      await fetch(`/api/sessions/${targetPaneId}`, { method: 'DELETE' }).catch(console.error)

      const newLayout = removePaneFromTree(layout, targetPaneId)
      setLayout(newLayout)
      if (newLayout) setWorkspaces((current) => current ? replaceActiveWorkspaceLayout(current, newLayout) : current)

      if (newLayout) {
        await fetch(workspaces?.active ? `/api/workspaces/${encodeURIComponent(workspaces.active)}/layout` : '/api/layout', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(newLayout),
        }).catch(console.error)
      }
    },
    [layout, workspaces?.active],
  )

  const swapPanes = useCallback(
    async (paneIdA: string, paneIdB: string) => {
      if (!layout) return
      const newLayout = swapPanesInTree(layout, paneIdA, paneIdB)
      setLayout(newLayout)
      setWorkspaces((current) => current ? replaceActiveWorkspaceLayout(current, newLayout) : current)
      await fetch(workspaces?.active ? `/api/workspaces/${encodeURIComponent(workspaces.active)}/layout` : '/api/layout', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newLayout),
      }).catch(console.error)
    },
    [layout, workspaces?.active],
  )

  return { layout, workspaces, displayConfig, error, updateSizes, splitPane, closePane, swapPanes, setActiveWorkspace, addWorkspace, deleteWorkspace }
}

function replaceActiveWorkspaceLayout(workspaces: WorkspacesResponse, layout: LayoutNode): WorkspacesResponse {
  return {
    ...workspaces,
    items: workspaces.items.map((workspace) =>
      workspace.id === workspaces.active ? { ...workspace, layout } : workspace,
    ),
  }
}
