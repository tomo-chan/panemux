import { useCallback, useEffect, useRef, useState } from 'react'
import { DetectShellResponseSchema, DisplayConfig, DisplayConfigSchema, LayoutNode, PaneConfig, TabPosition, WorkspacesResponse, WorkspacesResponseSchema, WorkspaceTabPositionRequestSchema } from '../schemas'
import { PaneEdge, findPaneById, generatePaneId, generateTmuxSessionName, insertPaneAtWorkspaceEdge, insertPaneBesideTargetPane, movePaneBesideTargetPane, movePaneToWorkspaceEdge, removePaneFromTree, splitPaneInTree, swapPanesInTree } from '../utils/layoutTree'

export type PanePlacement =
  | { type: 'workspace-edge'; edge: PaneEdge }
  | { type: 'pane-edge'; targetPaneId: string; edge: PaneEdge }

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

  const saveLayout = useCallback(async (updatedLayout: LayoutNode) => {
    const workspaceID = workspaces?.active
    await fetch(workspaceID ? `/api/workspaces/${encodeURIComponent(workspaceID)}/layout` : '/api/layout', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(updatedLayout),
    }).catch(console.error)
  }, [workspaces?.active])

  const detectDefaultLocalShell = useCallback(async () => {
    try {
      const r = await fetch('/api/detect-shell')
      if (r.ok) {
        return DetectShellResponseSchema.parse(await r.json()).shell
      }
    } catch {
      // non-fatal: backend will use its own default
    }
    return undefined
  }, [])

  const ensurePaneDefaults = useCallback(async (pane: PaneConfig): Promise<PaneConfig> => {
    if (pane.type === 'local' && pane.shell === undefined) {
      const shell = await detectDefaultLocalShell()
      if (shell) return { ...pane, shell }
    }
    return pane
  }, [detectDefaultLocalShell])

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

  const renameWorkspace = useCallback(async (workspaceID: string, title: string) => {
    try {
      setError(null)
      const response = await fetch(`/api/workspaces/${encodeURIComponent(workspaceID)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title }),
      })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const parsed = WorkspacesResponseSchema.parse(await response.json())
      setWorkspaces(parsed)
      const active = parsed.items.find((workspace) => workspace.id === parsed.active) ?? parsed.items[0]
      setLayout(active.layout)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to rename workspace')
    }
  }, [])

  const setWorkspaceTabPosition = useCallback(async (position: TabPosition) => {
    try {
      setError(null)
      const request = WorkspaceTabPositionRequestSchema.parse({ tab_position: position })
      const response = await fetch('/api/workspaces/tab-position', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(request),
      })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const parsed = WorkspacesResponseSchema.parse(await response.json())
      setWorkspaces((current) => current ? { ...parsed, items: current.items } : parsed)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update workspace tab position')
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

      const newPaneWithDefaults = await ensurePaneDefaults(newPane)

      await fetch('/api/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newPaneWithDefaults),
      }).catch(console.error)

      const newLayout = splitPaneInTree(layout, targetPaneId, direction, newPaneWithDefaults)
      setLayout(newLayout)
      setWorkspaces((current) => current ? replaceActiveWorkspaceLayout(current, newLayout) : current)

      await saveLayout(newLayout)
    },
    [ensurePaneDefaults, layout, saveLayout],
  )

  const closePane = useCallback(
    async (targetPaneId: string) => {
      if (!layout) return

      await fetch(`/api/sessions/${targetPaneId}`, { method: 'DELETE' }).catch(console.error)

      const newLayout = removePaneFromTree(layout, targetPaneId)
      setLayout(newLayout)
      if (newLayout) setWorkspaces((current) => current ? replaceActiveWorkspaceLayout(current, newLayout) : current)

      if (newLayout) {
        await saveLayout(newLayout)
      }
    },
    [layout, saveLayout],
  )

  const swapPanes = useCallback(
    async (paneIdA: string, paneIdB: string) => {
      if (!layout) return
      const newLayout = swapPanesInTree(layout, paneIdA, paneIdB)
      setLayout(newLayout)
      setWorkspaces((current) => current ? replaceActiveWorkspaceLayout(current, newLayout) : current)
      await saveLayout(newLayout)
    },
    [layout, saveLayout],
  )

  const createPane = useCallback(async (pane: PaneConfig, placement: PanePlacement) => {
    if (!layout) return
    const paneWithDefaults = await ensurePaneDefaults(pane)
    setError(null)

    const response = await fetch('/api/sessions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(paneWithDefaults),
    })
    if (!response.ok) {
      throw new Error(`HTTP ${response.status}`)
    }

    const newLayout = placement.type === 'workspace-edge'
      ? insertPaneAtWorkspaceEdge(layout, placement.edge, paneWithDefaults)
      : insertPaneBesideTargetPane(layout, placement.targetPaneId, placement.edge, paneWithDefaults)
    setLayout(newLayout)
    setWorkspaces((current) => current ? replaceActiveWorkspaceLayout(current, newLayout) : current)
    await saveLayout(newLayout)
  }, [ensurePaneDefaults, layout, saveLayout])

  const movePane = useCallback(async (sourcePaneId: string, placement: PanePlacement) => {
    if (!layout) return
    setError(null)
    const newLayout = placement.type === 'workspace-edge'
      ? movePaneToWorkspaceEdge(layout, sourcePaneId, placement.edge)
      : movePaneBesideTargetPane(layout, sourcePaneId, placement.targetPaneId, placement.edge)
    setLayout(newLayout)
    setWorkspaces((current) => current ? replaceActiveWorkspaceLayout(current, newLayout) : current)
    await saveLayout(newLayout)
  }, [layout, saveLayout])

  return { layout, workspaces, displayConfig, error, updateSizes, splitPane, closePane, swapPanes, createPane, movePane, setActiveWorkspace, addWorkspace, deleteWorkspace, renameWorkspace, setWorkspaceTabPosition }
}

function replaceActiveWorkspaceLayout(workspaces: WorkspacesResponse, layout: LayoutNode): WorkspacesResponse {
  return {
    ...workspaces,
    items: workspaces.items.map((workspace) =>
      workspace.id === workspaces.active ? { ...workspace, layout } : workspace,
    ),
  }
}
