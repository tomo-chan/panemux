import React, { useEffect, useRef, useState } from 'react'
import { useModalKeyboard } from '../hooks/useModalKeyboard'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

export type WorkspaceDestination = { workspaceId: string } | { newName: string }
interface Props {
  workspaces: { id: string; title: string }[]
  active: string
  onOpen: (destination: WorkspaceDestination, onWorkspaceCreated: (id: string) => void) => Promise<void>
  onCancel: () => void
}
const button: React.CSSProperties = { padding: '6px 14px', border: '1px solid #555', borderRadius: 3,
  background: '#3c3c3c', color: '#d4d4d4', font: 'inherit', cursor: 'pointer' }

export function WorkspacePickerDialog({ workspaces, active, onOpen, onCancel }: Props) {
  const [selected, setSelected] = useState<string | null>(active)
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const submitting = useRef(false)
  const dialogRef = useRef<HTMLDivElement>(null)
  const nameRef = useRef<HTMLInputElement>(null)
  useModalKeyboard({ isOpen: true, dialogRef, onEscape: busy ? undefined : onCancel })
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    dialogRef.current?.querySelector<HTMLInputElement>('input:checked')?.focus()
    return () => { if (previous?.isConnected) previous.focus() }
  }, [])
  useEffect(() => { if (selected === null) nameRef.current?.focus() }, [selected])

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (submitting.current) return
    const title = name.trim()
    if (selected === null && (!title || workspaces.some(workspace => workspace.title === title))) {
      setError(title ? 'A workspace with this name already exists' : 'Enter a workspace name')
      nameRef.current?.focus()
      return
    }
    if (selected !== null && !workspaces.some(workspace => workspace.id === selected)) {
      setError('Workspace no longer exists')
      return
    }
    submitting.current = true
    setBusy(true)
    setError(null)
    try {
      await onOpen(selected === null ? { newName: title } : { workspaceId: selected }, setSelected)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to open terminal')
    } finally {
      submitting.current = false
      setBusy(false)
    }
  }
  return (
    <div ref={dialogRef} role="dialog" aria-modal="true" aria-label="Open in workspace"
      style={{ position: 'fixed', inset: 0, zIndex: 1200, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <form onSubmit={event => { void submit(event) }} style={{ width: 394, maxWidth: 'calc(100vw - 32px)', maxHeight: 'calc(100vh - 32px)', overflowY: 'auto',
        background: '#252526', color: '#d4d4d4', border: '1px solid #444', borderRadius: 6, padding: 20, fontFamily: TERMINAL_FONT_FAMILY, fontSize: 13 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
          <strong style={{ fontSize: 16 }}>Open in workspace</strong>
          <button type="button" aria-label="Close" disabled={busy} onClick={onCancel} style={button}>×</button>
        </div>
        <fieldset disabled={busy} style={{ border: 0, padding: 0, margin: 0 }}>
          <legend style={{ marginBottom: 8 }}>Choose where to add the pane</legend>
          {workspaces.map(workspace => (
            <label key={workspace.id} style={{ display: 'flex', gap: 10, padding: 10, marginBottom: 6, border: '1px solid #444', borderRadius: 4 }}>
              <input type="radio" name="workspace" checked={selected === workspace.id} onChange={() => { setSelected(workspace.id); setError(null) }} />
              {workspace.title}
            </label>
          ))}
          <label style={{ display: 'flex', gap: 10, padding: 10 }}>
            <input type="radio" name="workspace" checked={selected === null} onChange={() => { setSelected(null); setError(null) }} />
            + New workspace
          </label>
          {selected === null && <label style={{ display: 'grid', gap: 6, marginTop: 10 }}>
            Workspace name
            <input ref={nameRef} value={name} onChange={event => { setName(event.target.value); setError(null) }}
              aria-invalid={!!error} aria-describedby={error ? 'workspace-picker-error' : undefined}
              style={{ padding: 8, background: '#1e1e1e', color: '#d4d4d4', border: '1px solid #555', font: 'inherit' }} />
          </label>}
        </fieldset>
        {error && <p role="alert" id="workspace-picker-error" style={{ color: '#fca5a5' }}>{error}</p>}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 20 }}>
          <button type="button" disabled={busy} onClick={onCancel} style={button}>Cancel</button>
          <button type="submit" disabled={busy} style={{ ...button, background: '#0e639c', color: '#fff', borderColor: '#0e639c' }}>
            {busy ? 'Opening…' : selected === null ? 'Create and open' : 'Open here'}
          </button>
        </div>
      </form>
    </div>
  )
}
