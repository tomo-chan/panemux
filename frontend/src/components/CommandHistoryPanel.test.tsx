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
})
