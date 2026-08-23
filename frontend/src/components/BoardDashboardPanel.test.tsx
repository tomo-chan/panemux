import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { BoardDashboardPanel } from './BoardDashboardPanel'

function fetchRouter(handlers: {
  status?: () => Response | Promise<Response>
  messages?: () => Response | Promise<Response>
}) {
  return vi.fn((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.startsWith('/api/board/status')) {
      return Promise.resolve(
        handlers.status ? handlers.status() : ({ ok: true, json: async () => ({ statuses: {} }) } as Response),
      )
    }
    if (url.startsWith('/api/board/messages')) {
      return Promise.resolve(
        handlers.messages ? handlers.messages() : ({ ok: true, json: async () => ({ messages: [], epoch: 'e1' }) } as Response),
      )
    }
    throw new Error(`unexpected fetch: ${url}`)
  })
}

describe('BoardDashboardPanel', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchRouter({}))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('does not render when closed', () => {
    render(<BoardDashboardPanel isOpen={false} token="tok" boardPanes={[]} onClose={vi.fn()} />)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('renders a pane status card from the statuses fixture', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({
          statuses: {
            main: {
              updated_at: new Date().toISOString(),
              state: 'working',
              repo: 'panemux',
              branch: 'feature/dashboard',
              summary: 'Running tests',
              last_tool: 'Bash',
              cwd: '/workspace/user/project',
            },
          },
        }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={vi.fn()} />)

    expect(await screen.findByText('main')).toBeDefined()
    expect(screen.getByText('working')).toBeDefined()
    expect(screen.getByText('Running tests')).toBeDefined()
    expect(screen.getByText(/tool:\s*Bash/)).toBeDefined()
  })


  it('renders a card with every optional field omitted without crashing', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({ statuses: { main: { updated_at: new Date().toISOString() } } }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={vi.fn()} />)

    expect(await screen.findByText('main')).toBeDefined()
  })

  it('renders an unrecognized state string without crashing', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({
          statuses: { main: { updated_at: new Date().toISOString(), state: 'something-new' } },
        }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={vi.fn()} />)

    expect(await screen.findByText('something-new')).toBeDefined()
  })

  it('shows a stale pill for a status older than 5 minutes', async () => {
    const old = new Date(Date.now() - 10 * 60 * 1000).toISOString()
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({ statuses: { main: { updated_at: old, state: 'idle' } } }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={vi.fn()} />)

    expect(await screen.findByText('stale')).toBeDefined()
  })

  it('does not show a stale pill for a recent status', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({
        ok: true,
        json: async () => ({ statuses: { main: { updated_at: new Date().toISOString(), state: 'idle' } } }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={vi.fn()} />)

    await screen.findByText('main')
    expect(screen.queryByText('stale')).toBeNull()
  })

  it('excludes board_status rows from the message feed', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      messages: () => ({
        ok: true,
        json: async () => ({
          messages: [
            { at: '2026-08-14T12:00:00Z', host: 'devbox', team: 'panemux', from: 'main', to: '_system', body: JSON.stringify({ kind: 'board_status' }), is_status: true, seq: 1 },
            { at: '2026-08-14T12:00:01Z', host: 'devbox', team: 'panemux', from: 'main', to: 'side', body: 'hello there', is_status: false, seq: 2 },
          ],
          epoch: 'e1',
        }),
      } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={vi.fn()} />)

    expect(await screen.findByText('hello there')).toBeDefined()
    expect(screen.queryByText(/board_status/)).toBeNull()
  })

  it('shows the empty states when there are no statuses or messages', async () => {
    render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={vi.fn()} />)

    expect(await screen.findByText('No pane has agent board enabled yet.')).toBeDefined()
    expect(screen.getByText('No messages yet.')).toBeDefined()
  })

  it('shows an error message while keeping the panel open', async () => {
    vi.stubGlobal('fetch', fetchRouter({
      status: () => ({ ok: false, status: 401 } as Response),
    }))

    render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={vi.fn()} />)

    expect(await screen.findByText('Not authorized to view the agent board.')).toBeDefined()
    expect(screen.getByRole('dialog')).toBeDefined()
  })

  it('closes on Escape', () => {
    const onClose = vi.fn()
    render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={onClose} />)

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(onClose).toHaveBeenCalled()
  })

  // Regression test for the Escape handler's listener phase, not merely
  // that it exists: a focused xterm terminal stops keydown propagation
  // before it ever reaches a bubble-phase window listener, so a
  // bubble-registered Escape handler silently does nothing in exactly the
  // state Cmd/Ctrl+Shift+B leaves the user in (panel open, terminal still
  // holding focus). Verified in a real browser by
  // frontend/e2e/agent-board.spec.ts; this is the same assertion at the
  // smallest unit, with a stand-in element that swallows the event the same
  // way.
  it('closes on Escape even when the focused element stops propagation before window', () => {
    const onClose = vi.fn()
    const swallowingTarget = document.createElement('textarea')
    swallowingTarget.addEventListener('keydown', (event) => event.stopPropagation())
    document.body.appendChild(swallowingTarget)

    render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={onClose} />)

    fireEvent.keyDown(swallowingTarget, { key: 'Escape' })

    expect(onClose).toHaveBeenCalled()
  })

  it('closes when clicking the overlay backdrop', async () => {
    const onClose = vi.fn()
    render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={onClose} />)

    fireEvent.click(await screen.findByRole('dialog'))

    expect(onClose).toHaveBeenCalled()
  })

  it('closes when clicking the close button', async () => {
    const onClose = vi.fn()
    render(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={onClose} />)

    fireEvent.click(await screen.findByLabelText('Close agent board panel'))

    expect(onClose).toHaveBeenCalled()
  })

  it('restores focus to the previously focused element when it closes', async () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    trigger.focus()

    const { rerender } = render(<BoardDashboardPanel isOpen={false} token="tok" boardPanes={[]} onClose={vi.fn()} />)
    rerender(<BoardDashboardPanel isOpen token="tok" boardPanes={[]} onClose={vi.fn()} />)

    await waitFor(() => expect(screen.getByRole('dialog')).toBeDefined())

    rerender(<BoardDashboardPanel isOpen={false} token="tok" boardPanes={[]} onClose={vi.fn()} />)

    expect(document.activeElement).toBe(trigger)
  })
})

describe('BoardDashboardPanel connection and activity', () => {
  const reported = {
    updated_at: new Date().toISOString(),
    state: 'working',
    summary: 'Rewriting the relay tests',
    last_tool: 'Edit',
    repo: 'example/project',
    branch: 'feature/x',
    pr_url: 'https://github.com/example/project/pull/42',
  }

  function renderPanel(statuses: Record<string, unknown>, boardPanes: { id: string; title?: string }[]) {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) =>
      Promise.resolve({
        ok: true,
        json: async () => (String(url).includes('/status') ? { statuses } : { messages: [], epoch: 'e' }),
      }),
    ))
    render(<BoardDashboardPanel isOpen token="tok" boardPanes={boardPanes} onClose={vi.fn()} />)
  }

  // The point of the board is telling you whether a pane is actually on it.
  // Listing only panes that already reported made "configured but never
  // joined" indistinguishable from "not configured at all".
  it('lists a board-enabled pane that has never reported', async () => {
    renderPanel({}, [{ id: 'api' }, { id: 'web' }])

    expect(await screen.findByText('api')).toBeDefined()
    expect(screen.getByText('web')).toBeDefined()
    expect(screen.getAllByText('not joined')).toHaveLength(2)
  })

  it('shows what a joined pane is doing', async () => {
    renderPanel({ api: reported }, [{ id: 'api' }])

    expect(await screen.findByText('working')).toBeDefined()
    expect(screen.getByText('Rewriting the relay tests')).toBeDefined()
    expect(screen.getByText('tool: Edit')).toBeDefined()
    expect(screen.queryByText('not joined')).toBeNull()
  })

  // repo/branch/PR come from panemux running git itself, shown in the pane
  // header and the workspace bar. The board's copies are self-reported and
  // can contradict them, so the board does not repeat them.
  it('does not repeat the repo, branch or PR the header already owns', async () => {
    renderPanel({ api: reported }, [{ id: 'api' }])
    await screen.findByText('working')

    expect(screen.queryByText('example/project')).toBeNull()
    expect(screen.queryByText('feature/x')).toBeNull()
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('still reports a pane that left the config but has a status', async () => {
    renderPanel({ ghost: reported }, [])
    expect(await screen.findByText('ghost')).toBeDefined()
  })

  it('keeps the empty state when no pane is board-enabled', async () => {
    renderPanel({}, [])
    expect(await screen.findByText('No pane has agent board enabled yet.')).toBeDefined()
  })
})

// The board's reason to exist once a workspace has more than a couple of
// board-enabled panes: reading down the column and knowing what each one is
// working on. That fails in two ways that have nothing to do with whether a
// summary was reported — a summary clipped to one 420px line, and a column
// of opaque pane IDs with no human name attached.
describe('BoardDashboardPanel work summaries', () => {
  const longSummary =
    'Rewriting the relay integration tests so a status row that arrives before its ' +
    'pane finishes joining is retried rather than dropped on the floor'

  function renderPanel(statuses: Record<string, unknown>, boardPanes: { id: string; title?: string }[]) {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) =>
      Promise.resolve({
        ok: true,
        json: async () => (String(url).includes('/status') ? { statuses } : { messages: [], epoch: 'e' }),
      }),
    ))
    render(<BoardDashboardPanel isOpen token="tok" boardPanes={boardPanes} onClose={vi.fn()} />)
  }

  it('renders a long summary in full rather than clipping it to one line', async () => {
    renderPanel({ api: { updated_at: new Date().toISOString(), summary: longSummary } }, [{ id: 'api' }])

    const summary = await screen.findByText(longSummary)
    // getByText matches on normalized text content, so a CSS-clipped string
    // would still be "found" here — the style is the actual assertion.
    expect(summary.style.whiteSpace).not.toBe('nowrap')
    expect(summary.style.textOverflow).not.toBe('ellipsis')
  })

  // The summary is the one field worth spending vertical space on, but not
  // unbounded space: an agent that reports three paragraphs must not push
  // every other pane below the fold.
  it('caps how much vertical space one summary can take', async () => {
    renderPanel({ api: { updated_at: new Date().toISOString(), summary: longSummary } }, [{ id: 'api' }])

    const summary = await screen.findByText(longSummary)
    expect(summary.style.getPropertyValue('-webkit-line-clamp')).toBe('4')
    expect(summary.style.overflow).toBe('hidden')
  })

  it('shows the pane title next to the ID so the column is readable', async () => {
    renderPanel({}, [{ id: 'pane-1755', title: 'Board Main' }])

    expect(await screen.findByText('pane-1755')).toBeDefined()
    expect(screen.getByText('Board Main')).toBeDefined()
  })

  it('falls back to the ID alone when a pane has no title', async () => {
    renderPanel({}, [{ id: 'pane-1755' }])

    expect(await screen.findByText('pane-1755')).toBeDefined()
  })

  // A pane reporting from outside the config has no title to look up, and
  // that must not blank out its ID.
  it('shows an unconfigured reporting pane by ID', async () => {
    renderPanel({ ghost: { updated_at: new Date().toISOString(), summary: 'Cleaning up' } }, [])

    expect(await screen.findByText('ghost')).toBeDefined()
    expect(screen.getByText('Cleaning up')).toBeDefined()
  })

  // last_tool is a bare tool name and stays on one line; only the summary
  // gets the extra room.
  it('keeps the tool line to a single line', async () => {
    renderPanel({ api: { updated_at: new Date().toISOString(), last_tool: 'Edit' } }, [{ id: 'api' }])

    const tool = await screen.findByText('tool: Edit')
    expect(tool.style.whiteSpace).toBe('nowrap')
  })
})
