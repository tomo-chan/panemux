import React, { useEffect, useRef, useState } from 'react'
import { useModalKeyboard } from '../hooks/useModalKeyboard'
import type { SSHConnectionsState } from '../hooks/useSSHConnections'
import type { SSHConnectionEntry, SSHConnectionRequest } from '../schemas'

// The task dashboard's Hosts… dialog (issue #272): lists, adds, edits and
// deletes config.yaml's ssh_connections, the hosts the dashboard collects
// from. It is distinct from the pane settings' Add SSH Host, which writes
// ~/.ssh/config. The server checks everything again; what is checked here is
// only what can be told without asking it.
//
// A saved password is never shown: an entry only says it has one. Leaving the
// password empty on an edit keeps it; "Remove saved password" removes it.

export interface DashboardHostsDialogProps {
  isOpen: boolean
  state: SSHConnectionsState
  /** Called after an entry was added, edited or deleted. */
  onChanged: () => void
  onClose: () => void
}

// The name rule the server applies to a new entry (config.ValidateSSHConnectionName).
const NAME_RE = /^[A-Za-z0-9_.-]+$/

interface FormValues {
  name: string
  host: string
  user: string
  port: string
  keyFile: string
  knownHostsFile: string
  password: string
}

interface FormState {
  mode: 'add' | 'edit'
  /** The entry being edited. */
  entry: SSHConnectionEntry | null
  values: FormValues
  clearPassword: boolean
  error: string | null
}

type Flash = { kind: 'status' | 'alert'; text: string }

const emptyValues: FormValues = { name: '', host: '', user: '', port: '', keyFile: '', knownHostsFile: '', password: '' }

function valuesOf(entry: SSHConnectionEntry): FormValues {
  return {
    name: entry.name,
    host: entry.host ?? '',
    user: entry.user ?? '',
    port: entry.port ? String(entry.port) : '',
    keyFile: entry.key_file ?? '',
    knownHostsFile: entry.known_hosts_file ?? '',
    password: '',
  }
}

function connectsTo(entry: SSHConnectionEntry): string {
  if (!entry.host) return entry.in_ssh_config ? 'from ~/.ssh/config' : '—'
  return `${entry.user ? `${entry.user}@` : ''}${entry.host}${entry.port ? `:${entry.port}` : ''}`
}

/** Why a pane-using entry cannot be deleted, or null when it can. */
function deleteRefusal(entry: SSHConnectionEntry): string | null {
  if (entry.panes.length === 0 || entry.in_ssh_config) return null
  return (
    `Cannot delete ${entry.name}: it is used by pane ${entry.panes.join(', ')}, and ~/.ssh/config has no ` +
    `Host block of that name for them to fall back to. Change those panes' connection first.`
  )
}

export const DashboardHostsDialog: React.FC<DashboardHostsDialogProps> = ({ isOpen, state, onChanged, onClose }) => {
  const { connections, sshConfigNames, error, load, create, update, remove } = state
  const [form, setForm] = useState<FormState | null>(null)
  const [confirming, setConfirming] = useState<string | null>(null)
  const [flash, setFlash] = useState<Flash | null>(null)
  const [saving, setSaving] = useState(false)
  const dialogRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!isOpen) return
    setForm(null)
    setConfirming(null)
    setFlash(null)
    void load()
  }, [isOpen, load])

  useModalKeyboard({ isOpen, dialogRef, onEscape: saving ? undefined : onClose })

  if (!isOpen) return null

  const startAdd = () => {
    setFlash(null)
    setConfirming(null)
    setForm({ mode: 'add', entry: null, values: emptyValues, clearPassword: false, error: null })
  }
  const startEdit = (entry: SSHConnectionEntry) => {
    setFlash(null)
    setConfirming(null)
    setForm({ mode: 'edit', entry, values: valuesOf(entry), clearPassword: false, error: null })
  }
  const startDelete = (entry: SSHConnectionEntry) => {
    setForm(null)
    const refusal = deleteRefusal(entry)
    if (refusal) {
      setConfirming(null)
      setFlash({ kind: 'alert', text: refusal })
      return
    }
    setFlash(null)
    setConfirming(entry.name)
  }

  const setValue = (key: keyof FormValues, value: string) =>
    setForm((current) => (current ? { ...current, values: { ...current.values, [key]: value }, error: null } : current))

  const save = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!form) return
    const v = form.values
    const name = v.name.trim()
    const host = v.host.trim()
    const fail = (message: string) => setForm({ ...form, error: message })

    if (form.mode === 'add') {
      if (name === '') return fail('Enter a name.')
      if (!NAME_RE.test(name)) return fail('A name can contain only letters, digits, hyphens, underscores and dots.')
      if (connections?.some((c) => c.name === name)) {
        return fail(`A dashboard host named "${name}" already exists. Edit it instead, or choose another name.`)
      }
    }
    const fromSSHConfig = form.mode === 'edit' ? form.entry?.in_ssh_config === true : sshConfigNames.includes(name)
    if (host === '' && !fromSSHConfig) {
      return fail(`Enter a host. ~/.ssh/config has no Host block named "${name}" to take it from.`)
    }
    let port: number | undefined
    if (v.port.trim() !== '') {
      // Digits only: Number() would also take 0x16 or 1e3.
      port = /^\d+$/.test(v.port.trim()) ? Number(v.port.trim()) : NaN
      if (!Number.isInteger(port) || port < 1 || port > 65535) return fail('Port must be a whole number from 1 to 65535.')
    }

    const request: SSHConnectionRequest = {
      ...(form.mode === 'add' ? { name } : {}),
      ...(host ? { host } : {}),
      ...(v.user.trim() ? { user: v.user.trim() } : {}),
      ...(port !== undefined ? { port } : {}),
      ...(v.keyFile.trim() ? { key_file: v.keyFile.trim() } : {}),
      ...(v.knownHostsFile.trim() ? { known_hosts_file: v.knownHostsFile.trim() } : {}),
      ...(form.clearPassword ? { clear_password: true } : v.password ? { password: v.password } : {}),
    }

    setSaving(true)
    const reason = form.mode === 'add' ? await create(request) : await update(name, request)
    setSaving(false)
    if (reason) return fail(`Could not save: ${reason}`)
    setForm(null)
    setFlash({
      kind: 'status',
      text:
        form.mode === 'add'
          ? `Added ${name}. It appears on the dashboard at the next update.`
          : `Saved ${name}. The dashboard reconnects to it with the new settings.`,
    })
    onChanged()
  }

  const confirmDelete = async (name: string) => {
    setSaving(true)
    const reason = await remove(name)
    setSaving(false)
    setConfirming(null)
    if (reason) {
      setFlash({ kind: 'alert', text: `Could not delete ${name}: ${reason}` })
      return
    }
    setFlash({ kind: 'status', text: `Deleted ${name}. The dashboard stops collecting from it.` })
    onChanged()
  }

  const formName = form?.values.name.trim() ?? ''
  const inheritsFrom = form && (form.mode === 'edit' ? form.entry?.in_ssh_config : sshConfigNames.includes(formName))
  const hasSavedPassword = form?.mode === 'edit' && form.entry?.has_password === true

  return (
    <div
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-labelledby="td-hosts-title"
      className="td-modal"
      onClick={(event) => {
        if (event.target === event.currentTarget && !saving) onClose()
      }}
    >
      <div className="td-modal-panel td-hosts-panel">
        <h2 id="td-hosts-title">
          Dashboard hosts <code className="td-hosts-source">ssh_connections in config.yaml</code>
        </h2>
        <p className="td-note">
          The task dashboard collects from this machine and from these hosts. Panes can use them too. A host that is
          only in ~/.ssh/config works in panes but is not collected from until you add its name here.
        </p>

        {error && (
          <div role="alert" className="td-alert td-alert-inline">
            Could not load the hosts: {error}
          </div>
        )}
        {flash && (
          <div role={flash.kind} className={flash.kind === 'alert' ? 'td-alert td-alert-inline' : 'td-ok'}>
            {flash.text}
          </div>
        )}

        <div className="td-hosts-table-wrap">
          <table className="td-hosts-table">
            <thead>
              <tr>
                <th scope="col">Name</th>
                <th scope="col">Connects to</th>
                <th scope="col">Details</th>
                <th scope="col">
                  <span className="td-visually-hidden">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {connections?.length === 0 && (
                <tr>
                  <td colSpan={4} className="td-empty">
                    No dashboard hosts yet. The dashboard collects from this machine only. Add a host, or add just the
                    name of a host from ~/.ssh/config.
                  </td>
                </tr>
              )}
              {connections?.map((entry) => (
                <tr key={entry.name} className={form?.entry?.name === entry.name ? 'td-hosts-editing' : undefined}>
                  <th scope="row" className="td-hosts-name">
                    {entry.name}
                  </th>
                  <td className="td-hosts-addr">{connectsTo(entry)}</td>
                  <td>
                    <span className="td-hosts-tags">
                      {entry.in_ssh_config && (
                        <span className="td-hosts-tag" title="Fields left empty come from the ~/.ssh/config Host block of this name.">
                          ~/.ssh/config
                        </span>
                      )}
                      {entry.key_file && (
                        <span className="td-hosts-tag" title={entry.key_file}>
                          key file
                        </span>
                      )}
                      {entry.has_password && <span className="td-hosts-tag td-hosts-tag-pw">password set</span>}
                      {entry.panes.length > 0 && (
                        <span className="td-hosts-tag" title={entry.panes.join(', ')}>
                          used by {entry.panes.length} pane{entry.panes.length > 1 ? 's' : ''}
                        </span>
                      )}
                    </span>
                  </td>
                  <td>
                    <span className="td-hosts-actions">
                      <button
                        type="button"
                        className="td-btn td-btn-sm"
                        aria-label={`Edit ${entry.name}`}
                        disabled={saving}
                        onClick={() => startEdit(entry)}
                      >
                        Edit
                      </button>
                      <button
                        type="button"
                        className="td-btn td-btn-sm"
                        aria-label={`Delete ${entry.name}`}
                        disabled={saving}
                        onClick={() => startDelete(entry)}
                      >
                        Delete
                      </button>
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {confirming && (
          <div role="group" aria-label="Confirm delete" className="td-hosts-confirm">
            <span>
              Delete {confirming}? The dashboard stops collecting from it. Labels and done marks on its tasks are kept.
            </span>
            <span className="td-spacer" />
            <button type="button" className="td-btn td-btn-sm" disabled={saving} onClick={() => setConfirming(null)}>
              Cancel
            </button>
            <button
              type="button"
              className="td-btn td-btn-sm td-btn-primary"
              disabled={saving}
              onClick={() => void confirmDelete(confirming)}
            >
              Delete
            </button>
          </div>
        )}

        {form && (
          <form className="td-hosts-form" onSubmit={(event) => void save(event)} noValidate>
            <h3>{form.mode === 'add' ? 'Add a dashboard host' : `Edit ${form.values.name}`}</h3>
            <div className="td-hosts-grid">
              <div className="td-field td-hosts-wide">
                <label htmlFor="td-hosts-name">Name</label>
                <input
                  id="td-hosts-name"
                  className="td-mono"
                  list="td-hosts-ssh-config"
                  value={form.values.name}
                  readOnly={form.mode === 'edit'}
                  placeholder="gpu-box"
                  autoComplete="off"
                  spellCheck={false}
                  onChange={(event) => setValue('name', event.target.value)}
                />
                <small className="td-note">
                  {form.mode === 'edit'
                    ? 'A name cannot be changed, so the panes that use it keep working.'
                    : 'Panes use this name as their connection. Pick a host from ~/.ssh/config to use its settings.'}
                </small>
              </div>
              {inheritsFrom && (
                <p className="td-hosts-inherit td-hosts-wide">
                  ~/.ssh/config has Host {formName}. The fields you leave empty use its values.
                </p>
              )}
              <label className="td-field">
                <span>{inheritsFrom ? 'Host (optional)' : 'Host'}</span>
                <input
                  value={form.values.host}
                  placeholder="gpu.example.com"
                  autoComplete="off"
                  spellCheck={false}
                  onChange={(event) => setValue('host', event.target.value)}
                />
              </label>
              <label className="td-field">
                <span>User (optional)</span>
                <input value={form.values.user} placeholder="ubuntu" autoComplete="off" onChange={(event) => setValue('user', event.target.value)} />
              </label>
              <label className="td-field">
                <span>Port (optional)</span>
                <input value={form.values.port} placeholder="22" inputMode="numeric" onChange={(event) => setValue('port', event.target.value)} />
              </label>
              <label className="td-field">
                <span>Key file (optional)</span>
                <input
                  value={form.values.keyFile}
                  placeholder="~/.ssh/id_ed25519"
                  autoComplete="off"
                  spellCheck={false}
                  onChange={(event) => setValue('keyFile', event.target.value)}
                />
              </label>
              <label className="td-field td-hosts-wide">
                <span>Known hosts file (optional)</span>
                <input
                  value={form.values.knownHostsFile}
                  placeholder="~/.ssh/known_hosts"
                  autoComplete="off"
                  spellCheck={false}
                  onChange={(event) => setValue('knownHostsFile', event.target.value)}
                />
              </label>
              <div className="td-field td-hosts-wide">
                <label htmlFor="td-hosts-password">Password (optional)</label>
                <input
                  id="td-hosts-password"
                  type="password"
                  value={form.values.password}
                  autoComplete="new-password"
                  disabled={form.clearPassword}
                  placeholder={hasSavedPassword ? 'Saved; leave empty to keep it' : 'Not set'}
                  onChange={(event) => setValue('password', event.target.value)}
                />
                <small className="td-note">
                  Stored in config.yaml in plain text; a key file is safer. A saved password is never shown.
                </small>
              </div>
              {hasSavedPassword && (
                <label className="td-hosts-check td-hosts-wide">
                  <input
                    type="checkbox"
                    checked={form.clearPassword}
                    onChange={(event) =>
                      setForm({ ...form, clearPassword: event.target.checked, values: { ...form.values, password: '' } })
                    }
                  />
                  Remove saved password
                </label>
              )}
            </div>
            {form.error && (
              <div role="alert" className="td-alert td-alert-inline">
                {form.error}
              </div>
            )}
            <div className="td-modal-actions">
              <button type="button" className="td-btn" disabled={saving} onClick={() => setForm(null)}>
                Cancel
              </button>
              <button type="submit" className="td-btn td-btn-primary" disabled={saving}>
                {saving ? 'Saving…' : 'Save host'}
              </button>
            </div>
          </form>
        )}

        <datalist id="td-hosts-ssh-config">
          {sshConfigNames.map((name) => (
            <option key={name} value={name} />
          ))}
        </datalist>

        {/* The dialog's own actions are offered only over the list: while a form
            or a delete confirmation is open, its Cancel is the way back, and
            Escape or a click outside still closes the dialog. */}
        {!form && !confirming && (
          <div className="td-modal-actions">
            <button type="button" className="td-btn" disabled={saving} onClick={startAdd}>
              Add host
            </button>
            <span className="td-spacer" />
            <button type="button" className="td-btn" disabled={saving} onClick={onClose}>
              Close
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
