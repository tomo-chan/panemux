import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { CommandPalette } from './CommandPalette'

class MockWebSocket {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3

  static instances: MockWebSocket[] = []
  onopen: (() => void) | null = null
  onmessage: ((e: { data: unknown }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  readyState = MockWebSocket.OPEN
  url: string
  protocols: string | string[] | undefined
  sent: unknown[] = []

  constructor(url: string, protocols?: string | string[]) {
    this.url = url
    this.protocols = protocols
    MockWebSocket.instances.push(this)
  }

  send(data: unknown) {
    this.sent.push(data)
  }

  close() {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.()
  }

  simulateOpen() {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.()
  }

  simulateMessage(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) })
  }
}

describe('CommandPalette', () => {
  let originalWebSocket: typeof WebSocket

  beforeEach(() => {
    originalWebSocket = window.WebSocket
    MockWebSocket.instances = []
    window.WebSocket = MockWebSocket as unknown as typeof WebSocket
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ entries: [] }),
    }))
  })

  afterEach(() => {
    window.WebSocket = originalWebSocket
    vi.unstubAllGlobals()
  })

  it('does not render when closed', () => {
    render(<CommandPalette isOpen={false} token="tok" onClose={vi.fn()} />)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('renders the prompt input and header when open', () => {
    render(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)
    expect(screen.getByRole('dialog')).toBeDefined()
    expect(screen.getByLabelText('Command center prompt')).toBeDefined()
  })

  it('fetches recent history with the bearer token on open', async () => {
    render(<CommandPalette isOpen token="sekret" onClose={vi.fn()} />)

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith('/api/board/command/history', {
        headers: { Authorization: 'Bearer sekret' },
      })
    })
  })

  it('shows the empty state when there is no history or turns', async () => {
    render(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)
    await waitFor(() => expect(fetch).toHaveBeenCalled())
    expect(screen.getByText('No recent activity.')).toBeDefined()
  })

  it('closes on Escape', () => {
    const onClose = vi.fn()
    render(<CommandPalette isOpen token="tok" onClose={onClose} />)

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(onClose).toHaveBeenCalled()
  })

  it('closes when clicking the overlay backdrop', () => {
    const onClose = vi.fn()
    render(<CommandPalette isOpen token="tok" onClose={onClose} />)

    fireEvent.click(screen.getByRole('dialog'))

    expect(onClose).toHaveBeenCalled()
  })

  it('connects the WS with the token as subprotocol while open', () => {
    render(<CommandPalette isOpen token="sekret" onClose={vi.fn()} />)
    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0].protocols).toEqual(['sekret'])
  })

  it('submits a prompt over WS and clears the input', async () => {
    render(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())

    const input = screen.getByLabelText('Command center prompt') as HTMLInputElement
    fireEvent.change(input, { target: { value: 'status please' } })
    fireEvent.click(screen.getByRole('button', { name: /send/i }))

    expect(ws.sent).toEqual([JSON.stringify({ prompt: 'status please' })])
    expect(input.value).toBe('')
    expect(await screen.findByText('> status please')).toBeDefined()
  })

  it('does not submit an empty prompt', () => {
    render(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())

    fireEvent.click(screen.getByRole('button', { name: /send/i }))

    expect(ws.sent).toHaveLength(0)
  })

  it('renders a streamed error frame on the active turn', async () => {
    render(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())

    fireEvent.change(screen.getByLabelText('Command center prompt'), { target: { value: 'hi' } })
    fireEvent.click(screen.getByRole('button', { name: /send/i }))
    act(() => ws.simulateMessage({ type: 'error', message: 'claude exited with error: exit status 1' }))

    expect(await screen.findByText('claude exited with error: exit status 1')).toBeDefined()
  })

  it('renders fixture history entries immediately on open', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        entries: [
          { at: '2026-08-10T12:00:00Z', raw: { type: 'result', result: 'previous answer' } },
        ],
      }),
    }))

    render(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)

    expect(await screen.findByText('previous answer')).toBeDefined()
  })

  it('updates incrementally as line frames stream in, one at a time', async () => {
    render(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())

    fireEvent.change(screen.getByLabelText('Command center prompt'), { target: { value: 'hi' } })
    fireEvent.click(screen.getByRole('button', { name: /send/i }))

    act(() => ws.simulateMessage({ type: 'line', raw: { type: 'progress' } }))
    expect(await screen.findByText('[progress]')).toBeDefined()
    expect(screen.queryByText('done')).toBeNull()

    act(() => ws.simulateMessage({ type: 'line', raw: { result: 'done' } }))
    expect(await screen.findByText('done')).toBeDefined()
    expect(screen.getByText('[progress]')).toBeDefined()
  })

  it('shows the busy message when a busy frame arrives', async () => {
    render(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())

    fireEvent.change(screen.getByLabelText('Command center prompt'), { target: { value: 'hi' } })
    fireEvent.click(screen.getByRole('button', { name: /send/i }))
    act(() => ws.simulateMessage({ type: 'busy' }))

    expect(await screen.findByText('Command center is busy — try again shortly.')).toBeDefined()
  })

  it('restores focus to the element that opened it once it closes', async () => {
    const trigger = document.createElement('button')
    trigger.textContent = 'Open palette'
    document.body.appendChild(trigger)
    trigger.focus()

    const { rerender } = render(<CommandPalette isOpen={false} token="tok" onClose={vi.fn()} />)
    rerender(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('dialog')).toBeDefined())

    rerender(<CommandPalette isOpen={false} token="tok" onClose={vi.fn()} />)

    expect(document.activeElement).toBe(trigger)
    document.body.removeChild(trigger)
  })
})
