import { describe, it, expect, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
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
  onBrowseDirectories: vi.fn().mockResolvedValue({
    path: '/workspace/user',
    entries: [{ name: 'projects', path: '/workspace/user/projects', has_children: true }],
  }),
}

describe('PaneSettingsDialog', () => {
  it('does not render when closed', () => {
    render(<PaneSettingsDialog {...defaultProps} isOpen={false} />)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('does not render when pane is missing', () => {
    render(<PaneSettingsDialog {...defaultProps} pane={null} />)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('closes on Escape when not saving', () => {
    const onClose = vi.fn()
    render(<PaneSettingsDialog {...defaultProps} onClose={onClose} />)

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(onClose).toHaveBeenCalled()
  })

  it('does not close on Escape while saving', () => {
    const onClose = vi.fn()
    render(<PaneSettingsDialog {...defaultProps} isSaving={true} onClose={onClose} />)

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(onClose).not.toHaveBeenCalled()
  })

  it('closes on backdrop click when not saving', () => {
    const onClose = vi.fn()
    render(<PaneSettingsDialog {...defaultProps} onClose={onClose} />)

    fireEvent.click(screen.getByRole('dialog'))

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

  it('loads directories when Browse is clicked and updates cwd when one is selected', async () => {
    const onBrowseDirectories = vi.fn().mockResolvedValue({
      path: '/workspace/user',
      entries: [{ name: 'projects', path: '/workspace/user/projects', has_children: false }],
    })
    render(<PaneSettingsDialog {...defaultProps} onBrowseDirectories={onBrowseDirectories} />)

    fireEvent.click(screen.getByRole('button', { name: 'Browse' }))

    await waitFor(() => expect(onBrowseDirectories).toHaveBeenCalled())
    expect(screen.getByRole('dialog', { name: 'Directory browser' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '/workspace/user/projects' }))
    fireEvent.click(screen.getByRole('button', { name: 'Choose Directory' }))

    expect(screen.getByLabelText('Working Directory')).toHaveValue('/workspace/user/projects')
  })

  it('opens the browser dialog even when the current directory has no child directories', async () => {
    const onBrowseDirectories = vi.fn().mockResolvedValue({
      path: '/tmp/sample-project',
      entries: [],
    })
    render(
      <PaneSettingsDialog
        {...defaultProps}
        pane={{ id: 'main', type: 'local', title: 'Main', shell: '/bin/zsh', cwd: '/tmp/sample-project' }}
        onBrowseDirectories={onBrowseDirectories}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Browse' }))

    expect(screen.getByRole('dialog', { name: 'Directory browser' })).toBeInTheDocument()
    await screen.findByText('No directories found.')
  })

  it('disables Browse for SSH panes until a connection is selected', () => {
    render(<PaneSettingsDialog
      {...defaultProps}
      pane={{ id: 'remote', type: 'ssh', title: 'Remote' }}
      sshConnectionNames={['prod']}
    />)

    expect(screen.getByRole('button', { name: 'Browse' })).toBeDisabled()
    expect(screen.getByText('Select an SSH connection to browse remote directories.')).toBeInTheDocument()
  })

  it('shows hidden directories after toggling the visibility option', async () => {
    const onBrowseDirectories = vi.fn()
      .mockResolvedValueOnce({
        path: '/workspace/user',
        entries: [{ name: 'visible', path: '/workspace/user/visible', has_children: false }],
      })
      .mockResolvedValueOnce({
        path: '/workspace/user',
        entries: [{ name: '.config', path: '/workspace/user/.config', has_children: true }],
      })
    render(<PaneSettingsDialog {...defaultProps} onBrowseDirectories={onBrowseDirectories} />)

    fireEvent.click(screen.getByRole('button', { name: 'Browse' }))
    await screen.findByText('visible')

    fireEvent.click(screen.getByLabelText('Show hidden directories'))

    await screen.findByText('.config')
    expect(onBrowseDirectories).toHaveBeenLastCalledWith('local', undefined, '/workspace/user', true)
  })

  it('can navigate to the parent directory from the browser dialog', async () => {
    const onBrowseDirectories = vi.fn()
      .mockResolvedValueOnce({
        path: '/workspace/user/projects',
        entries: [{ name: 'app', path: '/workspace/user/projects/app', has_children: false }],
      })
      .mockResolvedValueOnce({
        path: '/workspace/user',
        entries: [{ name: 'projects', path: '/workspace/user/projects', has_children: true }],
      })

    render(
      <PaneSettingsDialog
        {...defaultProps}
        pane={{ id: 'main', type: 'local', title: 'Main', shell: '/bin/zsh', cwd: '/workspace/user/projects' }}
        onBrowseDirectories={onBrowseDirectories}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Browse' }))
    await screen.findByText('app')
    fireEvent.click(screen.getByRole('button', { name: 'Go to parent directory' }))

    await screen.findByText('projects')
    expect(onBrowseDirectories).toHaveBeenLastCalledWith('local', undefined, '/workspace/user', false)
  })

  it('selects and expands a directory row on single click', async () => {
    const onBrowseDirectories = vi.fn()
      .mockResolvedValueOnce({
        path: '/workspace/user',
        entries: [{ name: 'projects', path: '/workspace/user/projects', has_children: true }],
      })
      .mockResolvedValueOnce({
        path: '/workspace/user/projects',
        entries: [{ name: 'sample-app', path: '/workspace/user/projects/sample-app', has_children: false }],
      })
    render(<PaneSettingsDialog {...defaultProps} onBrowseDirectories={onBrowseDirectories} />)

    fireEvent.click(screen.getByRole('button', { name: 'Browse' }))
    await screen.findByText('projects')

    fireEvent.click(screen.getByRole('button', { name: '/workspace/user/projects' }))

    await screen.findByText('sample-app')
  })
})
