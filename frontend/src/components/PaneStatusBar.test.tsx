import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { PaneStatusBar } from './PaneStatusBar'
import type { DisplayConfig, PaneConfig } from '../types'

const shown: DisplayConfig = { show_header: true, show_status_bar: true }
const hidden: DisplayConfig = { show_header: true, show_status_bar: false }

type PaneType = PaneConfig['type']

const pane = (overrides: Partial<PaneConfig> = {}): PaneConfig => ({
  id: 'main',
  type: 'local',
  ...overrides,
})

describe('PaneStatusBar', () => {
  it.each([
    ['local', 'LOCAL'],
    ['ssh', 'SSH'],
    ['tmux', 'TMUX'],
    ['ssh_tmux', 'SSH+TMUX'],
  ])('labels a %s pane', (type, label) => {
    render(<PaneStatusBar pane={pane({ type: type as PaneType })} displayConfig={shown} />)

    expect(screen.getByText(label)).toBeDefined()
  })

  // The cast is the point of the test: the label table's fallback is
  // unreachable through the declared union, and it is there for a response
  // that carries a type this build does not know about.
  it('falls back to the uppercased type for a pane type it does not know', () => {
    render(<PaneStatusBar pane={pane({ type: 'wayland' as PaneType })} displayConfig={shown} />)

    expect(screen.getByText('WAYLAND')).toBeDefined()
  })

  it('shows the connection name only when the pane has one', () => {
    const { rerender, container } = render(
      <PaneStatusBar pane={pane({ type: 'ssh', connection: 'host1' })} displayConfig={shown} />,
    )
    expect(screen.getByText('host1')).toBeDefined()

    rerender(<PaneStatusBar pane={pane({ type: 'ssh' })} displayConfig={shown} />)
    expect(container.textContent).toBe('SSH')
  })

  it('shows the terminal size only when both dimensions are known', () => {
    const { rerender } = render(
      <PaneStatusBar pane={pane()} displayConfig={shown} cols={120} rows={40} />,
    )
    expect(screen.getByText('120×40')).toBeDefined()

    rerender(<PaneStatusBar pane={pane()} displayConfig={shown} cols={120} />)
    expect(screen.queryByText(/120/)).toBeNull()
  })

  it('shows a zero size rather than treating it as absent', () => {
    render(<PaneStatusBar pane={pane()} displayConfig={shown} cols={0} rows={0} />)

    expect(screen.getByText('0×0')).toBeDefined()
  })

  it('renders nothing when the display config hides the status bar', () => {
    const { container } = render(<PaneStatusBar pane={pane()} displayConfig={hidden} />)

    expect(container.firstChild).toBeNull()
  })

  it.each([
    ['shows it against a hiding display config', true, hidden, false],
    ['hides it against a showing display config', false, shown, true],
  ])("the pane's own setting wins: %s", (_name, paneSetting, displayConfig, hiddenResult) => {
    const { container } = render(
      <PaneStatusBar pane={pane({ show_status_bar: paneSetting })} displayConfig={displayConfig} />,
    )

    expect(container.firstChild === null).toBe(hiddenResult)
  })
})
