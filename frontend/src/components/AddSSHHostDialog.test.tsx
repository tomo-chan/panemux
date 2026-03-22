import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { AddSSHHostDialog } from './AddSSHHostDialog'
import type { SSHConfigHost } from '../schemas'

const defaultProps = {
  isOpen: true,
  isSaving: false,
  saveError: null,
  onSave: vi.fn(),
  onClose: vi.fn(),
}

describe('AddSSHHostDialog', () => {
  it('does not render when isOpen=false', () => {
    render(<AddSSHHostDialog {...defaultProps} isOpen={false} />)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('renders all fields when open', () => {
    render(<AddSSHHostDialog {...defaultProps} />)
    expect(screen.getByRole('dialog')).toBeDefined()
    expect(screen.getByLabelText('Name')).toBeDefined()
    expect(screen.getByLabelText('Hostname')).toBeDefined()
    expect(screen.getByLabelText('User')).toBeDefined()
    expect(screen.getByLabelText('Port')).toBeDefined()
    expect(screen.getByLabelText('Identity File')).toBeDefined()
  })

  it('validates required fields before submitting', async () => {
    render(<AddSSHHostDialog {...defaultProps} onSave={defaultProps.onSave} />)

    fireEvent.click(screen.getByRole('button', { name: /add/i }))

    await waitFor(() => {
      expect(screen.getByText(/name is required/i)).toBeDefined()
    })
    expect(defaultProps.onSave).not.toHaveBeenCalled()
  })

  it('validates hostname is required', async () => {
    render(<AddSSHHostDialog {...defaultProps} onSave={defaultProps.onSave} />)

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'myhost' } })
    fireEvent.click(screen.getByRole('button', { name: /add/i }))

    await waitFor(() => {
      expect(screen.getByText(/hostname is required/i)).toBeDefined()
    })
    expect(defaultProps.onSave).not.toHaveBeenCalled()
  })

  it('validates user is required', async () => {
    render(<AddSSHHostDialog {...defaultProps} onSave={defaultProps.onSave} />)

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'myhost' } })
    fireEvent.change(screen.getByLabelText('Hostname'), { target: { value: 'myhost.example.com' } })
    fireEvent.click(screen.getByRole('button', { name: /add/i }))

    await waitFor(() => {
      expect(screen.getByText(/user is required/i)).toBeDefined()
    })
    expect(defaultProps.onSave).not.toHaveBeenCalled()
  })

  it('validates port range', async () => {
    render(<AddSSHHostDialog {...defaultProps} onSave={defaultProps.onSave} />)

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'myhost' } })
    fireEvent.change(screen.getByLabelText('Hostname'), { target: { value: 'myhost.example.com' } })
    fireEvent.change(screen.getByLabelText('User'), { target: { value: 'ubuntu' } })
    fireEvent.change(screen.getByLabelText('Port'), { target: { value: '99999' } })
    fireEvent.click(screen.getByRole('button', { name: /add/i }))

    await waitFor(() => {
      expect(screen.getByText(/port must be between/i)).toBeDefined()
    })
    expect(defaultProps.onSave).not.toHaveBeenCalled()
  })

  it('calls onSave with correct data when valid', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<AddSSHHostDialog {...defaultProps} onSave={onSave} />)

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'staging' } })
    fireEvent.change(screen.getByLabelText('Hostname'), { target: { value: 'staging.example.com' } })
    fireEvent.change(screen.getByLabelText('User'), { target: { value: 'deploy' } })
    fireEvent.change(screen.getByLabelText('Port'), { target: { value: '2222' } })
    fireEvent.change(screen.getByLabelText('Identity File'), { target: { value: '~/.ssh/staging_rsa' } })
    fireEvent.click(screen.getByRole('button', { name: /add/i }))

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith<[SSHConfigHost]>({
        name: 'staging',
        hostname: 'staging.example.com',
        user: 'deploy',
        port: 2222,
        identity_file: '~/.ssh/staging_rsa',
      })
    })
  })

  it('calls onSave without port and identity_file when not provided', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<AddSSHHostDialog {...defaultProps} onSave={onSave} />)

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'simple' } })
    fireEvent.change(screen.getByLabelText('Hostname'), { target: { value: 'simple.example.com' } })
    fireEvent.change(screen.getByLabelText('User'), { target: { value: 'ubuntu' } })
    fireEvent.click(screen.getByRole('button', { name: /add/i }))

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith<[SSHConfigHost]>({
        name: 'simple',
        hostname: 'simple.example.com',
        user: 'ubuntu',
      })
    })
  })

  it('shows saveError', () => {
    render(<AddSSHHostDialog {...defaultProps} saveError="host already exists" />)
    expect(screen.getByText(/host already exists/i)).toBeDefined()
  })

  it('cancel calls onClose', () => {
    const onClose = vi.fn()
    render(<AddSSHHostDialog {...defaultProps} onClose={onClose} />)
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(onClose).toHaveBeenCalled()
  })

  it('resets form to empty on each open', () => {
    const { rerender } = render(<AddSSHHostDialog {...defaultProps} isOpen={false} />)

    // Open dialog
    rerender(<AddSSHHostDialog {...defaultProps} isOpen={true} />)
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'typed-name' } })

    // Close and re-open
    rerender(<AddSSHHostDialog {...defaultProps} isOpen={false} />)
    rerender(<AddSSHHostDialog {...defaultProps} isOpen={true} />)

    // Form should be reset
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('')
  })

  it('disables Save button while saving', () => {
    render(<AddSSHHostDialog {...defaultProps} isSaving={true} />)
    const saveBtn = screen.getByRole('button', { name: /saving/i })
    expect(saveBtn).toBeDefined()
    expect((saveBtn as HTMLButtonElement).disabled).toBe(true)
  })
})
