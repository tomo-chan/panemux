import React, { useEffect, useRef, useState } from 'react'
import type { CodexHook as HostToolState, CodexHookAuto as HostToolAuto } from '../schemas'
import type { TaskActionResult } from '../hooks/useTasks'

export interface HostToolItemProps {
  tool: ToolDefinition

  host: string
  /** The host's name as the menu shows it. */
  label?: string
  load: (host: string) => Promise<TaskActionResult<HostToolState>>
  install: (host: string) => Promise<TaskActionResult<HostToolState>>
  setAuto: (host: string, enabled: boolean) => Promise<TaskActionResult<HostToolAuto>>
  /** The item is all the menu has: a host without the tool says so. */
  alone?: boolean
}

type Phase =
  | { phase: 'loading' }
  | { phase: 'ready'; hook: HostToolState }
  | { phase: 'failed'; error: string }

export const HostToolItem: React.FC<HostToolItemProps> = ({ tool, host, label = host, load, install, setAuto, alone = false }) => {
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

  const toggleAuto = (hook: HostToolState) => {
    setSaving(true)
    setSaveError(null)
    void setAuto(host, !hook.auto_update).then(async (result) => {
      if (!mounted.current) return
      if (!result.ok) {
        setSaving(false)
        setSaveError(result.error)
        return
      }
      setState({ phase: 'ready', hook: { ...hook, auto_update: result.launched.auto_update } })
      // Turned on while it shows Update: check once, which updates it.
      if (result.launched.auto_update && hook.status === 'outdated') {
        const checked = await load(host)
        if (!mounted.current) return
        if (checked.ok) setState({ phase: 'ready', hook: checked.launched })
      }
      setSaving(false)
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
        detail = <span className="td-muted">{tool.absent(label)}</span>
        break
      case 'missing':
        detail = <span className="td-muted">Not installed</span>
        action = 'Install'
        break
      case 'outdated':
        detail = <span className="td-muted">{tool.outdated}</span>
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
    <fieldset className="td-host-menu-codex" aria-label={tool.title}>
      <div className="td-host-menu-codex-row">
        <span>{tool.title}</span>
        {action && (
          <button
            type="button"
            className="td-btn td-btn-sm"
            aria-label={`${action} ${tool.title} on ${label}`}
            disabled={installing || saving}
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
            disabled={saving || installing || !!hook.auto_settings_error}
            onChange={() => toggleAuto(hook)}
          />
          Update automatically
        </label>
      )}
      {hook?.auto_updated && !installed && (
        <div role="status" className="td-muted">
          {tool.autoUpdatedNotice}
        </div>
      )}
      {hook?.auto_error && (
        <div role="alert" data-tone="error">
          Could not update automatically: {hook.auto_error}
        </div>
      )}
      {hook?.auto_settings_error && (
        <div role="alert" data-tone="error">
          Could not read the setting: {hook.auto_settings_error}
        </div>
      )}
      {saveError && (
        <div role="alert" data-tone="error">
          Could not save: {saveError}
        </div>
      )}
      {installed && (
        <div role="status" className="td-muted">
          {tool.installedNotice}
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

// Tool-specific copy is supplied by the adapter, while menu behavior is shared.
export interface ToolDefinition {
  id: string
  title: string
  absent: (label: string) => string
  outdated: string
  installedNotice: string
  autoUpdatedNotice: string
}
