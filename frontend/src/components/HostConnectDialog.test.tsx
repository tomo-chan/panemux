import { describe, it, expect, vi } from 'vitest'
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react'
import { HostConnectDialog } from './HostConnectDialog'
import type { HostConnectDialogProps } from './HostConnectDialog'

function props(overrides: Partial<HostConnectDialogProps> = {}): HostConnectDialogProps {
  return {
    host: 'gpu-box',
    sessionName: vi.fn().mockResolvedValue({ ok: true, launched: { tmux_session: 'gpu-box-0123abcd' } }),
    onCancel: vi.fn(),
    onOpen: vi.fn(),
    onTypeIn: vi.fn(),
    ...overrides,
  }
}

describe('HostConnectDialog (issue #314)', () => {
  it('is a modal dialog named for the host, with ssh chosen and Type in pane focused', () => {
    const p = props()
    render(<HostConnectDialog {...p} />)

    const dialog = screen.getByRole('dialog', { name: 'Open a terminal on gpu-box' })
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByRole('radio', { name: /^ssh\b/ })).toBeChecked()
    expect(screen.getByRole('radio', { name: /^ssh_tmux\b/ })).not.toBeChecked()
    expect(screen.queryByText(/tmux session:/)).toBeNull()
    expect(screen.getByRole('button', { name: 'Type in pane: gpu-box' })).toHaveFocus()
    expect(screen.getByRole('button', { name: 'Open: gpu-box' })).toBeEnabled()
    expect(p.sessionName).not.toHaveBeenCalled()
  })

  it('opens an ssh terminal with no tmux session, from either button', () => {
    const p = props()
    render(<HostConnectDialog {...p} />)

    fireEvent.click(screen.getByRole('button', { name: 'Type in pane: gpu-box' }))
    fireEvent.click(screen.getByRole('button', { name: 'Open: gpu-box' }))

    expect(p.onTypeIn).toHaveBeenCalledWith('ssh', undefined)
    expect(p.onOpen).toHaveBeenCalledWith('ssh', undefined)
  })

  it('asks the server for the ssh_tmux session name once, shows it, and uses it', async () => {
    let answer!: (value: Awaited<ReturnType<HostConnectDialogProps['sessionName']>>) => void
    const sessionName = vi.fn(() => new Promise<Awaited<ReturnType<HostConnectDialogProps['sessionName']>>>((r) => { answer = r }))
    const p = props({ sessionName })
    render(<HostConnectDialog {...p} />)

    fireEvent.click(screen.getByRole('radio', { name: /^ssh_tmux\b/ }))
    expect(sessionName).toHaveBeenCalledWith('gpu-box')
    expect(screen.getByText(/tmux session:/)).toHaveTextContent('tmux session: …')
    // Neither button can open an ssh_tmux terminal before the name is known.
    expect(screen.getByRole('button', { name: 'Type in pane: gpu-box' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Open: gpu-box' })).toBeDisabled()

    await act(async () => answer({ ok: true, launched: { tmux_session: 'gpu-box-0123abcd' } }))
    expect(screen.getByText(/tmux session:/)).toHaveTextContent('tmux session: gpu-box-0123abcd')

    // Back to ssh and again: the name is kept, not asked for again.
    fireEvent.click(screen.getByRole('radio', { name: /^ssh\b/ }))
    expect(screen.queryByText(/tmux session:/)).toBeNull()
    fireEvent.click(screen.getByRole('radio', { name: /^ssh_tmux\b/ }))
    expect(sessionName).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: 'Type in pane: gpu-box' }))
    fireEvent.click(screen.getByRole('button', { name: 'Open: gpu-box' }))
    expect(p.onTypeIn).toHaveBeenCalledWith('ssh_tmux', 'gpu-box-0123abcd')
    expect(p.onOpen).toHaveBeenCalledWith('ssh_tmux', 'gpu-box-0123abcd')
  })

  it('says why the name could not be had, keeps ssh_tmux closed, and still opens ssh', async () => {
    const p = props({ sessionName: vi.fn().mockResolvedValue({ ok: false, error: 'connection "gpu-box" is not in ssh_connections' }) })
    render(<HostConnectDialog {...p} />)

    fireEvent.click(screen.getByRole('radio', { name: /^ssh_tmux\b/ }))
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('Could not name the tmux session: connection "gpu-box" is not in ssh_connections'),
    )
    expect(screen.getByRole('button', { name: 'Type in pane: gpu-box' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Open: gpu-box' })).toBeDisabled()

    fireEvent.click(screen.getByRole('radio', { name: /^ssh\b/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Open: gpu-box' }))
    expect(p.onOpen).toHaveBeenCalledWith('ssh', undefined)
  })

  it('cancels from Cancel and from Escape', () => {
    const p = props()
    render(<HostConnectDialog {...p} />)

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    fireEvent.keyDown(window, { key: 'Escape' })

    expect(p.onCancel).toHaveBeenCalledTimes(2)
  })

  it('keeps Tab inside the dialog', () => {
    render(<HostConnectDialog {...props()} />)
    const typeIn = screen.getByRole('button', { name: 'Type in pane: gpu-box' })
    expect(typeIn).toHaveFocus()

    fireEvent.keyDown(window, { key: 'Tab' })

    expect(screen.getByRole('dialog').contains(document.activeElement)).toBe(true)
    expect(typeIn).not.toHaveFocus()
  })

  it('cancels on a pointer press outside it, and not on one inside', () => {
    const p = props()
    render(
      <>
        <button type="button">elsewhere</button>
        <HostConnectDialog {...p} />
      </>,
    )

    fireEvent.pointerDown(screen.getByRole('radio', { name: /^ssh_tmux\b/ }))
    expect(p.onCancel).not.toHaveBeenCalled()
    fireEvent.pointerDown(screen.getByRole('button', { name: 'elsewhere' }))
    expect(p.onCancel).toHaveBeenCalledTimes(1)
  })
})
