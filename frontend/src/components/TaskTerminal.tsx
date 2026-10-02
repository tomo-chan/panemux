import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useTerminal } from '../hooks/useTerminal'

// The terminal of the task dashboard's Type in pane popup (issue #284): the
// board's temporary attach (`/ws/{session_id}`, issue #283) drawn by the same
// useTerminal a pane uses, so IME, Enter, arrow keys, paste, copy and
// scrollback behave as they do in a pane. It carries none of a pane's header,
// status bar, layout or drag handling.

/** Where the popup's terminal stands, from its WebSocket and the session's status frames. */
export type TaskTerminalStatus = 'connecting' | 'connected' | 'disconnected' | 'ended' | 'failed'

export function taskTerminalStatus({
  connected,
  sessionState,
  reconnectFailed,
  everConnected,
}: {
  connected: boolean
  sessionState: 'running' | 'disconnected' | 'exited'
  reconnectFailed: boolean
  everConnected: boolean
}): TaskTerminalStatus {
  // The tmux client exited: the session ended, or ended between the server's
  // check and the attach, and tmux's message is on the terminal.
  if (sessionState === 'exited') return 'ended'
  if (connected && sessionState === 'running') return 'connected'
  if (everConnected) return 'disconnected'
  return reconnectFailed ? 'failed' : 'connecting'
}

interface TaskTerminalProps {
  sessionId: string
  onStatus: (status: TaskTerminalStatus) => void
}

export const TaskTerminal: React.FC<TaskTerminalProps> = ({ sessionId, onStatus }) => {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  const [everConnected, setEverConnected] = useState(false)
  const { connected, sessionState, reconnectFailed, handleResize } = useTerminal({
    sessionId,
    container,
    // /api/sessions/{id}/restart knows only panes; the popup opens the attach again instead.
    recoverOnDisconnect: false,
  })

  if (connected && !everConnected) setEverConnected(true)
  const status = taskTerminalStatus({ connected, sessionState, reconnectFailed, everConnected: everConnected || connected })
  const usable = status === 'connected'

  const onStatusRef = useRef(onStatus)
  onStatusRef.current = onStatus
  useEffect(() => {
    onStatusRef.current(status)
  }, [status])

  // Input waits for the connection: nothing typed before it opens, or after
  // it drops, is taken and then lost.
  useEffect(() => {
    if (!container) return
    if (usable) container.removeAttribute('inert')
    else container.setAttribute('inert', '')
  }, [container, usable])

  // Focus goes to the terminal each time it becomes usable, so the first key
  // after opening or reconnecting is an answer.
  useEffect(() => {
    if (!usable || !container) return
    container.querySelector<HTMLElement>('.xterm-helper-textarea')?.focus()
  }, [container, usable])

  const resize = useCallback(() => handleResize(), [handleResize])
  useEffect(() => {
    if (!container) return
    const observer = new ResizeObserver(resize)
    observer.observe(container)
    return () => observer.disconnect()
  }, [container, resize])

  return (
    <div
      ref={setContainer}
      className="td-term-surface"
      data-testid="task-terminal-surface"
      data-input={usable ? 'on' : 'off'}
    />
  )
}
