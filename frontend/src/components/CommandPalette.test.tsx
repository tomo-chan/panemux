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

  it('renders the assistant answer, not the frame types, as lines stream in', async () => {
    render(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())

    fireEvent.change(screen.getByLabelText('Command center prompt'), { target: { value: 'who is on the board?' } })
    fireEvent.click(screen.getByRole('button', { name: /send/i }))

    act(() => ws.simulateMessage({ type: 'line', raw: { type: 'system', subtype: 'init' } }))
    act(() => ws.simulateMessage({ type: 'line', raw: { type: 'stream_event', event: { type: 'message_start' } } }))
    act(() =>
      ws.simulateMessage({
        type: 'line',
        raw: { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'board_status', input: {} }] } },
      }),
    )
    act(() =>
      ws.simulateMessage({
        type: 'line',
        raw: { type: 'assistant', message: { content: [{ type: 'text', text: 'Both panes are on the board.' }] } },
      }),
    )
    act(() => ws.simulateMessage({ type: 'done' }))

    expect(await screen.findByText('Both panes are on the board.')).toBeDefined()
    expect(screen.getByText('→ board_status')).toBeDefined()
    // The bookkeeping frames must not be rendered as "[system]" / "[stream_event]".
    expect(screen.queryByText('[system]')).toBeNull()
    expect(screen.queryByText('[stream_event]')).toBeNull()
  })

  it('does not render the answer twice when the result frame repeats it', async () => {
    render(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())

    fireEvent.change(screen.getByLabelText('Command center prompt'), { target: { value: 'status' } })
    fireEvent.click(screen.getByRole('button', { name: /send/i }))

    act(() =>
      ws.simulateMessage({
        type: 'line',
        raw: { type: 'assistant', message: { content: [{ type: 'text', text: 'Delivered to both panes.' }] } },
      }),
    )
    act(() => ws.simulateMessage({ type: 'line', raw: { type: 'result', result: 'Delivered to both panes.' } }))
    act(() => ws.simulateMessage({ type: 'done' }))

    expect(await screen.findAllByText('Delivered to both panes.')).toHaveLength(1)
  })

  it('renders inline history from a fixture response, summarized the same way', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        entries: [
          { at: '2026-08-15T02:00:00Z', raw: { type: 'stream_event', event: { type: 'message_start' } } },
          {
            at: '2026-08-15T02:00:01Z',
            raw: { type: 'assistant', message: { content: [{ type: 'text', text: 'Earlier answer.' }] } },
          },
        ],
      }),
    }))

    render(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)

    expect(await screen.findByText('Earlier answer.')).toBeDefined()
    expect(screen.queryByText('[stream_event]')).toBeNull()
    expect(screen.queryByText('No recent activity.')).toBeNull()
  })

  it('scrolls the transcript to the newest line as frames arrive', async () => {
    render(<CommandPalette isOpen token="tok" onClose={vi.fn()} />)
    const ws = MockWebSocket.instances[0]
    act(() => ws.simulateOpen())

    const transcript = screen.getByTestId('command-palette-transcript')
    // jsdom does no layout, so scrollHeight is 0 and scrollTop is not writable
    // in the usual way. Stand in for both so the assignment is observable.
    let scrollTop = 0
    Object.defineProperty(transcript, 'scrollHeight', { value: 900, configurable: true })
    Object.defineProperty(transcript, 'scrollTop', {
      get: () => scrollTop,
      set: (value: number) => {
        scrollTop = value
      },
      configurable: true,
    })

    fireEvent.change(screen.getByLabelText('Command center prompt'), { target: { value: 'hi' } })
    fireEvent.click(screen.getByRole('button', { name: /send/i }))
    act(() =>
      ws.simulateMessage({
        type: 'line',
        raw: { type: 'assistant', message: { content: [{ type: 'text', text: 'An answer below the fold.' }] } },
      }),
    )

    await waitFor(() => expect(scrollTop).toBe(900))
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

    act(() =>
      ws.simulateMessage({
        type: 'line',
        raw: { type: 'assistant', message: { content: [{ type: 'text', text: 'Checking the board.' }] } },
      }),
    )
    expect(await screen.findByText('Checking the board.')).toBeDefined()
    expect(screen.queryByText('Both panes are idle.')).toBeNull()

    act(() =>
      ws.simulateMessage({
        type: 'line',
        raw: { type: 'assistant', message: { content: [{ type: 'text', text: 'Both panes are idle.' }] } },
      }),
    )
    expect(await screen.findByText('Both panes are idle.')).toBeDefined()
    // The earlier line stays put rather than being replaced.
    expect(screen.getByText('Checking the board.')).toBeDefined()
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

  it('closes on Escape even when the focused element stops propagation before window', () => {
    const onClose = vi.fn()
    const swallowingTarget = document.createElement('textarea')
    swallowingTarget.addEventListener('keydown', (event) => event.stopPropagation())
    document.body.appendChild(swallowingTarget)

    render(<CommandPalette isOpen token="tok" onClose={onClose} />)

    fireEvent.keyDown(swallowingTarget, { key: 'Escape' })

    expect(onClose).toHaveBeenCalled()
  })
})
