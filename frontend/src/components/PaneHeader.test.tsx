import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { PaneHeader } from './PaneHeader'
import type { PaneConfig, DisplayConfig } from '../types'

const defaultDisplay: DisplayConfig = { show_header: true, show_status_bar: false }

const localPane: PaneConfig = { id: 'p1', type: 'local' }
const sshPane: PaneConfig = { id: 'p2', type: 'ssh', connection: 'myserver' }

const defaultProps = {
  pane: localPane,
  connected: true,
  displayConfig: defaultDisplay,
  isMaximized: false,
  onSplit: vi.fn(),
  onCreateDefaultPane: vi.fn(),
  onClose: vi.fn(),
  onMaximize: vi.fn(),
  onSettings: vi.fn(),
  onOpenVSCode: vi.fn(),
}

describe('PaneHeader VSCode button', () => {
  it('renders the VSCode button when connected', () => {
    render(<PaneHeader {...defaultProps} />)
    expect(screen.getByTitle('Open in VSCode')).toBeDefined()
  })

  it('does not render the VSCode button when not connected', () => {
    render(<PaneHeader {...defaultProps} connected={false} />)
    expect(screen.queryByTitle('Open in VSCode')).toBeNull()
  })

  it('calls onOpenVSCode when VSCode button is clicked', () => {
    const onOpenVSCode = vi.fn()
    render(<PaneHeader {...defaultProps} onOpenVSCode={onOpenVSCode} />)
    fireEvent.click(screen.getByTitle('Open in VSCode'))
    expect(onOpenVSCode).toHaveBeenCalledOnce()
  })

  it('renders VSCode button for SSH pane when connected', () => {
    render(<PaneHeader {...defaultProps} pane={sshPane} />)
    expect(screen.getByTitle('Open in VSCode')).toBeDefined()
  })

  it('does not render header when show_header is false', () => {
    render(<PaneHeader {...defaultProps} displayConfig={{ show_header: false, show_status_bar: false }} />)
    expect(screen.queryByTitle('Open in VSCode')).toBeNull()
  })

  it('shows a grabbing cursor while the pane is being dragged', () => {
    render(<PaneHeader {...defaultProps} isDragging moveHandleProps={{ onMouseDown: vi.fn() }} />)
    expect(screen.getByTitle('Drag to move pane')).toHaveStyle({ cursor: 'grabbing' })
  })

  it('shows hover affordance on header action buttons', () => {
    render(<PaneHeader {...defaultProps} />)

    const button = screen.getByTitle('Split horizontal')
    fireEvent.mouseEnter(button)

    expect(button).toHaveStyle({
      backgroundColor: 'rgba(255, 255, 255, 0.07)',
      boxShadow: 'inset 0 0 0 1px rgba(255, 255, 255, 0.06)',
    })
  })

  it('shows a pressed state while a header action button is held down', () => {
    render(<PaneHeader {...defaultProps} />)

    const button = screen.getByTitle('Split horizontal')
    fireEvent.mouseDown(button, { button: 0 })

    expect(button).toHaveStyle({
      backgroundColor: 'rgba(255, 255, 255, 0.12)',
      boxShadow: 'inset 0 1px 2px rgba(0, 0, 0, 0.45)',
      transform: 'translateY(1px)',
    })

    fireEvent.mouseUp(button)
    expect(button).toHaveStyle({ transform: 'translateY(0)' })
  })

  it('calls onCreateDefaultPane for the right-side button', () => {
    const onCreateDefaultPane = vi.fn()
    render(<PaneHeader {...defaultProps} onCreateDefaultPane={onCreateDefaultPane} />)

    fireEvent.click(screen.getByTitle('Add new pane to the right'))

    expect(onCreateDefaultPane).toHaveBeenCalledWith('right')
  })

  it('calls onCreateDefaultPane for the bottom-side button', () => {
    const onCreateDefaultPane = vi.fn()
    render(<PaneHeader {...defaultProps} onCreateDefaultPane={onCreateDefaultPane} />)

    fireEvent.click(screen.getByTitle('Add new pane below'))

    expect(onCreateDefaultPane).toHaveBeenCalledWith('bottom')
  })

  it('renders a PR number link when git info includes PR metadata', () => {
    render(
      <PaneHeader
        {...defaultProps}
        gitInfo={{
          is_git: true,
          repo: 'panemux',
          repo_url: 'https://github.com/example/panemux',
          branch: 'feature/pane-pr-link',
          pr_number: 123,
          pr_url: 'https://github.com/example/panemux/pull/123',
        }}
      />
    )

    const link = screen.getByRole('link', { name: '#123' })
    expect(link).toHaveAttribute('href', 'https://github.com/example/panemux/pull/123')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(link).toHaveStyle({ textDecoration: 'none' })
  })

  it('shows an underline on PR link hover', () => {
    render(
      <PaneHeader
        {...defaultProps}
        gitInfo={{
          is_git: true,
          repo: 'panemux',
          repo_url: 'https://github.com/example/panemux',
          branch: 'feature/pane-pr-link',
          pr_number: 123,
          pr_url: 'https://github.com/example/panemux/pull/123',
        }}
      />
    )

    const link = screen.getByRole('link', { name: '#123' })
    fireEvent.mouseEnter(link)

    expect(link).toHaveStyle({
      textDecoration: 'underline',
      textUnderlineOffset: '2px',
    })
  })

  it('renders the repo name as a link when repo_url is present', () => {
    render(
      <PaneHeader
        {...defaultProps}
        gitInfo={{
          is_git: true,
          repo: 'panemux',
          repo_url: 'https://github.com/example/panemux',
          branch: 'main',
        }}
      />
    )

    const link = screen.getByRole('link', { name: 'panemux' })
    expect(link).toHaveAttribute('href', 'https://github.com/example/panemux')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('shows repo name as plain text when repo_url is absent', () => {
    render(
      <PaneHeader
        {...defaultProps}
        gitInfo={{
          is_git: true,
          repo: 'panemux',
          branch: 'main',
        }}
      />
    )

    expect(screen.queryByRole('link', { name: 'panemux' })).toBeNull()
    expect(screen.getByText('panemux')).toBeInTheDocument()
  })

  it('shows an underline on repo link hover', () => {
    render(
      <PaneHeader
        {...defaultProps}
        gitInfo={{
          is_git: true,
          repo: 'panemux',
          repo_url: 'https://github.com/example/panemux',
          branch: 'main',
        }}
      />
    )

    const link = screen.getByRole('link', { name: 'panemux' })
    fireEvent.mouseEnter(link)

    expect(link).toHaveStyle({
      textDecoration: 'underline',
      textUnderlineOffset: '2px',
    })
  })
})
