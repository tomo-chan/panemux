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

    // The readable text, not the frame it arrived in. This panel used to
    // dump JSON.stringify(raw) for every line, which made the record
    // unreadable in practice.
    expect(await screen.findByText('done')).toBeDefined()
    expect(screen.queryByText(/"result":/)).toBeNull()
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

describe('CommandHistoryPanel readability', () => {
  const entry = (raw: unknown) => ({ at: '2026-08-16T15:00:00Z', raw })

  function renderWith(entries: unknown[]) {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ entries }) }))
    render(<CommandHistoryPanel isOpen token="tok" onClose={vi.fn()} />)
  }

  it('shows the prompt and the answer, not raw JSON', async () => {
    renderWith([
      entry({ type: 'panemux_prompt', text: 'which panes are blocked?' }),
      entry({ type: 'stream_event', event: { type: 'message_start' } }),
      entry({ type: 'assistant', message: { content: [{ type: 'text', text: 'None are blocked.' }] } }),
    ])

    expect(await screen.findByText('None are blocked.')).toBeDefined()
    expect(screen.getByText('> which panes are blocked?')).toBeDefined()
    // The bookkeeping frame is neither rendered nor dumped as JSON.
    expect(screen.queryByText(/stream_event/)).toBeNull()
    expect(screen.queryByText(/"type":/)).toBeNull()
  })

  it('names the tools a turn used', async () => {
    renderWith([
      entry({ type: 'panemux_prompt', text: 'status?' }),
      entry({ type: 'assistant', message: { content: [{ type: 'tool_use', name: 'board_status', input: {} }] } }),
    ])

    expect(await screen.findByText('→ board_status')).toBeDefined()
  })

  it('keeps the empty state when every entry is bookkeeping', async () => {
    renderWith([entry({ type: 'stream_event' }), entry({ type: 'system', subtype: 'init' })])

    expect(await screen.findByText('No history yet.')).toBeDefined()
  })
})
