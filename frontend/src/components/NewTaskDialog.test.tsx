import { act, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { NewTaskDialog } from './NewTaskDialog'
import type { TaskHost } from '../schemas'

const hosts: TaskHost[] = [
  { name: '', status: 'ok' },
  { name: 'build-box', status: 'ok' },
  { name: 'gpu-box', status: 'error', error: 'i/o timeout' },
]

const launched = {
  id: 'ssh:build-box:claude:0f0e0d0c-0b0a-4908-8706-050403020100',
  session_id: '0f0e0d0c-0b0a-4908-8706-050403020100',
  tmux_session: 'task-0f0e0d0c',
}

function renderDialog(onLaunch = vi.fn().mockResolvedValue({ ok: true, launched }), onLaunched = vi.fn(), onClose = vi.fn()) {
  render(<NewTaskDialog isOpen hosts={hosts} onLaunch={onLaunch} onLaunched={onLaunched} onClose={onClose} />)
  return { onLaunch, onLaunched, onClose }
}

function fill(fields: { cwd?: string; prompt?: string; labels?: string; host?: string }) {
  if (fields.host !== undefined) fireEvent.change(screen.getByLabelText('Host'), { target: { value: fields.host } })
  if (fields.cwd !== undefined) fireEvent.change(screen.getByLabelText('Working directory'), { target: { value: fields.cwd } })
  if (fields.labels !== undefined) fireEvent.change(screen.getByLabelText('Labels'), { target: { value: fields.labels } })
  if (fields.prompt !== undefined) fireEvent.change(screen.getByLabelText('First instruction'), { target: { value: fields.prompt } })
}

describe('NewTaskDialog', () => {
  it('renders nothing while closed', () => {
    render(<NewTaskDialog isOpen={false} hosts={hosts} onLaunch={vi.fn()} onLaunched={vi.fn()} onClose={vi.fn()} />)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('offers every host, the panemux host first, and claude as the only agent', () => {
    renderDialog()
    const hostOptions = Array.from((screen.getByLabelText('Host') as HTMLSelectElement).options).map((o) => [o.value, o.text])
    expect(hostOptions).toEqual([['', 'Local'], ['build-box', 'build-box'], ['gpu-box', 'gpu-box (unreachable)']])
    const agentOptions = Array.from((screen.getByLabelText('Agent') as HTMLSelectElement).options).map((o) => o.value)
    expect(agentOptions).toEqual(['claude'])
  })

  it('starts the task with what was entered and hands the result on', async () => {
    const { onLaunch, onLaunched } = renderDialog()
    fill({ host: 'build-box', cwd: ' /remote/home/demo/payment ', labels: 'payment, sprint-42', prompt: '--fix the flaky test' })

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Start' }))
    })

    expect(onLaunch).toHaveBeenCalledWith({
      host: 'build-box', cwd: '/remote/home/demo/payment', prompt: '--fix the flaky test', labels: ['payment', 'sprint-42'],
    })
    expect(onLaunched).toHaveBeenCalledWith(launched, 'build-box')
  })

  it.each([
    [{ cwd: '', prompt: 'go' }, 'Enter the working directory.'],
    [{ cwd: 'relative/dir', prompt: 'go' }, 'The working directory must be an absolute path.'],
    [{ cwd: '/w', prompt: '   ' }, 'Enter the first instruction.'],
  ])('checks the form before asking the server: %o', async (fields, message) => {
    const { onLaunch } = renderDialog()
    fill(fields)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Start' }))
    })

    expect(screen.getByRole('alert').textContent).toBe(message)
    expect(onLaunch).not.toHaveBeenCalled()
  })

  it('shows why the server refused, keeps the form, and stays open', async () => {
    const { onLaunched, onClose } = renderDialog(vi.fn().mockResolvedValue({ ok: false, error: 'claude was not found on the host' }))
    fill({ cwd: '/w', prompt: 'go' })

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Start' }))
    })

    expect(screen.getByRole('alert').textContent).toBe('Could not start: claude was not found on the host')
    expect((screen.getByLabelText('First instruction') as HTMLTextAreaElement).value).toBe('go')
    expect(onLaunched).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
  })

  it('cannot be submitted or dismissed while a launch is in flight', async () => {
    let finish: (value: unknown) => void = () => {}
    const onLaunch = vi.fn().mockImplementation(() => new Promise((resolve) => { finish = resolve }))
    const { onClose } = renderDialog(onLaunch)
    fill({ cwd: '/w', prompt: 'go' })

    act(() => {
      fireEvent.click(screen.getByRole('button', { name: 'Start' }))
    })
    expect(screen.getByRole('button', { name: 'Starting…' })).toHaveProperty('disabled', true)
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveProperty('disabled', true)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()

    await act(async () => {
      finish({ ok: true, launched })
    })
  })

  it('closes on Cancel and on Escape', () => {
    const { onClose } = renderDialog()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(2)
  })
})
