import { useCallback, useEffect, useRef, useState } from 'react'
import { isLoopbackUrl, isOpenableUrl } from '../utils/paneUrlOpen'
import { requestPreview } from '../utils/preview'

/** What the preview side of a split shows. */
export type PreviewView =
  | { kind: 'loading'; request: string }
  | { kind: 'choose'; request: string; candidates: string[]; host: string }
  | { kind: 'error'; request: string; message: string }
  /** generation changes on each reload, so the frame loads the file again. */
  | { kind: 'ready'; url: string; path: string; host: string; generation: number }
  /**
   * A localhost URL on the panemux host (issue #386), framed as it is served:
   * a development server, not a file panemux serves.
   */
  | { kind: 'page'; url: string; generation: number }

export interface UsePreviewResult {
  /** null while no preview is shown, and the terminal takes the whole body. */
  view: PreviewView | null
  /** Opens a clicked path, or one of the candidates offered for it. */
  open: (path: string) => void
  /** Shows a localhost URL on the panemux host. Any other URL is ignored. */
  openUrl: (url: string) => void
  reload: () => void
  close: () => void
}

/**
 * Owns the preview of one terminal (issues #385 and #386). The preview is hidden when
 * the terminal is not connected and stays hidden after it reconnects; a tab
 * opened on it is the browser's own and is not affected.
 */
export function usePreview(sessionId: string, connected: boolean): UsePreviewResult {
  const [view, setView] = useState<PreviewView | null>(null)
  // Each open or close bumps this, so an answer that arrives after a later
  // click, or after the preview was closed, is dropped.
  const requestRef = useRef(0)

  const open = useCallback((path: string) => {
    if (!connected) return
    const request = ++requestRef.current
    setView({ kind: 'loading', request: path })
    requestPreview(sessionId, path)
      .then((resp) => {
        if (request !== requestRef.current) return
        setView(resp.status === 'ready'
          ? { kind: 'ready', url: resp.url, path: resp.path, host: resp.host, generation: 0 }
          : { kind: 'choose', request: path, candidates: resp.candidates, host: resp.host })
      })
      .catch((err: unknown) => {
        if (request !== requestRef.current) return
        setView({ kind: 'error', request: path, message: err instanceof Error ? err.message : String(err) })
      })
  }, [connected, sessionId])

  const openUrl = useCallback((url: string) => {
    if (!connected || !isOpenableUrl(url) || !isLoopbackUrl(url)) return
    requestRef.current++
    setView({ kind: 'page', url, generation: 0 })
  }, [connected])

  const reload = useCallback(() => {
    setView((current) =>
      current?.kind === 'ready' || current?.kind === 'page' ? { ...current, generation: current.generation + 1 } : current,
    )
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
