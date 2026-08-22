import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { useWebSocket } from './useWebSocket'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'
import type { SessionState } from '../schemas'

const REPAINT_SETTLE_DELAYS_MS = [50, 250]

// Characters that must never be part of an auto-detected URL, on top of the ASCII
// symbols the web links addon already excludes. The addon default only knows about
// ASCII punctuation, so Japanese output such as `https://example.com/docs。` or
// `（https://example.com/docs）` swallowed the trailing CJK punctuation into the link.
//
// Ranges (deliberately punctuation only, never letters or digits):
//   U+2018-U+201F, U+2026   curly quotes and the horizontal ellipsis
//   U+3000-U+3004, U+3008-U+3020, U+3030, U+303D-U+303F
//                           CJK symbols and punctuation (、。「」【】〜 ...). The gaps skip the
//                           letters and digits interleaved into this block: U+3005-U+3007
//                           (々〆〇), U+3021-U+3029 and U+3038-U+303C (Hangzhou/ideographic
//                           numerals and iteration marks), and the U+302A-U+302F combining
//                           marks.
//   U+30FB                  katakana middle dot (・)
//   U+FF01-U+FF0F, U+FF1A-U+FF20, U+FF3B-U+FF40, U+FF5B-U+FF65
//                           fullwidth ASCII punctuation and halfwidth kana punctuation
// Fullwidth digits (U+FF10-U+FF19) and fullwidth letters are intentionally left out so
// they stay linkable, and non-ASCII letters are not excluded at all: raw IRIs such as
// `https://ja.wikipedia.org/wiki/日本語` must keep working. Kana directly following a URL
// with no delimiter is therefore still absorbed — that case is indistinguishable from a
// legitimate kana IRI path by regex alone.
const NON_URL_CJK_PUNCTUATION =
  '\\u2018-\\u201f\\u2026\\u3000-\\u3004\\u3008-\\u3020\\u3030\\u303d-\\u303f' +
  '\\u30fb\\uff01-\\uff0f\\uff1a-\\uff20\\uff3b-\\uff40\\uff5b-\\uff65'

// The addon default regex with the ranges above added to both character classes.
// Kept non-global on purpose: the addon derives its own global copy via
// `new RegExp(regex.source, regex.flags + 'g')`, which throws on a duplicate `g`.
export const TERMINAL_URL_REGEX = new RegExp(
  `(https?|HTTPS?):[/]{2}[^\\s"'!*(){}|\\\\\\^<>\`${NON_URL_CJK_PUNCTUATION}]*` +
    `[^\\s"':,.!?{}|\\\\\\^~\\[\\]\`()<>${NON_URL_CJK_PUNCTUATION}]`,
  'u',
)

interface UseTerminalOptions {
  sessionId: string
  container: HTMLElement | null
  repoURL?: string
  onInteraction?: () => void | Promise<void>
  // Reconnect tuning, forwarded to useWebSocket. Only overridden in tests;
  // production callers rely on useWebSocket's defaults.
  reconnectDelay?: number
  maxReconnectDelay?: number
  maxReconnectAttempts?: number
}

interface TerminalEntry {
  term: Terminal
  fitAddon: FitAddon
  attachedContainer: HTMLElement | null
  disposeTimer: ReturnType<typeof setTimeout> | null
  repaintTimers: Set<ReturnType<typeof setTimeout>>
  resizeTimers: Set<ReturnType<typeof setTimeout>>
  send: ((data: string | ArrayBuffer | Uint8Array) => void) | null
  replayActive: boolean
  replayWriteDepth: number
  awaitingReplayEnd: boolean
  repoURL: string | null
}

interface PendingTerminalMessage {
  data: ArrayBuffer | string
  isBinary: boolean
}

const terminalEntries = new Map<string, TerminalEntry>()
type TerminalLifecycleState = 'running' | 'disconnected' | 'exited'

export function useTerminal({
  sessionId,
  container,
  repoURL,
  onInteraction,
  reconnectDelay,
  maxReconnectDelay,
  maxReconnectAttempts,
}: UseTerminalOptions) {
  const termRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const initializedRef = useRef(false)
  const sendRef = useRef<((data: string | ArrayBuffer | Uint8Array) => void) | null>(null)
  const entryRef = useRef<TerminalEntry | null>(null)
  const pendingMessagesRef = useRef<PendingTerminalMessage[]>([])
  const reconnectingAfterDisconnectRef = useRef(false)
  const recoverDisconnectedSessionRef = useRef<(() => Promise<void>) | null>(null)
  const [dims, setDims] = useState<{ cols: number; rows: number } | null>(null)
  const [sessionState, setSessionState] = useState<TerminalLifecycleState>('running')
  const [reconnectFailed, setReconnectFailed] = useState(false)

  const applyMessageToTerminal = useCallback((data: ArrayBuffer | string, isBinary: boolean) => {
    const term = termRef.current
    const entry = entryRef.current
    if (!term || !entry) return

    if (isBinary) {
      const bytes = new Uint8Array(data as ArrayBuffer)
      writeTerminalBytes(entry, bytes)
    } else {
      try {
        const msg = JSON.parse(data as string)
        if (msg.type === 'status') {
          const state = msg.state as SessionState
          console.log(`Session ${sessionId} status:`, state)
          if (state === 'exited') {
            setSessionState('exited')
            setReconnectFailed(false)
            term.write('\r\n\x1b[2m[Session ended]\x1b[0m\r\n')
          } else if (state === 'disconnected') {
            setSessionState('disconnected')
            void recoverDisconnectedSessionRef.current?.()
          } else {
            setSessionState('running')
            setReconnectFailed(false)
          }
        } else if (msg.type === 'replay') {
          if (msg.state === 'start') {
            entry.replayActive = true
            entry.awaitingReplayEnd = true
            setTerminalInputSuppressed(entry, true)
          } else {
            entry.replayActive = false
            entry.awaitingReplayEnd = false
            maybeRestoreTerminalInput(entry)
          }
        } else if (msg.type === 'error') {
          const safeMsg = msg.message.replace(/\x1b\[[0-9;?<>!]*[A-Za-z]/g, '')
          term.write(`\r\n\x1b[31mError: ${safeMsg}\x1b[0m\r\n`)
        }
      } catch {
        // Not JSON, treat as text
        term.write(data as string)
      }
    }
  }, [sessionId])

  const handleMessage = useCallback((data: ArrayBuffer | string, isBinary: boolean) => {
    if (!canApplyMessage(entryRef.current, termRef.current)) {
      // A pane can reconnect before React has reattached the reused xterm DOM node
      // during a workspace switch. Keep those bytes so the initial prompt is not
      // lost between the WebSocket open and the eventual attach.
      pendingMessagesRef.current.push({ data, isBinary })
      return
    }

    applyMessageToTerminal(data, isBinary)
  }, [applyMessageToTerminal])

  const wsUrl = `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/${sessionId}`
  const { send, connected, exhausted, reconnect } = useWebSocket(wsUrl, {
    onMessage: handleMessage,
    onOpen: () => {
      setSessionState('running')
      setReconnectFailed(false)
      reconnectingAfterDisconnectRef.current = false
      const entry = entryRef.current
      if (entry) resetReplayState(entry)
    },
    reconnectDelay,
    maxReconnectDelay,
    maxReconnectAttempts,
  })

  const recoverDisconnectedSession = useCallback(async () => {
    if (reconnectingAfterDisconnectRef.current) return
    reconnectingAfterDisconnectRef.current = true
    setReconnectFailed(false)

    try {
      const res = await fetch(`/api/sessions/${sessionId}/restart`, { method: 'POST' })
      if (!res.ok) {
        setReconnectFailed(true)
        return
      }
      reconnect()
    } catch {
      setReconnectFailed(true)
    } finally {
      reconnectingAfterDisconnectRef.current = false
    }
  }, [reconnect, sessionId])

  // This ref is read from an async status-frame handler that can run before an
  // effect flushes, so keep it current during render rather than one commit later.
  recoverDisconnectedSessionRef.current = recoverDisconnectedSession

  // A backend-reported "disconnected" status frame isn't the only way a pane
  // can go bad: the WebSocket itself can repeatedly fail to (re)establish and
  // exhaust its own retry budget without ever delivering a status frame (e.g.
  // during a prolonged network outage). Route that case through the same
  // recovery path so the pane doesn't get stuck showing "reconnecting..."
  // forever with no way to reach the manual restart button.
  useEffect(() => {
    if (!exhausted) return
    setSessionState('disconnected')
    void recoverDisconnectedSessionRef.current?.()
  }, [exhausted])

  // Keep sendRef in sync so onData closure always has the latest send
  useLayoutEffect(() => {
    sendRef.current = send
    if (entryRef.current) {
      entryRef.current.send = send
    }
  }, [send])

  useEffect(() => {
    // Browser focus and tab visibility changes can affect every attached pane's layout,
    // so each mounted terminal repaints its own attached instance when the page returns.
    const repaintCurrentTerminal = () => {
      const entry = entryRef.current
      if (entry) refreshTerminal(entry, setDims)
    }

    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible') {
        repaintCurrentTerminal()
      }
    }

    document.addEventListener('visibilitychange', handleVisibilityChange)
    window.addEventListener('focus', repaintCurrentTerminal)

    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      window.removeEventListener('focus', repaintCurrentTerminal)
    }
  }, [])

  useEffect(() => {
    const entry = entryRef.current
    if (!connected || !entry) return

    // On reconnect the container may still be measuring as 0x0. Delay the
    // initial resize until xterm reports non-zero cols/rows, otherwise the
    // backend drops the resize and some shells never redraw their prompt.
    scheduleConnectedResize(entry, setDims, send)
  }, [connected, send, sessionId])

  useEffect(() => {
    const entry = entryRef.current
    if (!entry) return
    entry.repoURL = repoURL ?? null
  }, [repoURL, sessionId])

  // Initialize terminal
  useEffect(() => {
    if (!container || initializedRef.current) return
    initializedRef.current = true

    const entry = getOrCreateTerminalEntry(sessionId)
    if (entry.disposeTimer) {
      clearTimeout(entry.disposeTimer)
      entry.disposeTimer = null
    }

    entry.attachedContainer = container
    entry.send = sendRef.current
    entryRef.current = entry
    termRef.current = entry.term
    fitAddonRef.current = entry.fitAddon

    attachTerminal(entry, container)
    // Flush anything buffered while the pane was logically mounted but the xterm
    // element had not yet been attached back into this container.
    flushPendingMessages(pendingMessagesRef.current, applyMessageToTerminal)
    refreshTerminal(entry, setDims)

    return () => {
      const currentEntry = entryRef.current
      if (!currentEntry) return

      clearScheduledRepaints(currentEntry)
      clearScheduledResizes(currentEntry)
      currentEntry.attachedContainer = null
      currentEntry.send = null
      currentEntry.disposeTimer = setTimeout(() => {
        if (currentEntry.attachedContainer) return
        clearScheduledRepaints(currentEntry)
        clearScheduledResizes(currentEntry)
        currentEntry.term.dispose()
        terminalEntries.delete(sessionId)
      }, 0)

      termRef.current = null
      fitAddonRef.current = null
      entryRef.current = null
      initializedRef.current = false
    }
  }, [container, sessionId])

  useEffect(() => {
    if (!container || !onInteraction) return

    const handleInteraction = () => {
      void onInteraction()
    }

    container.addEventListener('pointerdown', handleInteraction, true)
    container.addEventListener('keydown', handleInteraction, true)
    container.addEventListener('wheel', handleInteraction, { capture: true, passive: true })

    return () => {
      container.removeEventListener('pointerdown', handleInteraction, true)
      container.removeEventListener('keydown', handleInteraction, true)
      container.removeEventListener('wheel', handleInteraction, true)
    }
  }, [container, onInteraction])

  const restartSession = useCallback(async () => {
    try {
      const res = await fetch(`/api/sessions/${sessionId}/restart`, { method: 'POST' })
      if (res.ok) {
        setReconnectFailed(false)
        reconnect()
      } else if (sessionState === 'disconnected') {
        setReconnectFailed(true)
      }
    } catch { /* ignore network errors */ }
  }, [sessionId, reconnect, sessionState])

  // Handle resize
  const handleResize = useCallback(() => {
    const entry = entryRef.current
    if (!entry) return

    repaintTerminal(entry, setDims)
    const { cols, rows } = entry.term
    if (connected && cols > 0 && rows > 0) {
      send(JSON.stringify({ type: 'resize', cols, rows }))
    }
  }, [send, connected])

  return { handleResize, connected, dims, sessionState, reconnectFailed, restartSession }
}

function flushPendingMessages(
  pendingMessages: PendingTerminalMessage[],
  applyMessageToTerminal: (data: ArrayBuffer | string, isBinary: boolean) => void,
) {
  if (pendingMessages.length === 0) return

  for (const { data, isBinary } of pendingMessages) {
    applyMessageToTerminal(data, isBinary)
  }
  pendingMessages.length = 0
}

function canApplyMessage(entry: TerminalEntry | null, term: Terminal | null): boolean {
  return Boolean(entry?.attachedContainer && term?.element)
}

function getOrCreateTerminalEntry(sessionId: string): TerminalEntry {
  const existing = terminalEntries.get(sessionId)
  if (existing) return existing

  const term = new Terminal({
    cursorBlink: true,
    customGlyphs: true,
    fontSize: 14,
    fontFamily: TERMINAL_FONT_FAMILY,
    // xterm supports Shift-drag for forced selection by default; this option
    // additionally enables Option-drag on macOS while terminal mouse mode is active.
    macOptionClickForcesSelection: true,
    // Hide xterm.js accessibility textarea (still exists for IME/a11y but invisible)
    screenReaderMode: false,
    theme: {
      background: '#1a1b1e',
      foreground: '#d4d4d4',
      cursor: '#a9b7c6',
      black: '#1a1b1e',
      brightBlack: '#555555',
      red: '#f44747',
      brightRed: '#f44747',
      green: '#6a9955',
      brightGreen: '#6a9955',
      yellow: '#dcdcaa',
      brightYellow: '#dcdcaa',
      blue: '#569cd6',
      brightBlue: '#569cd6',
      magenta: '#c586c0',
      brightMagenta: '#c586c0',
      cyan: '#4ec9b0',
      brightCyan: '#4ec9b0',
      white: '#d4d4d4',
      brightWhite: '#ffffff',
    },
    allowProposedApi: true,
  })

  const fitAddon = new FitAddon()
  const webLinksAddon = new WebLinksAddon(undefined, { urlRegex: TERMINAL_URL_REGEX })
  const entry: TerminalEntry = {
    term,
    fitAddon,
    attachedContainer: null,
    disposeTimer: null,
    repaintTimers: new Set(),
    resizeTimers: new Set(),
    send: null,
    replayActive: false,
    replayWriteDepth: 0,
    awaitingReplayEnd: false,
    repoURL: null,
  }

  term.loadAddon(fitAddon)
  term.loadAddon(webLinksAddon)
  term.registerLinkProvider({
    provideLinks(y, callback) {
      callback(computePullRequestLinks(term, entry.repoURL, y))
    },
  })
  term.attachCustomKeyEventHandler((event) => {
    if (!isCopyShortcut(event) || !term.hasSelection()) {
      return true
    }

    copySelection(term.getSelection())
    event.preventDefault()
    return false
  })

  // Use the entry send ref so the same terminal instance can survive pane remounts.
  term.onData((data) => {
    entry.send?.(new TextEncoder().encode(data))
  })

  term.onBinary((data) => {
    const bytes = new Uint8Array(data.length)
    for (let i = 0; i < data.length; i++) {
      bytes[i] = data.charCodeAt(i) & 0xff
    }
    entry.send?.(bytes)
  })

  terminalEntries.set(sessionId, entry)
  return entry
}

function computePullRequestLinks(term: Terminal, repoURL: string | null, y: number) {
  if (!repoURL) return []

  const line = term.buffer.active.getLine(y - 1)
  if (!line) return []

  const text = line.translateToString(true)
  const links = []
  const pattern = /(^|[^\w])#(\d{1,8})(?![\w/])/g
  let match: RegExpExecArray | null
  while ((match = pattern.exec(text)) !== null) {
    const prefix = match[1] ?? ''
    const number = match[2]
    const hashIndex = match.index + prefix.length
    links.push({
      range: {
        start: { x: hashIndex, y: y - 1 },
        end: { x: hashIndex + number.length + 1, y: y - 1 },
      },
      text: `#${number}`,
      activate: () => {
        window.open(`${repoURL}/pull/${number}`, '_blank', 'noopener,noreferrer')
      },
    })
  }
  return links
}

export const __computePullRequestLinksForTests = computePullRequestLinks

function attachTerminal(entry: TerminalEntry, container: HTMLElement) {
  if (!entry.term.element) {
    entry.term.open(container)
    return
  }

  if (entry.term.element.parentElement !== container) {
    // Use appendChild (not replaceChildren) so that React-managed siblings
    // (session-exited overlay) are preserved. replaceChildren()
    // would remove those React-owned DOM nodes, causing React to crash when it
    // next tries to reconcile them.
    container.appendChild(entry.term.element)
  }
}

function refreshTerminal(
  entry: TerminalEntry,
  setDims: (dims: { cols: number; rows: number }) => void,
) {
  clearScheduledRepaints(entry)
  requestAnimationFrame(() => repaintTerminal(entry, setDims))

  for (const delay of REPAINT_SETTLE_DELAYS_MS) {
    const timer = setTimeout(() => {
      entry.repaintTimers.delete(timer)
      requestAnimationFrame(() => repaintTerminal(entry, setDims))
    }, delay)
    entry.repaintTimers.add(timer)
  }
}

function repaintTerminal(
  entry: TerminalEntry,
  setDims: (dims: { cols: number; rows: number }) => void,
) {
  if (!entry.attachedContainer) return

  entry.fitAddon.fit()
  if (entry.term.rows > 0) {
    entry.term.refresh(0, entry.term.rows - 1)
  }
  const { cols, rows } = entry.term
  setDims({ cols, rows })
}

function scheduleConnectedResize(
  entry: TerminalEntry,
  setDims: (dims: { cols: number; rows: number }) => void,
  send: (data: string | ArrayBuffer | Uint8Array) => void,
) {
  clearScheduledResizes(entry)

  const attemptResize = () => {
    if (!entry.attachedContainer) return

    repaintTerminal(entry, setDims)
    const { cols, rows } = entry.term
    if (cols > 0 && rows > 0) {
      clearScheduledResizes(entry)
      send(JSON.stringify({ type: 'resize', cols, rows }))
    }
  }

  requestAnimationFrame(attemptResize)

  for (const delay of REPAINT_SETTLE_DELAYS_MS) {
    const timer = setTimeout(() => {
      entry.resizeTimers.delete(timer)
      requestAnimationFrame(attemptResize)
    }, delay)
    entry.resizeTimers.add(timer)
  }
}

function clearScheduledRepaints(entry: TerminalEntry) {
  for (const timer of entry.repaintTimers) {
    clearTimeout(timer)
  }
  entry.repaintTimers.clear()
}

function clearScheduledResizes(entry: TerminalEntry) {
  for (const timer of entry.resizeTimers) {
    clearTimeout(timer)
  }
  entry.resizeTimers.clear()
}

export function __resetTerminalEntriesForTests() {
  for (const entry of terminalEntries.values()) {
    if (entry.disposeTimer) clearTimeout(entry.disposeTimer)
    clearScheduledRepaints(entry)
    clearScheduledResizes(entry)
    entry.term.dispose()
  }
  terminalEntries.clear()
}

function writeTerminalBytes(entry: TerminalEntry, bytes: Uint8Array) {
  const replayWrite = entry.replayActive
  if (replayWrite) {
    entry.replayWriteDepth++
    setTerminalInputSuppressed(entry, true)
  }

  entry.term.write(bytes, () => {
    if (!replayWrite) return
    entry.replayWriteDepth--
    maybeRestoreTerminalInput(entry)
  })
}

function resetReplayState(entry: TerminalEntry) {
  entry.replayActive = false
  entry.replayWriteDepth = 0
  entry.awaitingReplayEnd = false
  setTerminalInputSuppressed(entry, false)
}

function setTerminalInputSuppressed(entry: TerminalEntry, suppressed: boolean) {
  entry.term.options.disableStdin = suppressed
}

function maybeRestoreTerminalInput(entry: TerminalEntry) {
  if (entry.awaitingReplayEnd || entry.replayWriteDepth > 0) return
  setTerminalInputSuppressed(entry, false)
}

function isCopyShortcut(event: KeyboardEvent): boolean {
  const key = event.key.toLowerCase()
  if (key !== 'c') return false
  return event.metaKey || event.ctrlKey
}

function copySelection(text: string) {
  if (!text) return

  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(text).catch(() => fallbackCopy(text))
    return
  }

  fallbackCopy(text)
}

function fallbackCopy(text: string) {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', '')
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  textarea.style.pointerEvents = 'none'
  document.body.appendChild(textarea)
  textarea.select()
  document.execCommand('copy')
  document.body.removeChild(textarea)
}
