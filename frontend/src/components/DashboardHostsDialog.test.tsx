import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { DashboardHostsDialog } from './DashboardHostsDialog'
import type { SSHConnectionsState } from '../hooks/useSSHConnections'
import type { SSHConnectionEntry } from '../schemas'

const buildBox: SSHConnectionEntry = {
  name: 'build-box',
  host: 'build.invalid',
  user: 'demo',
  port: 2222,
  key_file: '/remote/home/demo/.ssh/id_ed25519',
  known_hosts_file: '/remote/home/demo/.ssh/known_hosts',
  has_password: false,
  in_ssh_config: false,
  panes: ['dev-build', 'dev-logs'],
}
const gpuBox: SSHConnectionEntry = { name: 'gpu-box', has_password: false, in_ssh_config: true, panes: [] }
const legacy: SSHConnectionEntry = {
  name: 'legacy-vm', host: '10.0.0.24', user: 'admin', has_password: true, in_ssh_config: false, panes: [],
}

function makeState(overrides: Partial<SSHConnectionsState> = {}): SSHConnectionsState {
  return {
    connections: [buildBox, gpuBox, legacy],
    sshConfigNames: ['bastion', 'gpu-box'],
    loading: false,
    error: null,
    load: vi.fn().mockResolvedValue(undefined),
    create: vi.fn().mockResolvedValue(null),
    update: vi.fn().mockResolvedValue(null),
    remove: vi.fn().mockResolvedValue(null),
    ...overrides,
  }
}

function renderDialog(state = makeState(), onChanged = vi.fn(), onClose = vi.fn()) {
  render(<DashboardHostsDialog isOpen state={state} onChanged={onChanged} onClose={onClose} />)
  return { state, onChanged, onClose }
}

function row(name: string) {
  return screen.getByRole('row', { name: new RegExp(`^${name}`) })
}

function fill(fields: Record<string, string>) {
  for (const [label, value] of Object.entries(fields)) {
    fireEvent.change(screen.getByLabelText(label), { target: { value } })
  }
}

async function click(name: string | RegExp) {
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name }))
  })
}

describe('DashboardHostsDialog', () => {
  it('renders nothing while closed and loads when opened', () => {
    const state = makeState()
    const { rerender } = render(
      <DashboardHostsDialog isOpen={false} state={state} onChanged={vi.fn()} onClose={vi.fn()} />,
    )
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(state.load).not.toHaveBeenCalled()

    rerender(<DashboardHostsDialog isOpen state={state} onChanged={vi.fn()} onClose={vi.fn()} />)
    expect(screen.getByRole('dialog', { name: /Dashboard hosts/ })).toBeTruthy()
    expect(state.load).toHaveBeenCalledTimes(1)
  })

  it('says what it edits and how it differs from ~/.ssh/config', () => {
    renderDialog()
    const dialog = screen.getByRole('dialog')
    expect(dialog.textContent).toContain('ssh_connections in config.yaml')
    expect(dialog.textContent).toContain('only in ~/.ssh/config')
  })

  it('lists each entry with where it connects and what it carries, never a password', () => {
    renderDialog()
    const build = row('build-box')
    expect(build.textContent).toContain('demo@build.invalid:2222')
    expect(build.textContent).toContain('key file')
    expect(build.textContent).toContain('used by 2 panes')

    const gpu = row('gpu-box')
    expect(gpu.textContent).toContain('from ~/.ssh/config')

    expect(row('legacy-vm').textContent).toContain('password set')
  })

  it('shows an empty state', () => {
    renderDialog(makeState({ connections: [] }))
    expect(screen.getByText(/No dashboard hosts yet/)).toBeTruthy()
  })

  it('shows a load failure', () => {
    renderDialog(makeState({ connections: null, error: 'boom' }))
    expect(screen.getByRole('alert').textContent).toContain('Could not load the hosts: boom')
  })

  it('adds a host with what was entered, leaving out empty fields', async () => {
    const { state, onChanged } = renderDialog()
    await click('Add host')
    fill({ Name: ' new-box ', Host: ' new.invalid ', 'User (optional)': 'demo', 'Port (optional)': '2200', 'Password (optional)': 'pw' })

    await click('Save host')

    expect(state.create).toHaveBeenCalledWith({
      name: 'new-box', host: 'new.invalid', user: 'demo', port: 2200, password: 'pw',
    })
    expect(onChanged).toHaveBeenCalled()
    expect(screen.getByRole('status').textContent).toContain('Added new-box')
    expect(screen.queryByLabelText('Name')).toBeNull()
  })

  it('adds just a name for a ~/.ssh/config host', async () => {
    const { state } = renderDialog()
    await click('Add host')
    fill({ Name: 'bastion' })

    expect(screen.getByText(/~\/\.ssh\/config has Host bastion/)).toBeTruthy()
    expect(screen.getByLabelText('Host (optional)')).toBeTruthy()
    await click('Save host')

    expect(state.create).toHaveBeenCalledWith({ name: 'bastion' })
  })

  it.each([
    [{ Name: '' }, 'Enter a name.'],
    [{ Name: 'a b', Host: 'h' }, 'A name can contain only letters'],
    [{ Name: 'x' }, 'Enter a host. ~/.ssh/config has no Host block named "x"'],
    [{ Name: 'x', Host: 'h', 'Port (optional)': '0' }, 'Port must be a whole number from 1 to 65535.'],
    [{ Name: 'x', Host: 'h', 'Port (optional)': '22.5' }, 'Port must be a whole number from 1 to 65535.'],
    [{ Name: 'x', Host: 'h', 'Port (optional)': '0x16' }, 'Port must be a whole number from 1 to 65535.'],
    [{ Name: 'x', Host: 'h', 'Port (optional)': '1e3' }, 'Port must be a whole number from 1 to 65535.'],
    [{ Name: 'build-box', Host: 'h' }, 'A dashboard host named "build-box" already exists.'],
  ])('refuses %j before sending it', async (fields, message) => {
    const { state } = renderDialog()
    await click('Add host')
    fill(fields)

    await click('Save host')

    expect(state.create).not.toHaveBeenCalled()
    expect(screen.getByRole('alert').textContent).toContain(message)
  })

  it('shows the server refusal and keeps the form', async () => {
    const state = makeState({ create: vi.fn().mockResolvedValue('failed to save ssh_connections') })
    const { onChanged } = renderDialog(state)
    await click('Add host')
    fill({ Name: 'new-box', Host: 'new.invalid' })

    await click('Save host')

    expect(screen.getByRole('alert').textContent).toContain('failed to save ssh_connections')
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('new-box')
    expect(onChanged).not.toHaveBeenCalled()
  })

  it('edits an entry with its name fixed, keeping a saved password unless asked', async () => {
    const { state } = renderDialog()
    await click('Edit legacy-vm')

    const name = screen.getByLabelText('Name') as HTMLInputElement
    expect(name.value).toBe('legacy-vm')
    expect(name.readOnly).toBe(true)
    const password = screen.getByLabelText('Password (optional)') as HTMLInputElement
    expect(password.value).toBe('')
    expect(password.placeholder).toContain('leave empty to keep it')

    fill({ Host: '10.0.0.25' })
    await click('Save host')

    expect(state.update).toHaveBeenCalledWith('legacy-vm', { host: '10.0.0.25', user: 'admin' })
    expect(screen.getByRole('status').textContent).toContain('Saved legacy-vm')
  })

  // PUT replaces every field, so a field the form failed to fill in would be
  // deleted by a save that never touched it.
  it('keeps every field of an entry it edits', async () => {
    const { state } = renderDialog()
    await click('Edit build-box')

    expect((screen.getByLabelText('Port (optional)') as HTMLInputElement).value).toBe('2222')
    fill({ 'User (optional)': 'deploy' })
    await click('Save host')

    expect(state.update).toHaveBeenCalledWith('build-box', {
      host: 'build.invalid',
      user: 'deploy',
      port: 2222,
      key_file: '/remote/home/demo/.ssh/id_ed25519',
      known_hosts_file: '/remote/home/demo/.ssh/known_hosts',
    })
  })

  it('lists an entry whose port config.yaml has out of range, so it can be fixed', async () => {
    const { state } = renderDialog(makeState({ connections: [{ ...legacy, port: 70000 }] }))
    expect(row('legacy-vm').textContent).toContain('10.0.0.24:70000')

    await click('Edit legacy-vm')
    fill({ 'Port (optional)': '2200' })
    await click('Save host')

    expect(state.update).toHaveBeenCalledWith('legacy-vm', { host: '10.0.0.24', user: 'admin', port: 2200 })
  })

  it('removes a saved password when asked', async () => {
    const { state } = renderDialog()
    await click('Edit legacy-vm')

    fireEvent.click(screen.getByLabelText('Remove saved password'))
    expect((screen.getByLabelText('Password (optional)') as HTMLInputElement).disabled).toBe(true)
    await click('Save host')

    expect(state.update).toHaveBeenCalledWith('legacy-vm', { host: '10.0.0.24', user: 'admin', clear_password: true })
  })

  it('offers no password removal for an entry without one', async () => {
    renderDialog()
    await click('Edit build-box')
    expect(screen.queryByLabelText('Remove saved password')).toBeNull()
  })

  it('refuses to delete an entry panes depend on, naming them', async () => {
    const { state } = renderDialog()
    await click('Delete build-box')

    expect(state.remove).not.toHaveBeenCalled()
    expect(screen.getByRole('alert').textContent).toContain('used by pane dev-build, dev-logs')
  })

  it('deletes after confirming', async () => {
    const { state, onChanged } = renderDialog()
    await click('Delete legacy-vm')
    expect(state.remove).not.toHaveBeenCalled()

    const confirm = screen.getByRole('group', { name: 'Confirm delete' })
    await act(async () => {
      fireEvent.click(within(confirm).getByRole('button', { name: 'Delete' }))
    })

    expect(state.remove).toHaveBeenCalledWith('legacy-vm')
    expect(onChanged).toHaveBeenCalled()
    expect(screen.getByRole('status').textContent).toContain('Deleted legacy-vm')
  })

  it('shows a delete the server refused', async () => {
    const state = makeState({ remove: vi.fn().mockResolvedValue('failed to save ssh_connections') })
    renderDialog(state)
    await click('Delete legacy-vm')
    const confirm = screen.getByRole('group', { name: 'Confirm delete' })
    await act(async () => {
      fireEvent.click(within(confirm).getByRole('button', { name: 'Delete' }))
    })

    expect(screen.getByRole('alert').textContent).toContain('Could not delete legacy-vm: failed to save ssh_connections')
  })

  it('cancelling the confirmation deletes nothing', async () => {
    const { state } = renderDialog()
    await click('Delete legacy-vm')
    const confirm = screen.getByRole('group', { name: 'Confirm delete' })
    fireEvent.click(within(confirm).getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('group', { name: 'Confirm delete' })).toBeNull()
    expect(state.remove).not.toHaveBeenCalled()
  })

  // While a form or a delete confirmation is open, its own buttons are the
  // only ones offered: the dialog's Add host and Close come back with the list.
  it('shows only the form buttons while a host is being added or edited', async () => {
    renderDialog()
    await click('Add host')

    expect(screen.queryByRole('button', { name: 'Add host' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Close' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Save host' })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.getByRole('button', { name: 'Add host' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Close' })).toBeTruthy()

    await click('Edit legacy-vm')
    expect(screen.queryByRole('button', { name: 'Add host' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Close' })).toBeNull()
  })

  it('shows only the confirmation buttons while a delete is being confirmed', async () => {
    renderDialog()
    await click('Delete legacy-vm')

    expect(screen.queryByRole('button', { name: 'Add host' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Close' })).toBeNull()

    const confirm = screen.getByRole('group', { name: 'Confirm delete' })
    fireEvent.click(within(confirm).getByRole('button', { name: 'Cancel' }))
    expect(screen.getByRole('button', { name: 'Add host' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Close' })).toBeTruthy()
  })

  it('closes from Close, from the backdrop and on Escape', () => {
    const { onClose } = renderDialog()
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    fireEvent.click(screen.getByRole('dialog'))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(3)
  })
})
