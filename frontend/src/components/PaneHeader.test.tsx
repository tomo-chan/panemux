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
  editMode: false,
  onSplit: vi.fn(),
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
})
