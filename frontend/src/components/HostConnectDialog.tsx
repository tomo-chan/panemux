import React, { useEffect, useRef, useState } from 'react'
import type { RefObject } from 'react'
import type { HostSessionName } from '../schemas'
import type { HostTerminalType, TaskActionResult } from '../hooks/useTasks'
import { useModalKeyboard } from '../hooks/useModalKeyboard'

// The connection menu a task dashboard host chip opens (issue #314): ssh or
// ssh_tmux, then Open (a workspace pane) or Type in pane (a popup on the
// board). The ssh_tmux session name is the server's (POST
// /api/hosts/session-name), asked for once the first time ssh_tmux is chosen
// and used by both buttons, so the name shown is the name opened.

type SessionNamePhase =
  | { phase: 'idle' }
  | { phase: 'loading' }
  | { phase: 'ready'; name: string }
  | { phase: 'failed'; error: string }

export interface HostConnectDialogProps {
  host: string
  sessionName: (host: string) => Promise<TaskActionResult<HostSessionName>>
  onCancel: () => void
  onOpen: (type: HostTerminalType, tmuxSession: string | undefined) => void
  onTypeIn: (type: HostTerminalType, tmuxSession: string | undefined) => void
  /** The chip that opened the menu: a press on it toggles the menu rather than counting as outside. */
  anchorRef?: RefObject<HTMLElement | null>
}

export const HostConnectDialog: React.FC<HostConnectDialogProps> = ({
  host,
  sessionName,
  onCancel,
  onOpen,
  onTypeIn,
  anchorRef,
}) => {
  const dialogRef = useRef<HTMLDivElement>(null)
  const typeInRef = useRef<HTMLButtonElement>(null)
  const [type, setType] = useState<HostTerminalType>('ssh')
  const [name, setName] = useState<SessionNamePhase>({ phase: 'idle' })

  useModalKeyboard({ isOpen: true, dialogRef, onEscape: onCancel })

  useEffect(() => {
    typeInRef.current?.focus()
  }, [])

  // A popover, not a sheet over the board: a press anywhere else closes it.
  const cancelRef = useRef(onCancel)
  cancelRef.current = onCancel
  useEffect(() => {
    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target instanceof Node ? event.target : null
      if (!target || dialogRef.current?.contains(target) || anchorRef?.current?.contains(target)) return
      cancelRef.current()
    }
    document.addEventListener('pointerdown', handlePointerDown, true)
    return () => document.removeEventListener('pointerdown', handlePointerDown, true)
  }, [anchorRef])

  // An answer that arrives after the menu closed is dropped.
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const choose = (next: HostTerminalType) => {
    setType(next)
    if (next !== 'ssh_tmux' || name.phase !== 'idle') return
    setName({ phase: 'loading' })
    void sessionName(host).then((result) => {
      if (!mounted.current) return
      setName(result.ok ? { phase: 'ready', name: result.launched.tmux_session } : { phase: 'failed', error: result.error })
    })
  }

  const tmuxSession = type === 'ssh_tmux' && name.phase === 'ready' ? name.name : undefined
  const ready = type === 'ssh' || tmuxSession !== undefined

  return (
    <div ref={dialogRef} role="dialog" aria-modal="true" aria-label={`Open a terminal on ${host}`} className="td-host-menu">
      <div className="td-host-menu-title">
        <span className="td-host-dot" aria-hidden="true" />
        <b>{host}</b>
        <span className="td-muted">Open a terminal</span>
      </div>
      <fieldset className="td-host-menu-options">
        <legend>Connection</legend>
        <label className="td-host-menu-option">
          <input type="radio" name="td-host-connection" value="ssh" checked={type === 'ssh'} onChange={() => choose('ssh')} />
          <span>ssh</span>
          <span className="td-muted">plain login shell</span>
        </label>
        <label className="td-host-menu-option">
          <input
            type="radio"
            name="td-host-connection"
            value="ssh_tmux"
            checked={type === 'ssh_tmux'}
            onChange={() => choose('ssh_tmux')}
          />
          <span>ssh_tmux</span>
          <span className="td-muted">keeps running after close</span>
        </label>
        {type === 'ssh_tmux' && name.phase !== 'failed' && (
          <div className="td-muted td-host-menu-tmux">
            tmux session: <code>{name.phase === 'ready' ? name.name : '…'}</code>
          </div>
        )}
        {type === 'ssh_tmux' && name.phase === 'failed' && (
          <div role="alert" className="td-host-menu-tmux" data-tone="error">
            Could not name the tmux session: {name.error}
          </div>
        )}
      </fieldset>
      <div className="td-host-menu-actions">
        <button type="button" className="td-btn" onClick={onCancel}>
          Cancel
        </button>
        <button
          type="button"
          className="td-btn"
          aria-label={`Open: ${host}`}
          disabled={!ready}
          onClick={() => onOpen(type, tmuxSession)}
        >
          Open
        </button>
        <button
          ref={typeInRef}
          type="button"
          className="td-btn td-btn-primary"
          aria-label={`Type in pane: ${host}`}
          disabled={!ready}
          onClick={() => onTypeIn(type, tmuxSession)}
        >
          Type in pane
        </button>
      </div>
    </div>
  )
}
