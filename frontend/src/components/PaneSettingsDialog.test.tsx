import { describe, it, expect, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { PaneSettingsDialog } from './PaneSettingsDialog'

const defaultProps = {
  isOpen: true,
  pane: { id: 'main', type: 'local' as const, title: 'Main', shell: '/bin/zsh' },
  sshConnectionNames: ['prod'],
  saveError: null,
  isSaving: false,
  onSave: vi.fn().mockResolvedValue(undefined),
  onClose: vi.fn(),
  onAddSSHHost: vi.fn(),
  onDetectShell: vi.fn().mockResolvedValue('/bin/zsh'),
}

describe('PaneSettingsDialog', () => {
  it('does not render when closed', () => {
    render(<PaneSettingsDialog {...defaultProps} isOpen={false} />)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('closes on Escape when not saving', () => {
    const onClose = vi.fn()
    render(<PaneSettingsDialog {...defaultProps} onClose={onClose} />)

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(onClose).toHaveBeenCalled()
  })

  it('does not close on backdrop click while saving', () => {
    const onClose = vi.fn()
    render(<PaneSettingsDialog {...defaultProps} isSaving={true} onClose={onClose} />)

    fireEvent.click(screen.getByRole('dialog'))

    expect(onClose).not.toHaveBeenCalled()
  })

  it('keeps the dialog surface within the viewport and scrollable', () => {
    render(<PaneSettingsDialog {...defaultProps} />)

    const surface = screen.getByText('Pane Settings').parentElement
    expect(surface).toHaveStyle({
      maxWidth: 'calc(100vw - 32px)',
      maxHeight: 'calc(100vh - 32px)',
      overflowY: 'auto',
    })
  })
})
