import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { CommandHistoryPanel } from './CommandHistoryPanel'

describe('CommandHistoryPanel', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('does not render when closed', () => {
    render(<CommandHistoryPanel isOpen={false} token="tok" onClose={vi.fn()} />)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('fetches history with the bearer token on open', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: true, json: async () => ({ entries: [] }) } as Response)

    render(<CommandHistoryPanel isOpen token="sekret" onClose={vi.fn()} />)

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith('/api/board/command/history', {
        headers: { Authorization: 'Bearer sekret' },
      })
    })
  })

  it('shows the empty state when there are no entries', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: true, json: async () => ({ entries: [] }) } as Response)

    render(<CommandHistoryPanel isOpen token="tok" onClose={vi.fn()} />)

    expect(await screen.findByText('No history yet.')).toBeDefined()
  })

  it('renders each history entry', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({
        entries: [
          { at: '2026-08-10T12:00:00Z', raw: { type: 'result', result: 'done' } },
        ],
      }),
    } as Response)

    render(<CommandHistoryPanel isOpen token="tok" onClose={vi.fn()} />)

    expect(await screen.findByText(/"result":"done"/)).toBeDefined()
  })

  it('shows an error message when the request fails', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: false, status: 500, json: async () => ({}) } as Response)

    render(<CommandHistoryPanel isOpen token="tok" onClose={vi.fn()} />)

    expect(await screen.findByText('Failed to load history (500).')).toBeDefined()
  })

  it('closes on Escape', () => {
    vi.mocked(fetch).mockResolvedValue({ ok: true, json: async () => ({ entries: [] }) } as Response)
    const onClose = vi.fn()

    render(<CommandHistoryPanel isOpen token="tok" onClose={onClose} />)
    fireEvent.keyDown(window, { key: 'Escape' })

    expect(onClose).toHaveBeenCalled()
  })

  it('closes when clicking the close button', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: true, json: async () => ({ entries: [] }) } as Response)
    const onClose = vi.fn()

    render(<CommandHistoryPanel isOpen token="tok" onClose={onClose} />)
    fireEvent.click(await screen.findByLabelText('Close history panel'))

    expect(onClose).toHaveBeenCalled()
  })

  it('closes when clicking the overlay backdrop', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: true, json: async () => ({ entries: [] }) } as Response)
    const onClose = vi.fn()

    render(<CommandHistoryPanel isOpen token="tok" onClose={onClose} />)
    fireEvent.click(await screen.findByRole('dialog'))

    expect(onClose).toHaveBeenCalled()
  })

  it('restores focus to the element that opened it once it closes', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: true, json: async () => ({ entries: [] }) } as Response)
    const trigger = document.createElement('button')
    trigger.textContent = 'Open history'
    document.body.appendChild(trigger)
    trigger.focus()

    const { rerender } = render(<CommandHistoryPanel isOpen={false} token="tok" onClose={vi.fn()} />)
    rerender(<CommandHistoryPanel isOpen token="tok" onClose={vi.fn()} />)
    // Simulate the panel's own content stealing focus while open (e.g. a
    // user clicking inside it), so restoring the trigger's focus on close
    // is actually exercised rather than trivially already true because
    // nothing ever moved focus away from it.
    ;(await screen.findByLabelText('Close history panel')).focus()

    rerender(<CommandHistoryPanel isOpen={false} token="tok" onClose={vi.fn()} />)

    expect(document.activeElement).toBe(trigger)
    document.body.removeChild(trigger)
  })

  it('closes on Escape even when the focused element stops propagation before window', () => {
    const onClose = vi.fn()
    const swallowingTarget = document.createElement('textarea')
    swallowingTarget.addEventListener('keydown', (event) => event.stopPropagation())
    document.body.appendChild(swallowingTarget)

    render(<CommandHistoryPanel isOpen token="tok" onClose={onClose} />)

    fireEvent.keyDown(swallowingTarget, { key: 'Escape' })

    expect(onClose).toHaveBeenCalled()
  })
})
