import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { PaneUrlOpenNotice } from './PaneUrlOpenNotice'

const noop = vi.fn()

const defaultProps = {
  pendingUrl: null,
  error: null,
  onConfirm: noop,
  onDismiss: noop,
  onDismissError: noop,
}

describe('PaneUrlOpenNotice', () => {
  it('renders nothing when there is neither a request nor an error', () => {
    const { container } = render(<PaneUrlOpenNotice {...defaultProps} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('asks before opening a URL a pane requested', () => {
    const onConfirm = vi.fn()
    render(
      <PaneUrlOpenNotice
        {...defaultProps}
        pendingUrl="https://example.com/device?code=ABCD"
        onConfirm={onConfirm}
      />,
    )

    expect(screen.getByText('https://example.com/device?code=ABCD')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('lets the request be ignored', () => {
    const onDismiss = vi.fn()
    render(<PaneUrlOpenNotice {...defaultProps} pendingUrl="https://example.com/device" onDismiss={onDismiss} />)

    fireEvent.click(screen.getByRole('button', { name: 'Ignore' }))

    expect(onDismiss).toHaveBeenCalledTimes(1)
  })

  it('shows a port-forwarding failure and lets it be dismissed', () => {
    const onDismissError = vi.fn()
    render(
      <PaneUrlOpenNotice
        {...defaultProps}
        error="loopback port unavailable: cannot bind 127.0.0.1:8085"
        onDismissError={onDismissError}
      />,
    )

    expect(screen.getByText(/cannot bind 127\.0\.0\.1:8085/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))

    expect(onDismissError).toHaveBeenCalledTimes(1)
  })

  it('prefers the pending request when an older error is still showing', () => {
    render(
      <PaneUrlOpenNotice
        {...defaultProps}
        pendingUrl="https://example.com/device"
        error="loopback port unavailable"
      />,
    )

    expect(screen.getByRole('button', { name: 'Open' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Dismiss' })).not.toBeInTheDocument()
  })

  it('keeps the full URL available when it is too long to show', () => {
    const longUrl = `https://example.com/auth?redirect_uri=${'x'.repeat(300)}`
    render(<PaneUrlOpenNotice {...defaultProps} pendingUrl={longUrl} />)

    expect(screen.getByTitle(longUrl)).toBeInTheDocument()
  })
})
