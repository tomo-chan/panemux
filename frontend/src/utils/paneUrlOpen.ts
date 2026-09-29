import { OpenUrlResponseSchema, type OpenUrlResponse } from '../schemas'

// BROWSER_OPEN_OSC_IDENT is the private OSC identifier the browser shim
// installed in a pane writes when a program on that pane's host tries to open
// a browser. Keep it in sync with BrowserOpenOSCIdent in
// internal/session/browseropen.go.
export const BROWSER_OPEN_OSC_IDENT = 7373

const BROWSER_OPEN_OSC_PREFIX = 'panemux-open;'

/**
 * Returns the URL carried by a browser-open OSC payload, or null when the
 * payload is not one, or names a scheme panemux must never open. Terminal
 * output is untrusted: anything that can write to a pane's terminal can emit
 * this sequence, so the scheme check here is a guard, not a formality.
 */
export function parseBrowserOpenOsc(data: string): string | null {
  if (!data.startsWith(BROWSER_OPEN_OSC_PREFIX)) return null
  const url = data.slice(BROWSER_OPEN_OSC_PREFIX.length)
  return isOpenableUrl(url) ? url : null
}

/** Only http and https URLs may ever be opened on a pane's behalf. */
export function isOpenableUrl(raw: string): boolean {
  const parsed = parseUrl(raw)
  return parsed !== null && (parsed.protocol === 'http:' || parsed.protocol === 'https:')
}

/**
 * Reports whether the URL itself points at loopback, meaning the browser will
 * hit the forwarded port as soon as the tab opens rather than after a login
 * round trip.
 */
export function isLoopbackUrl(raw: string): boolean {
  const parsed = parseUrl(raw)
  if (!parsed) return false
  const host = parsed.hostname.toLowerCase().replace(/^\[/, '').replace(/\]$/, '')
  return host === 'localhost' || host === '::1' || /^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(host)
}

function parseUrl(raw: string): URL | null {
  try {
    return new URL(raw)
  } catch {
    return null
  }
}

/**
 * Asks the backend to publish the URL's loopback callback port on the panemux
 * host, so an OAuth redirect to http://localhost:<port>/… reaches the CLI
 * waiting for it inside the pane.
 */
export async function requestPortForward(sessionId: string, url: string): Promise<OpenUrlResponse> {
  const res = await fetch(`/api/sessions/${encodeURIComponent(sessionId)}/open-url`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url }),
  })
  if (!res.ok) {
    const message = (await res.text()).trim()
    throw new Error(message || `port forwarding failed (${res.status})`)
  }
  return OpenUrlResponseSchema.parse(await res.json())
}

/** Opens a tab the caller will navigate later, once a forward is ready. */
export function openBlankTab(): Window | null {
  const win = window.open('about:blank', '_blank')
  if (win) {
    try {
      win.opener = null
    } catch {
      // Some browsers make opener read-only; the tab is still ours to drive.
    }
  }
  return win
}

/** Opens a URL in a new tab, with no handle back to the dashboard. */
export function openUrlTab(url: string): void {
  window.open(url, '_blank', 'noopener,noreferrer')
}
