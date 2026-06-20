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
  WorkspacesResponseSchema,
  DirectoryEntrySchema,
  DirectoryBrowserResponseSchema,
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
      items: [],
    }).success).toBe(false)

    expect(WorkspacesResponseSchema.safeParse({
      active: '',
      tab_position: 'top',
      items: [
        {
          id: '',
          title: '',
          layout: { direction: 'horizontal', children: [] },
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
