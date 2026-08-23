import { useCallback, useState } from 'react'
import {
  isLoopbackUrl,
  isOpenableUrl,
  openBlankTab,
  openUrlTab,
  requestPortForward,
} from '../utils/paneUrlOpen'

export interface UsePaneUrlOpenResult {
  /** URL a pane asked to open that is waiting for the operator's approval. */
  pendingUrl: string | null
  /** Last port-forwarding failure, shown until dismissed. */
  error: string | null
  /** Opens a URL the operator activated themselves (a link click). */
  openUrl: (url: string) => void
  /** Records a URL a program inside the pane asked panemux to open. */
  requestOpen: (url: string) => void
  confirmPendingOpen: () => void
  dismissPendingOpen: () => void
  dismissError: () => void
}

/**
 * Owns everything that happens when a URL is opened out of one pane: opening
 * the tab in the browser showing the dashboard, and asking the backend to
 * publish the URL's loopback callback port on the panemux host so the OAuth
 * redirect lands where the CLI is waiting.
 */
export function usePaneUrlOpen(sessionId: string): UsePaneUrlOpenResult {
  const [pendingUrl, setPendingUrl] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const openUrl = useCallback((url: string) => {
    if (!isOpenableUrl(url)) return
    setError(null)

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
  }, [sessionId])

  const requestOpen = useCallback((url: string) => {
    if (!isOpenableUrl(url)) return
    setPendingUrl(url)
  }, [])

  // The open happens here, not inside a setPendingUrl updater: React treats
  // updaters as pure and re-runs them (StrictMode does so on every update),
  // which would open the tab and post the forward request twice per approval.
  const confirmPendingOpen = useCallback(() => {
    if (pendingUrl) openUrl(pendingUrl)
    setPendingUrl(null)
  }, [openUrl, pendingUrl])

  const dismissPendingOpen = useCallback(() => setPendingUrl(null), [])
  const dismissError = useCallback(() => setError(null), [])

  return { pendingUrl, error, openUrl, requestOpen, confirmPendingOpen, dismissPendingOpen, dismissError }
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}
