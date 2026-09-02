import { describe, it, expect } from 'vitest'
import {
  DisplayConfigSchema,
  GitInfoSchema,
  PaneConfigSchema,
  LayoutNodeSchema,
  LayoutChildSchema,
  SessionInfoSchema,
  SessionInfoListSchema,
  WSControlMessageSchema,
  SSHConfigHostSchema,
  SSHConfigHostsResponseSchema,
  DetectShellResponseSchema,
  WorkspaceTabPositionRequestSchema,
  WorkspaceVerticalBarWidthRequestSchema,
  WorkspacesResponseSchema,
  DirectoryEntrySchema,
  DirectoryBrowserResponseSchema,
  BoardSessionTokenResponseSchema,
  BoardCommandFrameSchema,
  BoardCommandHistoryEntrySchema,
  BoardCommandHistoryResponseSchema,
  BoardStatusEntrySchema,
  BoardStatusResponseSchema,
  BoardMessageSchema,
  BoardMessagesResponseSchema,
} from './index'

describe('GitInfoSchema', () => {
  it('accepts git info with PR metadata', () => {
    const result = GitInfoSchema.safeParse({
      is_git: true,
      branch: 'feature/pane-pr-link',
      repo: 'panemux',
      repo_url: 'https://github.com/example/panemux',
      pr_number: 123,
      pr_url: 'https://github.com/example/panemux/pull/123',
    })
    expect(result.success).toBe(true)
  })

  it('rejects invalid PR metadata types', () => {
    const result = GitInfoSchema.safeParse({
      is_git: true,
      pr_number: '123',
      pr_url: 123,
    })
    expect(result.success).toBe(false)
  })

  it('accepts git info without a worktrees array', () => {
    const result = GitInfoSchema.safeParse({
      is_git: true,
      branch: 'main',
      repo: 'panemux',
    })
    expect(result.success).toBe(true)
  })

  it('accepts git info with multiple worktrees', () => {
    const result = GitInfoSchema.safeParse({
      is_git: true,
      branch: 'feature/worktree-a',
      repo: 'panemux',
      pr_number: 111,
      pr_url: 'https://github.com/example/panemux/pull/111',
      worktrees: [
        {
          branch: 'feature/worktree-a',
          repo: 'panemux',
          pr_number: 111,
          pr_url: 'https://github.com/example/panemux/pull/111',
        },
        {
          branch: 'feature/worktree-b',
          repo: 'panemux',
        },
      ],
    })
    expect(result.success).toBe(true)
  })

  it('rejects a worktrees entry with invalid PR metadata types', () => {
    const result = GitInfoSchema.safeParse({
      is_git: true,
      worktrees: [{ branch: 'main', pr_number: '111' }],
    })
    expect(result.success).toBe(false)
  })
})

describe('DisplayConfigSchema', () => {
  it('accepts valid display config', () => {
    const result = DisplayConfigSchema.safeParse({ show_header: true, show_status_bar: false })
    expect(result.success).toBe(true)
  })

  it('rejects missing show_header', () => {
    const result = DisplayConfigSchema.safeParse({ show_status_bar: false })
    expect(result.success).toBe(false)
  })

  it('rejects non-boolean value', () => {
    const result = DisplayConfigSchema.safeParse({ show_header: 'yes', show_status_bar: false })
    expect(result.success).toBe(false)
  })
})

describe('PaneConfigSchema', () => {
  it('accepts valid pane', () => {
    const result = PaneConfigSchema.safeParse({ id: 'main', type: 'local' })
    expect(result.success).toBe(true)
  })

  it('rejects missing id', () => {
    const result = PaneConfigSchema.safeParse({ type: 'local' })
    expect(result.success).toBe(false)
  })

  it('rejects empty id', () => {
    const result = PaneConfigSchema.safeParse({ id: '', type: 'local' })
    expect(result.success).toBe(false)
  })

  it('rejects invalid type', () => {
    const result = PaneConfigSchema.safeParse({ id: 'main', type: 'unknown' })
    expect(result.success).toBe(false)
  })

  it('accepts optional show_header override', () => {
    const result = PaneConfigSchema.safeParse({ id: 'main', type: 'local', show_header: false })
    expect(result.success).toBe(true)
  })
})

describe('LayoutNodeSchema', () => {
  it('accepts nested layout', () => {
    const result = LayoutNodeSchema.safeParse({
      direction: 'horizontal',
      children: [
        {
          size: 50,
          direction: 'vertical',
          children: [
            { size: 50, pane: { id: 'p1', type: 'local' } },
            { size: 50, pane: { id: 'p2', type: 'local' } },
          ],
        },
        { size: 50, pane: { id: 'p3', type: 'local' } },
      ],
    })
    expect(result.success).toBe(true)
  })

  it('rejects invalid direction', () => {
    const result = LayoutNodeSchema.safeParse({
      direction: 'diagonal',
      children: [],
    })
    expect(result.success).toBe(false)
  })
})

describe('LayoutChildSchema', () => {
  it('accepts positive size', () => {
    const result = LayoutChildSchema.safeParse({
      size: 50,
      pane: { id: 'main', type: 'local' },
    })
    expect(result.success).toBe(true)
  })

  it('rejects negative size', () => {
    const result = LayoutChildSchema.safeParse({
      size: -10,
      pane: { id: 'main', type: 'local' },
    })
    expect(result.success).toBe(false)
  })

  it('rejects zero size', () => {
    const result = LayoutChildSchema.safeParse({
      size: 0,
      pane: { id: 'main', type: 'local' },
    })
    expect(result.success).toBe(false)
  })
})

describe('WorkspacesResponseSchema', () => {
  it('accepts valid workspaces response', () => {
    const result = WorkspacesResponseSchema.safeParse({
      active: 'dev',
      tab_position: 'left',
      vertical_bar_width: 280,
      items: [
        {
          id: 'dev',
          title: 'Dev',
          layout: {
            direction: 'horizontal',
            children: [{ size: 100, pane: { id: 'main', type: 'local' } }],
          },
        },
      ],
    })
    expect(result.success).toBe(true)
  })

  it('rejects invalid tab position', () => {
    const result = WorkspacesResponseSchema.safeParse({
      active: 'dev',
      tab_position: 'diagonal',
      vertical_bar_width: 280,
      items: [
        {
          id: 'dev',
          title: 'Dev',
          layout: { direction: 'horizontal', children: [] },
        },
      ],
    })
    expect(result.success).toBe(false)
  })

  it('accepts every tab position', () => {
    for (const tabPosition of ['top', 'bottom', 'left', 'right']) {
      const result = WorkspacesResponseSchema.safeParse({
        active: 'dev',
        tab_position: tabPosition,
        vertical_bar_width: 280,
        items: [
          {
            id: 'dev',
            title: 'Dev',
            layout: {
              direction: 'horizontal',
              children: [{ size: 100, pane: { id: 'main', type: 'local' } }],
            },
          },
        ],
      })
      expect(result.success).toBe(true)
    }
  })

  it('rejects empty workspace list and blank identifiers', () => {
    expect(WorkspacesResponseSchema.safeParse({
      active: 'dev',
      tab_position: 'top',
      vertical_bar_width: 280,
      items: [],
    }).success).toBe(false)

    expect(WorkspacesResponseSchema.safeParse({
      active: '',
      tab_position: 'top',
      vertical_bar_width: 280,
      items: [
        {
          id: '',
          title: '',
          layout: { direction: 'horizontal', children: [] },
        },
      ],
    }).success).toBe(false)
  })

  it('rejects out-of-range vertical bar widths', () => {
    expect(WorkspacesResponseSchema.safeParse({
      active: 'dev',
      tab_position: 'left',
      vertical_bar_width: 120,
      items: [
        {
          id: 'dev',
          title: 'Dev',
          layout: {
            direction: 'horizontal',
            children: [{ size: 100, pane: { id: 'main', type: 'local' } }],
          },
        },
      ],
    }).success).toBe(false)
  })
})

describe('WorkspaceTabPositionRequestSchema', () => {
  it('accepts every tab position', () => {
    for (const tabPosition of ['top', 'bottom', 'left', 'right']) {
      const result = WorkspaceTabPositionRequestSchema.safeParse({ tab_position: tabPosition })
      expect(result.success).toBe(true)
    }
  })

  it('rejects invalid or missing tab position', () => {
    expect(WorkspaceTabPositionRequestSchema.safeParse({ tab_position: 'diagonal' }).success).toBe(false)
    expect(WorkspaceTabPositionRequestSchema.safeParse({}).success).toBe(false)
  })
})

describe('WorkspaceVerticalBarWidthRequestSchema', () => {
  it('accepts valid widths', () => {
    for (const width of [180, 280, 520]) {
      const result = WorkspaceVerticalBarWidthRequestSchema.safeParse({ vertical_bar_width: width })
      expect(result.success).toBe(true)
    }
  })

  it('rejects invalid or missing widths', () => {
    expect(WorkspaceVerticalBarWidthRequestSchema.safeParse({ vertical_bar_width: 120 }).success).toBe(false)
    expect(WorkspaceVerticalBarWidthRequestSchema.safeParse({}).success).toBe(false)
  })
})

describe('SessionInfoSchema', () => {
  it('accepts valid session', () => {
    const result = SessionInfoSchema.safeParse({
      id: 's1',
      type: 'local',
      title: 'Terminal',
      state: 'connected',
    })
    expect(result.success).toBe(true)
  })

  it('rejects invalid state', () => {
    const result = SessionInfoSchema.safeParse({
      id: 's1',
      type: 'local',
      title: 'Terminal',
      state: 'unknown',
    })
    expect(result.success).toBe(false)
  })
})

describe('SessionInfoListSchema', () => {
  it('accepts a valid sessions list response', () => {
    const result = SessionInfoListSchema.safeParse([
      {
        id: 's1',
        type: 'local',
        title: 'Terminal',
        state: 'connected',
      },
      {
        id: 's2',
        type: 'ssh',
        title: 'Remote',
        state: 'disconnected',
      },
    ])
    expect(result.success).toBe(true)
  })

  it('rejects invalid session items in the list', () => {
    const result = SessionInfoListSchema.safeParse([
      {
        id: 's1',
        type: 'local',
        title: 'Terminal',
        state: 'unknown',
      },
    ])
    expect(result.success).toBe(false)
  })
})

describe('DirectoryEntrySchema', () => {
  it('accepts a valid directory entry', () => {
    const result = DirectoryEntrySchema.safeParse({
      name: 'src',
      path: '/workspace/user/src',
      has_children: true,
    })
    expect(result.success).toBe(true)
  })

  it('rejects empty names and missing flags', () => {
    expect(DirectoryEntrySchema.safeParse({
      name: '',
      path: '/tmp',
      has_children: true,
    }).success).toBe(false)
    expect(DirectoryEntrySchema.safeParse({
      name: 'tmp',
      path: '/tmp',
    }).success).toBe(false)
  })
})

describe('DirectoryBrowserResponseSchema', () => {
  it('accepts a directory browser response', () => {
    const result = DirectoryBrowserResponseSchema.safeParse({
      path: '/workspace/user',
      entries: [
        { name: 'projects', path: '/workspace/user/projects', has_children: true },
      ],
    })
    expect(result.success).toBe(true)
  })

  it('rejects invalid entries', () => {
    expect(DirectoryBrowserResponseSchema.safeParse({
      path: '/workspace/user',
      entries: [
        { name: '', path: '/workspace/user/projects', has_children: true },
      ],
    }).success).toBe(false)
  })
})

describe('WSControlMessageSchema', () => {
  it('accepts resize message', () => {
    const result = WSControlMessageSchema.safeParse({
      type: 'resize',
      cols: 80,
      rows: 24,
    })
    expect(result.success).toBe(true)
  })

  it('rejects cols=0', () => {
    const result = WSControlMessageSchema.safeParse({
      type: 'resize',
      cols: 0,
      rows: 24,
    })
    expect(result.success).toBe(false)
  })

  it('accepts status message', () => {
    const result = WSControlMessageSchema.safeParse({
      type: 'status',
      state: 'connected',
    })
    expect(result.success).toBe(true)
  })

  it('accepts replay lifecycle messages', () => {
    expect(WSControlMessageSchema.safeParse({
      type: 'replay',
      state: 'start',
    }).success).toBe(true)

    expect(WSControlMessageSchema.safeParse({
      type: 'replay',
      state: 'end',
    }).success).toBe(true)
  })

  it('rejects unknown type', () => {
    const result = WSControlMessageSchema.safeParse({ type: 'unknown' })
    expect(result.success).toBe(false)
  })
})

describe('SSHConfigHostSchema', () => {
  it('accepts valid host with all fields', () => {
    const result = SSHConfigHostSchema.safeParse({
      name: 'myhost',
      hostname: 'myhost.example.com',
      user: 'ubuntu',
      port: 22,
      identity_file: '~/.ssh/id_rsa',
    })
    expect(result.success).toBe(true)
  })

  it('accepts valid host without optional fields', () => {
    const result = SSHConfigHostSchema.safeParse({
      name: 'myhost',
      hostname: 'myhost.example.com',
      user: 'ubuntu',
    })
    expect(result.success).toBe(true)
  })

  it('rejects missing name', () => {
    const result = SSHConfigHostSchema.safeParse({
      hostname: 'myhost.example.com',
      user: 'ubuntu',
    })
    expect(result.success).toBe(false)
  })

  it('rejects empty name', () => {
    const result = SSHConfigHostSchema.safeParse({
      name: '',
      hostname: 'myhost.example.com',
      user: 'ubuntu',
    })
    expect(result.success).toBe(false)
  })

  it('rejects missing hostname', () => {
    const result = SSHConfigHostSchema.safeParse({
      name: 'myhost',
      user: 'ubuntu',
    })
    expect(result.success).toBe(false)
  })

  it('rejects missing user', () => {
    const result = SSHConfigHostSchema.safeParse({
      name: 'myhost',
      hostname: 'myhost.example.com',
    })
    expect(result.success).toBe(false)
  })

  it('rejects port above 65535', () => {
    const result = SSHConfigHostSchema.safeParse({
      name: 'myhost',
      hostname: 'myhost.example.com',
      user: 'ubuntu',
      port: 70000,
    })
    expect(result.success).toBe(false)
  })

  it('rejects negative port', () => {
    const result = SSHConfigHostSchema.safeParse({
      name: 'myhost',
      hostname: 'myhost.example.com',
      user: 'ubuntu',
      port: -1,
    })
    expect(result.success).toBe(false)
  })
})

describe('SSHConfigHostsResponseSchema', () => {
  it('accepts valid response with hosts', () => {
    const result = SSHConfigHostsResponseSchema.safeParse({
      hosts: [{ name: 'myhost', hostname: 'myhost.example.com', user: 'ubuntu' }],
    })
    expect(result.success).toBe(true)
  })

  it('accepts empty hosts array', () => {
    const result = SSHConfigHostsResponseSchema.safeParse({ hosts: [] })
    expect(result.success).toBe(true)
  })

  it('rejects missing hosts field', () => {
    const result = SSHConfigHostsResponseSchema.safeParse({})
    expect(result.success).toBe(false)
  })
})

describe('DetectShellResponseSchema', () => {
  it('accepts valid response', () => {
    const result = DetectShellResponseSchema.safeParse({ shell: '/usr/bin/zsh' })
    expect(result.success).toBe(true)
  })

  it('rejects missing shell field', () => {
    const result = DetectShellResponseSchema.safeParse({})
    expect(result.success).toBe(false)
  })

  it('rejects non-string shell', () => {
    const result = DetectShellResponseSchema.safeParse({ shell: 42 })
    expect(result.success).toBe(false)
  })
})

describe('PaneConfigSchema field length limits', () => {
  it('rejects shell longer than 512 characters', () => {
    const result = PaneConfigSchema.safeParse({ id: 'p1', type: 'local', shell: 'a'.repeat(513) })
    expect(result.success).toBe(false)
  })

  it('accepts shell at the 512 character limit', () => {
    const result = PaneConfigSchema.safeParse({ id: 'p1', type: 'local', shell: 'a'.repeat(512) })
    expect(result.success).toBe(true)
  })

  it('rejects cwd longer than 4096 characters', () => {
    const result = PaneConfigSchema.safeParse({ id: 'p1', type: 'local', cwd: '/'.repeat(4097) })
    expect(result.success).toBe(false)
  })

  it('accepts cwd at the 4096 character limit', () => {
    const result = PaneConfigSchema.safeParse({ id: 'p1', type: 'local', cwd: '/'.repeat(4096) })
    expect(result.success).toBe(true)
  })

  it('rejects title longer than 256 characters', () => {
    const result = PaneConfigSchema.safeParse({ id: 'p1', type: 'local', title: 'x'.repeat(257) })
    expect(result.success).toBe(false)
  })
})

describe('LayoutChildSchema children depth limit', () => {
  it('rejects children array with more than 50 elements', () => {
    const children = Array.from({ length: 51 }, (_, i) => ({
      size: 2,
      pane: { id: `p${i}`, type: 'local' },
    }))
    const result = LayoutChildSchema.safeParse({ size: 50, children })
    expect(result.success).toBe(false)
  })

  it('accepts children array with exactly 50 elements', () => {
    const children = Array.from({ length: 50 }, (_, i) => ({
      size: 2,
      pane: { id: `p${i}`, type: 'local' },
    }))
    const result = LayoutChildSchema.safeParse({ size: 50, children })
    expect(result.success).toBe(true)
  })
})

describe('WSControlMessageSchema error message length limit', () => {
  it('rejects error message longer than 2000 characters', () => {
    const result = WSControlMessageSchema.safeParse({ type: 'error', message: 'x'.repeat(2001) })
    expect(result.success).toBe(false)
  })

  it('accepts error message at the 2000 character limit', () => {
    const result = WSControlMessageSchema.safeParse({ type: 'error', message: 'x'.repeat(2000) })
    expect(result.success).toBe(true)
  })
})

describe('BoardSessionTokenResponseSchema', () => {
  it('accepts a full response', () => {
    const result = BoardSessionTokenResponseSchema.safeParse({
      token: 'sekret',
      command_center_enabled: true,
      agent_board_enabled: true,
    })
    expect(result.success).toBe(true)
  })

  it('rejects a response missing agent_board_enabled', () => {
    const result = BoardSessionTokenResponseSchema.safeParse({
      token: 'sekret',
      command_center_enabled: true,
    })
    expect(result.success).toBe(false)
  })

  it('rejects a response missing command_center_enabled', () => {
    const result = BoardSessionTokenResponseSchema.safeParse({
      token: 'sekret',
      agent_board_enabled: true,
    })
    expect(result.success).toBe(false)
  })

  it('rejects a response missing token', () => {
    const result = BoardSessionTokenResponseSchema.safeParse({
      command_center_enabled: true,
      agent_board_enabled: true,
    })
    expect(result.success).toBe(false)
  })

  it('rejects non-boolean agent_board_enabled', () => {
    const result = BoardSessionTokenResponseSchema.safeParse({
      token: 'sekret',
      command_center_enabled: true,
      agent_board_enabled: 'true',
    })
    expect(result.success).toBe(false)
  })
})

describe('BoardCommandFrameSchema', () => {
  it('accepts a line frame with arbitrary raw payload', () => {
    const result = BoardCommandFrameSchema.safeParse({ type: 'line', raw: { type: 'result', result: 'done' } })
    expect(result.success).toBe(true)
  })

  it('accepts an error frame', () => {
    const result = BoardCommandFrameSchema.safeParse({ type: 'error', message: 'boom' })
    expect(result.success).toBe(true)
  })

  it('accepts a done frame', () => {
    const result = BoardCommandFrameSchema.safeParse({ type: 'done' })
    expect(result.success).toBe(true)
  })

  it('accepts a busy frame', () => {
    const result = BoardCommandFrameSchema.safeParse({ type: 'busy' })
    expect(result.success).toBe(true)
  })

  it('rejects an error frame missing message', () => {
    const result = BoardCommandFrameSchema.safeParse({ type: 'error' })
    expect(result.success).toBe(false)
  })

  it('rejects an unknown frame type', () => {
    const result = BoardCommandFrameSchema.safeParse({ type: 'ping' })
    expect(result.success).toBe(false)
  })
})

describe('BoardCommandHistoryEntrySchema', () => {
  it('accepts an entry with arbitrary raw payload', () => {
    const result = BoardCommandHistoryEntrySchema.safeParse({
      at: '2026-08-10T12:00:00Z',
      raw: { type: 'result', result: 'done' },
    })
    expect(result.success).toBe(true)
  })

  it('rejects an entry missing at', () => {
    const result = BoardCommandHistoryEntrySchema.safeParse({ raw: {} })
    expect(result.success).toBe(false)
  })
})

describe('BoardCommandHistoryResponseSchema', () => {
  it('accepts an empty entries array', () => {
    const result = BoardCommandHistoryResponseSchema.safeParse({ entries: [] })
    expect(result.success).toBe(true)
  })

  it('accepts a populated entries array', () => {
    const result = BoardCommandHistoryResponseSchema.safeParse({
      entries: [{ at: '2026-08-10T12:00:00Z', raw: { type: 'result' } }],
    })
    expect(result.success).toBe(true)
  })

  it('rejects a non-array entries field', () => {
    const result = BoardCommandHistoryResponseSchema.safeParse({ entries: {} })
    expect(result.success).toBe(false)
  })
})

describe('BoardStatusEntrySchema', () => {
  it('accepts an entry with every field present', () => {
    const result = BoardStatusEntrySchema.safeParse({
      updated_at: '2026-08-14T12:00:00.123456789Z',
      state: 'working',
      cwd: '/workspace/user/project',
      branch: 'feature/dashboard',
      repo: 'panemux',
      pr_url: 'https://github.com/example/panemux/pull/42',
      last_tool: 'Bash',
      summary: 'Running tests',
    })
    expect(result.success).toBe(true)
  })

  it('accepts an entry with every optional field omitted', () => {
    const result = BoardStatusEntrySchema.safeParse({ updated_at: '2026-08-14T12:00:00Z' })
    expect(result.success).toBe(true)
  })

  it('rejects an entry missing updated_at', () => {
    const result = BoardStatusEntrySchema.safeParse({ state: 'working' })
    expect(result.success).toBe(false)
  })

  it('rejects a non-string state', () => {
    const result = BoardStatusEntrySchema.safeParse({ updated_at: '2026-08-14T12:00:00Z', state: 42 })
    expect(result.success).toBe(false)
  })

  it('accepts an unrecognized state string (agent free text, not an enum)', () => {
    const result = BoardStatusEntrySchema.safeParse({ updated_at: '2026-08-14T12:00:00Z', state: 'something-new' })
    expect(result.success).toBe(true)
  })

  // Regression test for a length cap this schema used to carry. The Go side
  // imposes no limit on any of these fields, so a cap here could only reject
  // payloads the server considers valid — and because entries live inside a
  // z.record, one over-long field failed the entire response, blanking every
  // other pane's status on every poll until that pane reported something
  // shorter. An agent writing a couple of paragraphs of summary is ordinary,
  // not exceptional: the bootstrap instruction gives it no length guidance.
  it.each([
    ['summary', 'summary'],
    ['cwd', 'cwd'],
    ['branch', 'branch'],
    ['repo', 'repo'],
    ['pr_url', 'pr_url'],
    ['last_tool', 'last_tool'],
    ['state', 'state'],
  ])('accepts an arbitrarily long %s', (_name, field) => {
    const result = BoardStatusEntrySchema.safeParse({
      updated_at: '2026-08-14T12:00:00Z',
      [field]: 'x'.repeat(10000),
    })
    expect(result.success).toBe(true)
  })

  it('keeps every other pane readable when one pane reports a very long summary', () => {
    const result = BoardStatusResponseSchema.safeParse({
      statuses: {
        chatty: { updated_at: '2026-08-14T12:00:00Z', summary: 'x'.repeat(10000) },
        quiet: { updated_at: '2026-08-14T12:00:00Z', state: 'idle' },
      },
    })
    expect(result.success).toBe(true)
  })
})

describe('BoardStatusResponseSchema', () => {
  it('accepts an empty statuses map', () => {
    const result = BoardStatusResponseSchema.safeParse({ statuses: {} })
    expect(result.success).toBe(true)
  })

  it('accepts a populated statuses map keyed by pane id', () => {
    const result = BoardStatusResponseSchema.safeParse({
      statuses: { main: { updated_at: '2026-08-14T12:00:00Z', state: 'idle' } },
    })
    expect(result.success).toBe(true)
  })

  it('rejects statuses given as an array instead of a map', () => {
    const result = BoardStatusResponseSchema.safeParse({
      statuses: [{ updated_at: '2026-08-14T12:00:00Z' }],
    })
    expect(result.success).toBe(false)
  })

  it('rejects a response missing statuses', () => {
    const result = BoardStatusResponseSchema.safeParse({})
    expect(result.success).toBe(false)
  })
})

describe('BoardMessageSchema', () => {
  it('accepts a fully populated message', () => {
    const result = BoardMessageSchema.safeParse({
      at: '2026-08-14T12:00:00Z',
      host: 'devbox',
      team: 'panemux',
      from: 'claude-main',
      to: '_system',
      body: '{"kind":"board_status"}',
      seq: 7,
      is_status: true,
    })
    expect(result.success).toBe(true)
  })

  it('accepts an arbitrarily long body (no max, unlike other free-text fields)', () => {
    // Deliberately no .max() here: Zod's .max() rejects rather than
    // truncates, so a cap would let one oversized message fail parsing for
    // the whole feed. See useBoardStatus's fetch handling for how a single
    // bad row is expected to be tolerated instead.
    const result = BoardMessageSchema.safeParse({
      at: '2026-08-14T12:00:00Z',
      host: 'devbox',
      team: 'panemux',
      from: 'claude-main',
      to: 'claude-side',
      body: 'x'.repeat(10000),
      seq: 1,
      is_status: false,
    })
    expect(result.success).toBe(true)
  })

  it('rejects a non-integer seq', () => {
    const result = BoardMessageSchema.safeParse({
      at: '2026-08-14T12:00:00Z',
      host: 'devbox',
      team: 'panemux',
      from: 'claude-main',
      to: 'claude-side',
      body: 'hi',
      seq: 'seven',
      is_status: false,
    })
    expect(result.success).toBe(false)
  })

  it('rejects a message missing from', () => {
    const result = BoardMessageSchema.safeParse({
      at: '2026-08-14T12:00:00Z',
      host: 'devbox',
      team: 'panemux',
      to: 'claude-side',
      body: 'hi',
      seq: 1,
      is_status: false,
    })
    expect(result.success).toBe(false)
  })
})

describe('BoardMessagesResponseSchema', () => {
  it('accepts an empty messages array', () => {
    const result = BoardMessagesResponseSchema.safeParse({ messages: [], epoch: 'cache-1' })
    expect(result.success).toBe(true)
  })

  it('accepts a populated messages array', () => {
    const result = BoardMessagesResponseSchema.safeParse({
      messages: [
        { at: '2026-08-14T12:00:00Z', host: 'devbox', team: 'panemux', from: 'a', to: 'b', body: 'hi', seq: 1, is_status: false },
      ],
      epoch: 'cache-1',
    })
    expect(result.success).toBe(true)
  })

  it('rejects a non-array messages field', () => {
    const result = BoardMessagesResponseSchema.safeParse({ messages: {}, epoch: 'cache-1' })
    expect(result.success).toBe(false)
  })

  it('rejects a response missing epoch', () => {
    // epoch is what lets a client notice the server-side cache restarted and
    // renumbered its seq values; without it a stale cursor silently freezes
    // the feed, so it is required rather than optional.
    const result = BoardMessagesResponseSchema.safeParse({ messages: [] })
    expect(result.success).toBe(false)
  })
})

describe('PaneConfigSchema agent_board round-trip', () => {
  // Regression test for silent config loss: the layout tree is parsed with
  // this schema and PUT back wholesale on any edit, so a field the schema
  // drops is deleted from config.yaml by an unrelated action — verified
  // against a real server, where one browser-shaped layout PUT removed a
  // pane's agent_board entirely and turned the dashboard off.
  it('preserves agent_board through a parse', () => {
    const pane = {
      id: 'api',
      type: 'local' as const,
      title: 'API',
      agent_board: { enabled: true, mode: 'turn' as const },
    }

    const parsed = PaneConfigSchema.parse(pane)

    expect(parsed.agent_board).toEqual({ enabled: true, mode: 'turn' })
  })

  it('accepts a pane with no agent_board at all', () => {
    const parsed = PaneConfigSchema.parse({ id: 'api', type: 'local' })
    expect(parsed.agent_board).toBeUndefined()
  })

  it('accepts every mode the backend validates', () => {
    for (const mode of ['monitor', 'turn', 'both', 'off']) {
      const parsed = PaneConfigSchema.parse({ id: 'api', type: 'local', agent_board: { enabled: true, mode } })
      expect(parsed.agent_board?.mode).toBe(mode)
    }
  })

  it('rejects a mode the backend does not know', () => {
    const result = PaneConfigSchema.safeParse({
      id: 'api',
      type: 'local',
      agent_board: { enabled: true, mode: 'watch' },
    })
    expect(result.success).toBe(false)
  })

  it('rejects a non-boolean enabled', () => {
    const result = PaneConfigSchema.safeParse({ id: 'api', type: 'local', agent_board: { enabled: 'yes' } })
    expect(result.success).toBe(false)
  })

  it('survives a full layout parse, not just a bare pane', () => {
    const layout = {
      direction: 'horizontal' as const,
      children: [
        { size: 100, pane: { id: 'api', type: 'local' as const, agent_board: { enabled: true, mode: 'both' as const } } },
      ],
    }

    const parsed = LayoutNodeSchema.parse(layout)

    expect(parsed.children[0].pane?.agent_board).toEqual({ enabled: true, mode: 'both' })
  })
})

// Issue #199 review. normalizeLayoutNode relocates a root pane only when the
// node has no children — the `{pane, children}` shape is left alone, because
// prepending the pane to children that already sum to 100 would rescale every
// sibling and surface a pane that has never rendered. So the server does emit
// a root `pane`, and an undeclared key here is not merely unread: parse()
// strips it, useLayout stores the stripped tree, and the next split PUTs it
// back — deleting the pane from the user's config.yaml. That is the failure
// mode the PaneConfigSchema comment records agent_board being lost to.
describe('LayoutNodeSchema root pane round-trip', () => {
  const withRootPane = {
    pane: { id: 'root', type: 'local' as const },
    direction: 'horizontal' as const,
    children: [{ size: 100, pane: { id: 'a', type: 'local' as const } }],
  }

  it('accepts a root pane sitting beside children', () => {
    expect(LayoutNodeSchema.safeParse(withRootPane).success).toBe(true)
  })

  it('does not strip the root pane, which would delete it from config.yaml', () => {
    expect(LayoutNodeSchema.parse(withRootPane)).toEqual(withRootPane)
  })

  it('still accepts a node with no root pane, the shape normalization produces', () => {
    const relocated = {
      direction: 'vertical' as const,
      children: [{ size: 100, pane: { id: 'a', type: 'local' as const } }],
    }
    expect(LayoutNodeSchema.parse(relocated)).toEqual(relocated)
  })
})
