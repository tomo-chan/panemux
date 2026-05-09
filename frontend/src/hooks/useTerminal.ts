import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { MutableRefObject } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { useWebSocket } from './useWebSocket'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'
import { createAgentAttentionDetector } from '../utils/agentAttention'

const ATTENTION_NOTIFY_INTERVAL_MS = 10_000
const REPAINT_SETTLE_DELAYS_MS = [50, 250]

interface UseTerminalOptions {
  sessionId: string
  container: HTMLElement | null
  editMode?: boolean
  onAttention?: (sessionId: string) => void
}

interface TerminalEntry {
  term: Terminal
  fitAddon: FitAddon
  attachedContainer: HTMLElement | null
  disposeTimer: ReturnType<typeof setTimeout> | null
  repaintTimers: Set<ReturnType<typeof setTimeout>>
  resizeTimers: Set<ReturnType<typeof setTimeout>>
  send: ((data: string | ArrayBuffer | Uint8Array) => void) | null
  editMode: boolean
}

interface PendingTerminalMessage {
  data: ArrayBuffer | string
  isBinary: boolean
}

const terminalEntries = new Map<string, TerminalEntry>()

export function useTerminal({ sessionId, container, editMode = false, onAttention }: UseTerminalOptions) {
  const termRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const initializedRef = useRef(false)
  const sendRef = useRef<((data: string | ArrayBuffer | Uint8Array) => void) | null>(null)
  const entryRef = useRef<TerminalEntry | null>(null)
  const attentionDetectorRef = useRef(createAgentAttentionDetector())
  const outputDecoderRef = useRef(new TextDecoder())
  const lastAttentionAtRef = useRef<number | null>(null)
  const pendingMessagesRef = useRef<PendingTerminalMessage[]>([])
  const [dims, setDims] = useState<{ cols: number; rows: number } | null>(null)
  const [sessionExited, setSessionExited] = useState(false)

  const applyMessageToTerminal = useCallback((data: ArrayBuffer | string, isBinary: boolean) => {
    const term = termRef.current
    if (!term) return

    if (isBinary) {
      const bytes = new Uint8Array(data as ArrayBuffer)
      term.write(bytes)
      const text = outputDecoderRef.current.decode(bytes, { stream: true })
      if (shouldNotifyAttention(attentionDetectorRef.current.feed(text), lastAttentionAtRef)) {
        onAttention?.(sessionId)
      }
    } else {
      try {
        const msg = JSON.parse(data as string)
        if (msg.type === 'status') {
          console.log(`Session ${sessionId} status:`, msg.state)
          if (msg.state === 'exited') {
            setSessionExited(true)
            term.write('\r\n\x1b[2m[Session ended]\x1b[0m\r\n')
          }
        } else if (msg.type === 'error') {
          term.write(`\r\n\x1b[31mError: ${msg.message}\x1b[0m\r\n`)
        }
      } catch {
        // Not JSON, treat as text
        const text = data as string
        term.write(text)
        if (shouldNotifyAttention(attentionDetectorRef.current.feed(text), lastAttentionAtRef)) {
          onAttention?.(sessionId)
        }
      }
    }
  }, [onAttention, sessionId])

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
  const { send, connected, reconnect } = useWebSocket(wsUrl, {
    onMessage: handleMessage,
    onOpen: () => {
      lastAttentionAtRef.current = null
      attentionDetectorRef.current.reset()
      outputDecoderRef.current = new TextDecoder()
      setSessionExited(false)
    },
  })

  // Keep sendRef in sync so onData closure always has the latest send
  useLayoutEffect(() => {
    sendRef.current = send
    if (entryRef.current) {
      entryRef.current.send = send
    }
  }, [send])

  // Sync editMode into the entry so onData/onBinary handlers can read it
  useLayoutEffect(() => {
    if (entryRef.current) entryRef.current.editMode = editMode
  }, [editMode])

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
    entry.editMode = editMode
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

  const restartSession = useCallback(async () => {
    try {
      const res = await fetch(`/api/sessions/${sessionId}/restart`, { method: 'POST' })
      if (res.ok) reconnect()
    } catch { /* ignore network errors */ }
  }, [sessionId, reconnect])

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

  return { handleResize, connected, dims, sessionExited, restartSession }
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
  const webLinksAddon = new WebLinksAddon()
  const entry: TerminalEntry = {
    term,
    fitAddon,
    attachedContainer: null,
    disposeTimer: null,
    repaintTimers: new Set(),
    resizeTimers: new Set(),
    send: null,
    editMode: false,
  }

  term.loadAddon(fitAddon)
  term.loadAddon(webLinksAddon)
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
    if (!entry.editMode) entry.send?.(new TextEncoder().encode(data))
  })

  term.onBinary((data) => {
    if (!entry.editMode) {
      const bytes = new Uint8Array(data.length)
      for (let i = 0; i < data.length; i++) {
        bytes[i] = data.charCodeAt(i) & 0xff
      }
      entry.send?.(bytes)
    }
  })

  terminalEntries.set(sessionId, entry)
  return entry
}

function attachTerminal(entry: TerminalEntry, container: HTMLElement) {
  if (!entry.term.element) {
    entry.term.open(container)
    return
  }

  if (entry.term.element.parentElement !== container) {
    // Use appendChild (not replaceChildren) so that React-managed siblings
    // (edit-mode overlay, session-exited overlay) are preserved.  replaceChildren()
    // would remove those React-owned DOM nodes, causing React to crash when it
    // next tries to reconcile them (e.g. removeChild on a detached node when
    // edit mode is toggled off).
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

function shouldNotifyAttention(detected: boolean, lastAttentionAtRef: MutableRefObject<number | null>): boolean {
  if (!detected) return false

  const now = Date.now()
  if (
    lastAttentionAtRef.current !== null &&
    now - lastAttentionAtRef.current < ATTENTION_NOTIFY_INTERVAL_MS
  ) {
    return false
  }

  lastAttentionAtRef.current = now
  return true
}
