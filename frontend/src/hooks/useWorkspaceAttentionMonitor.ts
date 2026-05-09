import { useEffect, useMemo, useRef } from 'react'
import { createAgentAttentionDetector } from '../utils/agentAttention'
import type { LayoutNode, WorkspacesResponse } from '../schemas'

const ATTENTION_NOTIFY_INTERVAL_MS = 10_000

interface UseWorkspaceAttentionMonitorOptions {
  workspaces: WorkspacesResponse | null
  onAttention: (paneId: string) => void
}

interface PaneMonitorState {
  detector: ReturnType<typeof createAgentAttentionDetector>
  decoder: TextDecoder
  lastAttentionAt: number | null
}

export function useWorkspaceAttentionMonitor({ workspaces, onAttention }: UseWorkspaceAttentionMonitorOptions) {
  const monitorStatesRef = useRef<Map<string, PaneMonitorState>>(new Map())

  const paneIds = useMemo(() => {
    if (!workspaces) return []

    return workspaces.items.flatMap((workspace) => collectPaneIDs(workspace.layout))
  }, [workspaces])

  useEffect(() => {
    if (paneIds.length === 0) return

    const sockets = paneIds.map((paneId) => {
      const state = getOrCreatePaneMonitorState(monitorStatesRef.current, paneId)
      const ws = new WebSocket(buildWebSocketURL(paneId))
      ws.binaryType = 'arraybuffer'

      ws.onopen = () => {
        state.lastAttentionAt = null
        state.detector.reset()
        state.decoder = new TextDecoder()
      }

      ws.onmessage = (event) => {
        if (typeof event.data === 'string') return
        const text = state.decoder.decode(new Uint8Array(event.data as ArrayBuffer), { stream: true })
        if (shouldNotifyAttention(state, text)) {
          onAttention(paneId)
        }
      }

      ws.onerror = () => {
        ws.close()
      }

      return ws
    })

    return () => {
      for (const socket of sockets) {
        socket.onclose = null
        socket.onerror = null
        socket.close()
      }
    }
  }, [onAttention, paneIds])
}

function buildWebSocketURL(paneId: string): string {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${location.host}/ws/${paneId}`
}

function collectPaneIDs(layout: LayoutNode): string[] {
  return layout.children.flatMap(collectChildPaneIDs)
}

function collectChildPaneIDs(child: LayoutNode['children'][number]): string[] {
  if (child.pane && (!child.children || child.children.length === 0)) {
    return [child.pane.id]
  }

  if (child.children?.length) {
    return child.children.flatMap(collectChildPaneIDs)
  }

  return []
}

function getOrCreatePaneMonitorState(states: Map<string, PaneMonitorState>, paneId: string): PaneMonitorState {
  const existing = states.get(paneId)
  if (existing) return existing

  const created: PaneMonitorState = {
    detector: createAgentAttentionDetector(),
    decoder: new TextDecoder(),
    lastAttentionAt: null,
  }
  states.set(paneId, created)
  return created
}

function shouldNotifyAttention(state: PaneMonitorState, text: string): boolean {
  if (!state.detector.feed(text)) return false

  const now = Date.now()
  if (state.lastAttentionAt !== null && now - state.lastAttentionAt < ATTENTION_NOTIFY_INTERVAL_MS) {
    return false
  }

  state.lastAttentionAt = now
  return true
}
