import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { useWebSocket } from './useWebSocket'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface UseTerminalOptions {
  sessionId: string
  container: HTMLElement | null
}

interface TerminalEntry {
  term: Terminal
  fitAddon: FitAddon
  attachedContainer: HTMLElement | null
  disposeTimer: ReturnType<typeof setTimeout> | null
  send: ((data: string | ArrayBuffer | Uint8Array) => void) | null
}

const terminalEntries = new Map<string, TerminalEntry>()

export function useTerminal({ sessionId, container }: UseTerminalOptions) {
  const termRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const initializedRef = useRef(false)
  const sendRef = useRef<((data: string | ArrayBuffer | Uint8Array) => void) | null>(null)
  const entryRef = useRef<TerminalEntry | null>(null)
  const [dims, setDims] = useState<{ cols: number; rows: number } | null>(null)
  const [sessionExited, setSessionExited] = useState(false)

  const handleMessage = useCallback((data: ArrayBuffer | string, isBinary: boolean) => {
    if (!termRef.current) return

    if (isBinary) {
      termRef.current.write(new Uint8Array(data as ArrayBuffer))
    } else {
      try {
        const msg = JSON.parse(data as string)
        if (msg.type === 'status') {
          console.log(`Session ${sessionId} status:`, msg.state)
          if (msg.state === 'exited') {
            setSessionExited(true)
            termRef.current.write('\r\n\x1b[2m[Session ended]\x1b[0m\r\n')
          }
        } else if (msg.type === 'error') {
          termRef.current.write(`\r\n\x1b[31mError: ${msg.message}\x1b[0m\r\n`)
        }
      } catch {
        // Not JSON, treat as text
        termRef.current.write(data as string)
      }
    }
  }, [sessionId])

  const wsUrl = `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/${sessionId}`
  const { send, connected, reconnect } = useWebSocket(wsUrl, {
    onMessage: handleMessage,
    onOpen: () => {
      setSessionExited(false)
      if (fitAddonRef.current && termRef.current) {
        fitAddonRef.current.fit()
        const { cols, rows } = termRef.current
        sendRef.current?.(JSON.stringify({ type: 'resize', cols, rows }))
      }
    },
  })

  // Keep sendRef in sync so onData closure always has the latest send
  useLayoutEffect(() => {
    sendRef.current = send
    if (entryRef.current) {
      entryRef.current.send = send
    }
  }, [send])

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
    refreshTerminal(entry, setDims)

    return () => {
      const currentEntry = entryRef.current
      if (!currentEntry) return

      currentEntry.attachedContainer = null
      currentEntry.send = null
      currentEntry.disposeTimer = setTimeout(() => {
        if (currentEntry.attachedContainer) return
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

    entry.fitAddon.fit()
    entry.term.refresh(0, entry.term.rows - 1)
    const { cols, rows } = entry.term
    setDims({ cols, rows })
    if (connected) {
      send(JSON.stringify({ type: 'resize', cols, rows }))
    }
  }, [send, connected])

  return { handleResize, connected, dims, sessionExited, restartSession }
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
    send: null,
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

function attachTerminal(entry: TerminalEntry, container: HTMLElement) {
  if (!entry.term.element) {
    entry.term.open(container)
    return
  }

  if (entry.term.element.parentElement !== container) {
    container.replaceChildren()
    container.appendChild(entry.term.element)
  }
}

function refreshTerminal(
  entry: TerminalEntry,
  setDims: (dims: { cols: number; rows: number }) => void,
) {
  requestAnimationFrame(() => {
    entry.fitAddon.fit()
    entry.term.refresh(0, entry.term.rows - 1)
    const { cols, rows } = entry.term
    setDims({ cols, rows })
  })
}

export function __resetTerminalEntriesForTests() {
  for (const entry of terminalEntries.values()) {
    if (entry.disposeTimer) clearTimeout(entry.disposeTimer)
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
