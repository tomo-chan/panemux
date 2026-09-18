import { useEffect, useMemo, useRef } from 'react'
import { createAgentAttentionDetector } from '../utils/agentAttention'
import { collectLeafPanes } from '../utils/layoutTree'
import { getLastNotifiedAttentionSignature, setLastNotifiedAttentionSignature } from '../utils/attentionNotificationState'
import type { WorkspacesResponse } from '../schemas'

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
  // App.tsx rebuilds its attention callback whenever the workspace list or the
  // layout changes — which is every workspace switch — so holding it in a ref
  // is what keeps that from tearing down and reopening every monitor socket
  // (issue #78). The sockets read the ref when a message arrives, so they
  // always report to the current callback.
  const onAttentionRef = useRef(onAttention)

  useEffect(() => {
    onAttentionRef.current = onAttention
  }, [onAttention])

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
      for (const pane of collectLeafPanes(workspace.layout)) {
        metadata.set(pane.id, { workspaceId: workspace.id })
      }
    }

    return metadata
  }, [workspaces?.items])

  // Pane membership is read when a message arrives rather than captured by the
  // socket effect, so a pane moving between workspaces updates the
  // notification decision without reopening anything.
  const paneMetadataRef = useRef(paneMetadataById)
  paneMetadataRef.current = paneMetadataById

  // The socket effect's dependency is the pane ID set itself, not the object
  // that carries it: a refetched workspace list is all new objects even when
  // the same panes are still being watched.
  const paneIdsKey = useMemo(() => [...paneMetadataById.keys()].sort().join('\n'), [paneMetadataById])

  useEffect(() => {
    const paneIds = paneIdsKey === '' ? [] : paneIdsKey.split('\n')
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
          paneWorkspaceId: paneMetadataRef.current.get(paneId)?.workspaceId ?? null,
          activeWorkspaceId: activeWorkspaceIdRef.current,
          maximizedPaneId: maximizedPaneIdRef.current,
          browserIsActive: isBrowserActive(),
          signature: attentionMatch.signature,
        })

        if (shouldNotifyBrowser) {
          setLastNotifiedAttentionSignature(paneId, attentionMatch.signature)
        }
        onAttentionRef.current(paneId, shouldNotifyBrowser)
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
  }, [paneIdsKey])
}

function buildWebSocketURL(paneId: string): string {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${location.host}/ws/${paneId}`
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
