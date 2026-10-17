import React, { useEffect, useRef, useState } from 'react'
import type { CodexHook, CodexHookAuto } from '../schemas'
import type { TaskActionResult } from '../hooks/useTasks'

// The Codex input notifications item of a host's menu (issue #396): whether
// the host's codex has panemux's permission hook, and Install or Update. A
// host without codex shows nothing; one that could not be checked says so
// rather than being taken for a host without codex. Update automatically
// (issue #402) has panemux run Update by itself when the hook differs.

export interface CodexHookItemProps {
  host: string
  /** The host's name as the menu shows it. */
  label?: string
  load: (host: string) => Promise<TaskActionResult<CodexHook>>
  install: (host: string) => Promise<TaskActionResult<CodexHook>>
  setAuto: (host: string, enabled: boolean) => Promise<TaskActionResult<CodexHookAuto>>
  /** The item is all the menu has: a host without codex says so. */
  alone?: boolean
}

type Phase =
  | { phase: 'loading' }
  | { phase: 'ready'; hook: CodexHook }
  | { phase: 'failed'; error: string }

export const CodexHookItem: React.FC<CodexHookItemProps> = ({ host, label = host, load, install, setAuto, alone = false }) => {
  const [state, setState] = useState<Phase>({ phase: 'loading' })
  const [installing, setInstalling] = useState(false)
  const [installed, setInstalled] = useState(false)
  const [installError, setInstallError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  // An answer that arrives after the menu closed is dropped.
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    void load(host).then((result) => {
      if (!mounted.current) return
      setState(result.ok ? { phase: 'ready', hook: result.launched } : { phase: 'failed', error: result.error })
    })
    return () => {
      mounted.current = false
    }
  }, [host, load])

  const run = () => {
    setInstalling(true)
    setInstallError(null)
    void install(host).then((result) => {
      if (!mounted.current) return
      setInstalling(false)
      if (result.ok) {
        setState({ phase: 'ready', hook: result.launched })
        setInstalled(true)
      } else {
        setInstallError(result.error)
      }
    })
  }

  const toggleAuto = (hook: CodexHook) => {
    setSaving(true)
    setSaveError(null)
    void setAuto(host, !hook.auto_update).then((result) => {
      if (!mounted.current) return
      setSaving(false)
      if (result.ok) {
        setState({ phase: 'ready', hook: { ...hook, auto_update: result.launched.auto_update } })
      } else {
        setSaveError(result.error)
      }
    })
  }

  const hook = state.phase === 'ready' ? state.hook : null
  if (hook?.status === 'absent' && !alone) return null

  let detail: React.ReactNode
  let action: string | null = null
  if (state.phase === 'loading') {
    detail = <span className="td-muted">Checking…</span>
  } else if (state.phase === 'failed') {
    detail = <span className="td-muted">Not checked: {state.error}</span>
  } else {
    switch (state.hook.status) {
      case 'absent':
        detail = <span className="td-muted">No Codex found on {label}</span>
        break
      case 'missing':
        detail = <span className="td-muted">Not installed</span>
        action = 'Install'
        break
      case 'outdated':
        detail = <span className="td-muted">Differs from this panemux's hook</span>
        action = 'Update'
        break
      case 'installed':
        detail = <span className="td-host-menu-installed">Installed</span>
        break
      case 'invalid':
        detail = (
          <span role="alert" data-tone="error">
            Cannot install: {state.hook.error}
          </span>
        )
        break
      default:
        detail = <span className="td-muted">Not checked: {state.hook.error}</span>
    }
  }

  return (
    <fieldset className="td-host-menu-codex" aria-label="Codex input notifications">
      <div className="td-host-menu-codex-row">
        <span>Codex input notifications</span>
        {action && (
          <button
            type="button"
            className="td-btn td-btn-sm"
            aria-label={`${action} Codex input notifications on ${label}`}
            disabled={installing}
            onClick={run}
          >
            {installing ? 'Installing…' : action}
          </button>
        )}
      </div>
      <div className="td-host-menu-codex-detail">{detail}</div>
      {hook && (hook.status === 'missing' || hook.status === 'outdated' || hook.status === 'installed') && (
        <label className="td-host-menu-codex-auto">
          <input
            type="checkbox"
            checked={hook.auto_update ?? false}
            disabled={saving}
            onChange={() => toggleAuto(hook)}
          />
          Update automatically
        </label>
      )}
      {hook?.auto_updated && !installed && (
        <div role="status" className="td-muted">
          Updated automatically. Trust it in codex's /hooks before it runs.
        </div>
      )}
      {hook?.auto_error && (
        <div role="alert" data-tone="error">
          Could not update automatically: {hook.auto_error}
        </div>
      )}
      {saveError && (
        <div role="alert" data-tone="error">
          Could not save: {saveError}
        </div>
      )}
      {installed && (
        <div role="status" className="td-muted">
          Trust it in codex's /hooks before it runs.
        </div>
      )}
      {installError && (
        <div role="alert" data-tone="error">
          Could not install: {installError}
        </div>
      )}
    </fieldset>
  )
}
