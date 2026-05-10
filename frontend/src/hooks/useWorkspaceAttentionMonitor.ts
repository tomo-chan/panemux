import { useEffect, useMemo, useRef } from 'react'
import { createAgentAttentionDetector } from '../utils/agentAttention'
import { getLastNotifiedAttentionSignature, setLastNotifiedAttentionSignature } from '../utils/attentionNotificationState'
import type { LayoutNode, WorkspacesResponse } from '../schemas'

interface UseWorkspaceAttentionMonitorOptions {
  workspaces: WorkspacesResponse | null
  maximizedPaneId: string | null
  onAttention: (paneId: string, showBrowserNotification?: boolean) => void
}

interface PaneMonitorState {
  detector: ReturnType<typeof createAgentAttentionDetector>
  decoder: TextDecoder
}

export function useWorkspaceAttentionMonitor({ workspaces, maximizedPaneId, onAttention }: UseWorkspaceAttentionMonitorOptions) {
  const monitorStatesRef = useRef<Map<string, PaneMonitorState>>(new Map())
  const activeWorkspaceIdRef = useRef<string | null>(workspaces?.active ?? null)
  const maximizedPaneIdRef = useRef<string | null>(maximizedPaneId)

  useEffect(() => {
    activeWorkspaceIdRef.current = workspaces?.active ?? null
  }, [workspaces?.active])

  useEffect(() => {
    maximizedPaneIdRef.current = maximizedPaneId
  }, [maximizedPaneId])

  const paneMetadataById = useMemo(() => {
    const metadata = new Map<string, { workspaceId: string }>()
    if (!workspaces) return metadata

    for (const workspace of workspaces.items) {
      for (const paneId of collectPaneIDs(workspace.layout)) {
        metadata.set(paneId, { workspaceId: workspace.id })
      }
    }

    return metadata
  }, [workspaces?.items])

  useEffect(() => {
    const paneIds = [...paneMetadataById.keys()]
    if (paneIds.length === 0) return

    const sockets = paneIds.map((paneId) => {
      const state = getOrCreatePaneMonitorState(monitorStatesRef.current, paneId)
      const ws = new WebSocket(buildWebSocketURL(paneId))
      ws.binaryType = 'arraybuffer'

      ws.onopen = () => {
        state.detector.reset()
        state.decoder = new TextDecoder()
      }

      ws.onmessage = (event) => {
        if (typeof event.data === 'string') return
        const text = state.decoder.decode(new Uint8Array(event.data as ArrayBuffer), { stream: true })
        const attentionMatch = state.detector.feed(text)
        if (!attentionMatch) return

        const shouldNotifyBrowser = shouldNotifyBrowserAttention({
          paneId,
          paneWorkspaceId: paneMetadataById.get(paneId)?.workspaceId ?? null,
          activeWorkspaceId: activeWorkspaceIdRef.current,
          maximizedPaneId: maximizedPaneIdRef.current,
          browserIsActive: isBrowserActive(),
          signature: attentionMatch.signature,
        })

        if (shouldNotifyBrowser) {
          setLastNotifiedAttentionSignature(paneId, attentionMatch.signature)
        }
        onAttention(paneId, shouldNotifyBrowser)
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
  }, [onAttention, paneMetadataById])
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
  }
  states.set(paneId, created)
  return created
}

function shouldNotifyBrowserAttention({
  paneId,
  paneWorkspaceId,
  activeWorkspaceId,
  maximizedPaneId,
  browserIsActive,
  signature,
}: {
  paneId: string
  paneWorkspaceId: string | null
  activeWorkspaceId: string | null
  maximizedPaneId: string | null
  browserIsActive: boolean
  signature: string
}): boolean {
  if (getLastNotifiedAttentionSignature(paneId) === signature) return false
  if (!browserIsActive) return true
  if (!paneWorkspaceId || paneWorkspaceId !== activeWorkspaceId) return true
  if (!maximizedPaneId) return false

  return paneId !== maximizedPaneId
}

function isBrowserActive(): boolean {
  return document.visibilityState === 'visible' && document.hasFocus()
}
