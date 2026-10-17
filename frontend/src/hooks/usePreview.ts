import { useCallback, useEffect, useRef, useState } from 'react'
import { isLoopbackUrl, isOpenableUrl } from '../utils/paneUrlOpen'
import { localhostFrameUrl, requestPreview, type PreviewTarget } from '../utils/preview'

/** What the preview side of a split shows. */
export type PreviewView =
  | { kind: 'loading'; request: string }
  | { kind: 'choose'; request: string; candidates: string[]; host: string }
  | { kind: 'error'; request: string; message: string }
  /** generation changes on each reload, so the frame loads the file again. */
  | { kind: 'ready'; url: string; path: string; host: string; generation: number }
  /**
   * A localhost page, framed as it is served: a development server, not a
   * file panemux serves. url is what a new tab opens and frameUrl what the
   * frame shows. On the panemux host (issue #386) they are the URL asked
   * for, but for [::1], which is framed as localhost. On an SSH host (issue
   * #387) both are the URL's relay on the panemux host; host names the SSH
   * host and shown the URL as it was clicked.
   */
  | { kind: 'page'; url: string; frameUrl: string; generation: number; host?: string; shown?: string }

export interface UsePreviewResult {
  /** null while no preview is shown, and the terminal takes the whole body. */
  view: PreviewView | null
  /** Opens a clicked path, or one of the candidates offered for it. */
  open: (path: string) => void
  /**
   * Shows a localhost URL: on the panemux host as it is, or, for a terminal
   * on an SSH host (remote), through its relay. Any other URL is ignored.
   */
  openUrl: (url: string) => void
  reload: () => void
  close: () => void
}

/**
 * Owns the preview of one terminal (issues #385 and #386). The preview is hidden when
 * the terminal is not connected and stays hidden after it reconnects; a tab
 * opened on it is the browser's own and is not affected.
 */
export function usePreview(sessionId: string, connected: boolean, remote = false): UsePreviewResult {
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
        if (resp.status === 'choose') {
          setView({ kind: 'choose', request, candidates: resp.candidates, host: resp.host })
        } else if (resp.kind === 'url') {
          setView({ kind: 'page', url: resp.url, frameUrl: resp.url, generation: 0, host: resp.host, shown: resp.path })
        } else {
          setView({ kind: 'ready', url: resp.url, path: resp.path, host: resp.host, generation: 0 })
        }
      })
      .catch((err: unknown) => {
        if (id !== requestRef.current) return
        setView({ kind: 'error', request, message: err instanceof Error ? err.message : String(err) })
      })
  }, [connected, sessionId])

  const open = useCallback((path: string) => start(path, { path }), [start])

  const openUrl = useCallback((url: string) => {
    if (!connected || !isOpenableUrl(url) || !isLoopbackUrl(url)) return
    if (remote) {
      start(url, { url })
      return
    }
    requestRef.current++
    setView({ kind: 'page', url, frameUrl: localhostFrameUrl(url), generation: 0 })
  }, [connected, remote, start])

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
