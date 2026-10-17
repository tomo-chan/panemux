import { useCallback, useEffect, useRef, useState } from 'react'
import {
  isLoopbackUrl,
  isOpenableUrl,
  openBlankTab,
  openUrlTab,
  requestPortForward,
} from '../utils/paneUrlOpen'

/**
 * Who asked to open a URL that waits for the operator: a program in the pane
 * (the browser shim), or an OSC 8 hyperlink the operator clicked, whose
 * target the terminal output chose and the visible text need not show.
 */
export type UrlOpenSource = 'program' | 'link'

export interface UsePaneUrlOpenOptions {
  /**
   * Shows a localhost URL beside the terminal (issue #386). Given only for a
   * terminal on the panemux host, where that URL names a server on this host,
   * and only while that terminal is connected, as the preview opens only then.
   */
  previewUrl?: (url: string) => void
  /**
   * Whether opening a URL asks the backend to forward its loopback callback
   * port (the default). Only panes have forwards.
   */
  forward?: boolean
}

export interface UsePaneUrlOpenResult {
  /** URL waiting for the operator's approval. */
  pendingUrl: string | null
  pendingSource: UrlOpenSource | null
  /** Whether the pending URL can be shown in the preview. */
  canPreview: boolean
  /** Last port-forwarding failure, shown until dismissed. */
  error: string | null
  /**
   * Opens a URL the operator activated themselves (a link whose text is the
   * URL): a localhost URL in the preview when there is one, anything else in
   * a tab.
   */
  openUrl: (url: string) => void
  /** Records a URL to open once the operator approves it. */
  requestOpen: (url: string, source?: UrlOpenSource) => void
  /** Preview: shows the pending URL beside the terminal. */
  previewPendingOpen: () => void
  /** Open: opens the pending URL in a tab, preparing its forward. */
  confirmPendingOpen: () => void
  /** Ignore. */
  dismissPendingOpen: () => void
  dismissError: () => void
}

interface PendingOpen {
  url: string
  source: UrlOpenSource
}

/**
 * The one place a URL out of a terminal is opened — a clicked link, an OSC 8
 * hyperlink and the browser shim alike: in a tab of the browser showing the
 * dashboard, with the URL's loopback callback port published on the panemux
 * host so an OAuth redirect lands where the CLI is waiting, or, for a
 * localhost URL on the panemux host, in the split preview.
 *
 * sessionId names the terminal served: the pane whose forwards are asked for,
 * or, with forwarding off, any key that changes when the terminal is replaced.
 */
export function usePaneUrlOpen(sessionId: string, options: UsePaneUrlOpenOptions = {}): UsePaneUrlOpenResult {
  const { previewUrl, forward = true } = options
  const [pending, setPending] = useState<PendingOpen | null>(null)
  const [error, setError] = useState<string | null>(null)
  // The request each button acts on. It is taken synchronously, so a second
  // press before the strip hides — or Preview then Open — finds nothing left
  // to open, and one request opens at most once.
  const pendingRef = useRef<PendingOpen | null>(null)

  const setPendingOpen = useCallback((next: PendingOpen | null) => {
    pendingRef.current = next
    setPending(next)
  }, [])

  const takePending = useCallback((): PendingOpen | null => {
    const taken = pendingRef.current
    setPendingOpen(null)
    return taken
  }, [setPendingOpen])

  // A request belongs to the terminal that raised it. When the session is
  // replaced — the popup's Retry or Reconnect makes a new attach — approving
  // it would answer a terminal that is gone.
  useEffect(() => {
    setPendingOpen(null)
  }, [sessionId, setPendingOpen])

  const canPreviewUrl = useCallback(
    (url: string) => previewUrl !== undefined && isLoopbackUrl(url),
    [previewUrl],
  )

  const openInTab = useCallback((url: string) => {
    if (!isOpenableUrl(url)) return
    setError(null)

    if (!forward) {
      openUrlTab(url)
      return
    }

    if (!isLoopbackUrl(url)) {
      // The callback only fires after the operator logs in, so the tab can
      // open right away — synchronously, inside the activating gesture,
      // which is what keeps the popup blocker out of the way.
      openUrlTab(url)
      void requestPortForward(sessionId, url).catch((err: unknown) => setError(errorMessage(err)))
      return
    }

    // A loopback URL is hit the moment the tab opens, so it has to wait for
    // the forward. The tab is still opened synchronously, then navigated.
    const tab = openBlankTab()
    void requestPortForward(sessionId, url)
      .then(() => {
        if (tab) tab.location.href = url
      })
      .catch((err: unknown) => {
        setError(errorMessage(err))
        tab?.close()
      })
  }, [forward, sessionId])

  const openUrl = useCallback((url: string) => {
    if (!isOpenableUrl(url)) return
    // The operator opened it themselves: a request still asking for the same
    // URL would only open it a second time.
    if (pendingRef.current?.url === url) setPendingOpen(null)
    if (canPreviewUrl(url)) {
      previewUrl?.(url)
      return
    }
    openInTab(url)
  }, [canPreviewUrl, openInTab, previewUrl, setPendingOpen])

  const requestOpen = useCallback((url: string, source: UrlOpenSource = 'program') => {
    if (!isOpenableUrl(url)) return
    setPendingOpen({ url, source })
  }, [setPendingOpen])

  // Each action opens outside any state updater: React treats updaters as
  // pure and re-runs them (StrictMode does so on every update), which would
  // open the tab and post the forward request twice per approval.
  const previewPendingOpen = useCallback(() => {
    const current = pendingRef.current
    if (!current || !canPreviewUrl(current.url)) return
    takePending()
    previewUrl?.(current.url)
  }, [canPreviewUrl, previewUrl, takePending])

  const confirmPendingOpen = useCallback(() => {
    const taken = takePending()
    if (taken) openInTab(taken.url)
  }, [openInTab, takePending])

  const dismissPendingOpen = useCallback(() => setPendingOpen(null), [setPendingOpen])
  const dismissError = useCallback(() => setError(null), [])

  return {
    pendingUrl: pending?.url ?? null,
    pendingSource: pending?.source ?? null,
    canPreview: pending !== null && canPreviewUrl(pending.url),
    error,
    openUrl,
    requestOpen,
    previewPendingOpen,
    confirmPendingOpen,
    dismissPendingOpen,
    dismissError,
  }
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}
