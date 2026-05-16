import { describe, expect, it, vi } from 'vitest'
vi.mock('../hooks/useTerminal', () => ({
  useTerminal: () => ({
    handleResize: vi.fn(),
    connected: true,
    dims: null,
    sessionExited: false,
    restartSession: vi.fn(),
  }),
}))
vi.mock('../hooks/useGitInfo', () => ({
  useGitInfo: () => ({ is_git: false }),
}))
import { dropZoneStyle, PANE_DROP_ZONE_THICKNESS } from './TerminalPane'

describe('TerminalPane drop zones', () => {
  it('uses the expanded thickness for horizontal edge drop zones', () => {
    expect(dropZoneStyle('top', false)).toMatchObject({ height: PANE_DROP_ZONE_THICKNESS })
    expect(dropZoneStyle('bottom', true)).toMatchObject({ height: PANE_DROP_ZONE_THICKNESS })
  })

  it('uses the expanded thickness for vertical edge drop zones', () => {
    expect(dropZoneStyle('left', false)).toMatchObject({ width: PANE_DROP_ZONE_THICKNESS })
    expect(dropZoneStyle('right', true)).toMatchObject({ width: PANE_DROP_ZONE_THICKNESS })
  })
})
