import { useCallback, useEffect, useRef, useState } from 'react'
import { requestPreview, type PreviewTarget } from '../utils/preview'

/** What the preview side of a split shows. */
export type PreviewView =
  | { kind: 'loading'; request: string }
  | { kind: 'choose'; request: string; candidates: string[]; host: string }
  | { kind: 'error'; request: string; message: string }
  /**
   * generation changes on each reload, so the frame loads the file again.
   * source is "file" for a file served by panemux, "url" for a localhost URL
   * of an SSH host reached through its relay; path is the file's path, or
   * the URL as clicked.
   */
  | { kind: 'ready'; source: 'file' | 'url'; url: string; path: string; host: string; generation: number }

export interface UsePreviewResult {
  /** null while no preview is shown, and the terminal takes the whole body. */
  view: PreviewView | null
  /** Opens a clicked path, or one of the candidates offered for it. */
  open: (path: string) => void
  /** Opens a clicked localhost URL of a pane on an SSH host (issue #387). */
  openUrl: (url: string) => void
  reload: () => void
  close: () => void
}

/**
 * Owns the preview of one terminal (issue #385). The preview is hidden when
 * the terminal is not connected and stays hidden after it reconnects; a tab
 * opened on it is the browser's own and is not affected.
 */
export function usePreview(sessionId: string, connected: boolean): UsePreviewResult {
  const [view, setView] = useState<PreviewView | null>(null)
  // Each open or close bumps this, so an answer that arrives after a later
  // click, or after the preview was closed, is dropped.
  const requestRef = useRef(0)

  const start = useCallback((request: string, target: PreviewTarget) => {
    if (!connected) return
    const id = ++requestRef.current
    setView({ kind: 'loading', request })
    requestPreview(sessionId, target)
      .then((resp) => {
        if (id !== requestRef.current) return
        setView(resp.status === 'ready'
          ? { kind: 'ready', source: resp.kind, url: resp.url, path: resp.path, host: resp.host, generation: 0 }
          : { kind: 'choose', request, candidates: resp.candidates, host: resp.host })
      })
      .catch((err: unknown) => {
        if (id !== requestRef.current) return
        setView({ kind: 'error', request, message: err instanceof Error ? err.message : String(err) })
      })
  }, [connected, sessionId])

  const open = useCallback((path: string) => start(path, { path }), [start])
  const openUrl = useCallback((url: string) => start(url, { url }), [start])

  const reload = useCallback(() => {
    setView((current) => (current?.kind === 'ready' ? { ...current, generation: current.generation + 1 } : current))
  }, [])

  const close = useCallback(() => {
    requestRef.current++
    setView(null)
  }, [])

  useEffect(() => {
    if (!connected) close()
  }, [connected, close])

  return { view, open, openUrl, reload, close }
}
