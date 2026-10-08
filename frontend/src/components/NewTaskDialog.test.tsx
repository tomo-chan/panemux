import { act, fireEvent, render, screen, within } from '@testing-library/react'
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

  it('offers every host, the panemux host first, and claude or codex', () => {
    renderDialog()
    const hostOptions = Array.from((screen.getByLabelText('Host') as HTMLSelectElement).options).map((o) => [o.value, o.text])
    expect(hostOptions).toEqual([['', 'Local'], ['build-box', 'build-box'], ['gpu-box', 'gpu-box (unreachable)']])
    const agentOptions = Array.from((screen.getByLabelText('Agent') as HTMLSelectElement).options).map((o) => o.value)
    expect(agentOptions).toEqual(['claude', 'codex'])
  })

  it('starts the task with what was entered and hands the result on', async () => {
    const { onLaunch, onLaunched } = renderDialog()
    fill({ host: 'build-box', cwd: ' /remote/home/demo/payment ', labels: 'payment, sprint-42', prompt: '--fix the flaky test' })

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Start' }))
    })

    expect(onLaunch).toHaveBeenCalledWith({
      host: 'build-box', agent: 'claude', cwd: '/remote/home/demo/payment', prompt: '--fix the flaky test',
      labels: ['payment', 'sprint-42'],
    })
    expect(onLaunched).toHaveBeenCalledWith(launched, 'build-box', 'claude')
  })

  it('starts codex, saying when its labels are recorded', async () => {
    const codexLaunched = { tmux_session: 'task-0a1b2c3d', pending_labels: ['payment'] }
    const { onLaunch, onLaunched } = renderDialog(vi.fn().mockResolvedValue({ ok: true, launched: codexLaunched }))
    expect(screen.queryByText(/recorded once codex/)).toBeNull()
    fireEvent.change(screen.getByLabelText('Agent'), { target: { value: 'codex' } })
    expect(screen.getByText(/recorded once codex has started its session/)).toBeTruthy()
    fill({ cwd: '/workspace/user/api', labels: 'payment', prompt: 'go' })

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Start' }))
    })

    expect(onLaunch).toHaveBeenCalledWith({ host: '', agent: 'codex', cwd: '/workspace/user/api', prompt: 'go', labels: ['payment'] })
    expect(onLaunched).toHaveBeenCalledWith(codexLaunched, '', 'codex')
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

// Labels used before (issue #310): the known labels as toggle tags under the
// Labels field, filtered by what is typed after the last comma.
describe('NewTaskDialog label suggestions', () => {
  const knownLabels = ['bug', 'Docs', 'docs', 'frontend', 'refactor', 'release-1.4', 'research']

  function renderWithLabels(onLaunch = vi.fn().mockResolvedValue({ ok: true, launched })) {
    render(
      <NewTaskDialog isOpen hosts={hosts} knownLabels={knownLabels} onLaunch={onLaunch} onLaunched={vi.fn()} onClose={vi.fn()} />,
    )
    return { onLaunch }
  }

  function tags(): [string, string | null][] {
    return within(screen.getByRole('group', { name: 'Used before:' }))
      .getAllByRole('button')
      .map((b) => [b.textContent ?? '', b.getAttribute('aria-pressed')])
  }

  const labelsInput = () => screen.getByLabelText('Labels') as HTMLInputElement

  it('offers nothing when no label was used before', () => {
    renderDialog()
    expect(screen.queryByRole('group', { name: 'Used before:' })).toBeNull()
  })

  it('offers every known label, in order, none entered', () => {
    renderWithLabels()
    expect(tags()).toEqual(knownLabels.map((l) => [`+ ${l}`, 'false']))
  })

  it('adds a label on a click and takes it out on another', () => {
    renderWithLabels()
    fireEvent.click(screen.getByRole('button', { name: 'bug' }))
    expect(labelsInput().value).toBe('bug, ')
    fireEvent.click(screen.getByRole('button', { name: 'frontend' }))
    expect(labelsInput().value).toBe('bug, frontend, ')
    expect(screen.getByRole('button', { name: 'bug' })).toHaveAttribute('aria-pressed', 'true')

    fireEvent.click(screen.getByRole('button', { name: 'bug' }))
    expect(labelsInput().value).toBe('frontend, ')
    expect(screen.getByRole('button', { name: 'bug' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('filters by what is typed after the last comma, ignoring case, and keeps the entered labels', () => {
    renderWithLabels()
    fill({ labels: 'bug, frontend, RE' })
    expect(tags()).toEqual([
      ['✓ bug', 'true'],
      ['✓ frontend', 'true'],
      ['+ refactor', 'false'],
      ['+ release-1.4', 'false'],
      ['+ research', 'false'],
    ])

    fireEvent.click(screen.getByRole('button', { name: 'research' }))
    expect(labelsInput().value).toBe('bug, frontend, research, ')
    expect(tags()).toHaveLength(knownLabels.length)

    fill({ labels: '' })
    expect(tags()).toEqual(knownLabels.map((l) => [`+ ${l}`, 'false']))
  })

  it('says when no label used before matches, and still starts with the new label', async () => {
    const { onLaunch } = renderWithLabels()
    fill({ cwd: '/workspace/user/project', labels: 'bug, payments', prompt: 'go' })
    expect(tags()).toEqual([['✓ bug', 'true']])
    expect(screen.getByRole('group', { name: 'Used before:' })).toHaveTextContent(
      'No label used before contains “payments”. It is added as a new label.',
    )

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Start' }))
    })
    expect(onLaunch).toHaveBeenCalledWith(expect.objectContaining({ labels: ['bug', 'payments'] }))
  })

  it('says nothing about a typed label that is a known one', () => {
    renderWithLabels()
    fill({ labels: 'bug' })
    expect(tags()).toEqual([['✓ bug', 'true']])
    expect(screen.getByRole('group', { name: 'Used before:' })).not.toHaveTextContent('No label used before')
  })
})
