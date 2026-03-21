import { describe, it, expect } from 'vitest'
import {
  DisplayConfigSchema,
  PaneConfigSchema,
  LayoutNodeSchema,
  LayoutChildSchema,
  SessionInfoSchema,
  WSControlMessageSchema,
} from './index'

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

  it('rejects unknown type', () => {
    const result = WSControlMessageSchema.safeParse({ type: 'unknown' })
    expect(result.success).toBe(false)
  })
})
