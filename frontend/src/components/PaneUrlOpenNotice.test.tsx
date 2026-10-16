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

  // Issue #386: a localhost URL on the panemux host can be previewed.
  it('offers Preview, Open and Ignore when the URL can be previewed', () => {
    const onPreview = vi.fn()
    const onConfirm = vi.fn()
    render(
      <PaneUrlOpenNotice
        {...defaultProps}
        pendingUrl="http://localhost:5173/"
        canPreview
        onPreview={onPreview}
        onConfirm={onConfirm}
      />,
    )

    const buttons = screen.getAllByRole('button').map((b) => b.textContent)
    expect(buttons).toEqual(['Preview', 'Open', 'Ignore'])
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
    expect(onPreview).toHaveBeenCalledTimes(1)
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('offers Preview only for a URL that can be previewed', () => {
    const { rerender } = render(
      <PaneUrlOpenNotice {...defaultProps} pendingUrl="http://localhost:5173/" canPreview onPreview={vi.fn()} />,
    )
    expect(screen.getByRole('button', { name: 'Preview' })).toBeInTheDocument()

    rerender(<PaneUrlOpenNotice {...defaultProps} pendingUrl="https://example.com/" onPreview={vi.fn()} />)
    expect(screen.queryByRole('button', { name: 'Preview' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open' })).toBeInTheDocument()
  })

  it('says a clicked link asks, not the pane', () => {
    const { rerender } = render(<PaneUrlOpenNotice {...defaultProps} pendingUrl="https://example.com/" />)
    expect(screen.getByText('This pane wants to open')).toBeInTheDocument()

    rerender(<PaneUrlOpenNotice {...defaultProps} pendingUrl="https://example.com/" source="link" />)
    expect(screen.getByText('This link goes to')).toBeInTheDocument()
  })
})
