import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ConfirmDialog } from './ConfirmDialog'

const defaultProps = {
  isOpen: true,
  title: 'Delete workspace',
  message: 'Delete workspace "Dev"?',
  onConfirm: vi.fn(),
  onCancel: vi.fn(),
}

describe('ConfirmDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders nothing when closed', () => {
    render(<ConfirmDialog {...defaultProps} isOpen={false} />)

    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('renders the title and message as a modal dialog when open', () => {
    render(<ConfirmDialog {...defaultProps} />)

    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(dialog).toHaveAttribute('aria-label', 'Delete workspace')
    expect(screen.getByText('Delete workspace "Dev"?')).toBeDefined()
  })

  it('confirms only when the confirm button is pressed', () => {
    render(<ConfirmDialog {...defaultProps} />)

    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))

    expect(defaultProps.onConfirm).toHaveBeenCalledTimes(1)
    expect(defaultProps.onCancel).not.toHaveBeenCalled()
  })

  it('cancels when the cancel button is pressed, and never confirms', () => {
    render(<ConfirmDialog {...defaultProps} />)

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(defaultProps.onCancel).toHaveBeenCalledTimes(1)
    expect(defaultProps.onConfirm).not.toHaveBeenCalled()
  })

  it('uses custom button labels when given', () => {
    render(<ConfirmDialog {...defaultProps} confirmLabel="Remove it" cancelLabel="Keep it" />)

    expect(screen.getByRole('button', { name: 'Remove it' })).toBeDefined()
    expect(screen.getByRole('button', { name: 'Keep it' })).toBeDefined()
  })

  it('marks the confirm button as destructive only when asked', () => {
    const { rerender } = render(<ConfirmDialog {...defaultProps} />)
    expect(screen.getByRole('button', { name: 'Confirm' })).toHaveAttribute('data-tone', 'default')

    rerender(<ConfirmDialog {...defaultProps} isDestructive />)
    expect(screen.getByRole('button', { name: 'Confirm' })).toHaveAttribute('data-tone', 'danger')
  })

  it('cancels on Escape', () => {
    render(<ConfirmDialog {...defaultProps} />)

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(defaultProps.onCancel).toHaveBeenCalledTimes(1)
    expect(defaultProps.onConfirm).not.toHaveBeenCalled()
  })

  it('ignores Escape while closed, so it cannot cancel something that is not being asked', () => {
    render(<ConfirmDialog {...defaultProps} isOpen={false} />)

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(defaultProps.onCancel).not.toHaveBeenCalled()
  })

  it('cancels on a backdrop click but not on a click inside the dialog', () => {
    render(<ConfirmDialog {...defaultProps} />)

    fireEvent.click(screen.getByTestId('confirm-dialog-backdrop'))
    expect(defaultProps.onCancel).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByText('Delete workspace "Dev"?'))
    expect(defaultProps.onCancel).toHaveBeenCalledTimes(1)
  })

  it('focuses the confirm button when it opens, so the keyboard alone can answer', () => {
    render(<ConfirmDialog {...defaultProps} />)

    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Confirm' }))
  })
})
