import { describe, it, expect } from 'vitest'
import { splitPaneInTree, removePaneFromTree, generatePaneId, findPaneById, replacePaneInTree } from './layoutTree'
import type { LayoutNode } from '../schemas'

const simpleLayout: LayoutNode = {
  direction: 'horizontal',
  children: [{ size: 100, pane: { id: 'main', type: 'local' } }],
}

const twoChildLayout: LayoutNode = {
  direction: 'horizontal',
  children: [
    { size: 50, pane: { id: 'left', type: 'local' } },
    { size: 50, pane: { id: 'right', type: 'local' } },
  ],
}

describe('splitPaneInTree', () => {
  it('splits a leaf pane into two children', () => {
    const result = splitPaneInTree(simpleLayout, 'main', 'horizontal', {
      id: 'new-pane',
      type: 'local',
    })
    expect(result.children).toHaveLength(1)
    const split = result.children[0]
    expect(split.direction).toBe('horizontal')
    expect(split.children).toHaveLength(2)
    expect(split.children![0].pane?.id).toBe('main')
    expect(split.children![1].pane?.id).toBe('new-pane')
    expect(split.children![0].size).toBe(50)
    expect(split.children![1].size).toBe(50)
  })

  it('preserves parent size when splitting', () => {
    const result = splitPaneInTree(twoChildLayout, 'left', 'vertical', {
      id: 'new-pane',
      type: 'local',
    })
    expect(result.children[0].size).toBe(50)
    expect(result.children[0].direction).toBe('vertical')
  })

  it('splits a nested pane correctly', () => {
    const nested: LayoutNode = {
      direction: 'horizontal',
      children: [
        { size: 50, pane: { id: 'left', type: 'local' } },
        {
          size: 50,
          direction: 'vertical',
          children: [
            { size: 50, pane: { id: 'top-right', type: 'local' } },
            { size: 50, pane: { id: 'bottom-right', type: 'local' } },
          ],
        },
      ],
    }
    const result = splitPaneInTree(nested, 'top-right', 'horizontal', {
      id: 'new-pane',
      type: 'local',
    })
    const rightSplit = result.children[1]
    const topRight = rightSplit.children![0]
    expect(topRight.direction).toBe('horizontal')
    expect(topRight.children).toHaveLength(2)
  })

  it('returns unchanged layout when pane not found', () => {
    const result = splitPaneInTree(simpleLayout, 'nonexistent', 'horizontal', {
      id: 'new-pane',
      type: 'local',
    })
    expect(result.children).toHaveLength(1)
    expect(result.children[0].pane?.id).toBe('main')
  })
})

describe('removePaneFromTree', () => {
  it('returns null when the last pane is removed', () => {
    const result = removePaneFromTree(simpleLayout, 'main')
    expect(result).toBeNull()
  })

  it('removes a leaf pane and resizes siblings to 100', () => {
    const result = removePaneFromTree(twoChildLayout, 'left')
    expect(result).not.toBeNull()
    expect(result!.children).toHaveLength(1)
    expect(result!.children[0].pane?.id).toBe('right')
    expect(result!.children[0].size).toBe(100)
  })

  it('removes correct pane when multiple exist', () => {
    const result = removePaneFromTree(twoChildLayout, 'right')
    expect(result!.children).toHaveLength(1)
    expect(result!.children[0].pane?.id).toBe('left')
  })

  it('collapses a split with a single remaining child', () => {
    const nested: LayoutNode = {
      direction: 'horizontal',
      children: [
        { size: 50, pane: { id: 'left', type: 'local' } },
        {
          size: 50,
          direction: 'vertical',
          children: [
            { size: 50, pane: { id: 'top', type: 'local' } },
            { size: 50, pane: { id: 'bottom', type: 'local' } },
          ],
        },
      ],
    }
    const result = removePaneFromTree(nested, 'top')
    expect(result!.children).toHaveLength(2)
    // The second child should be collapsed to just the 'bottom' pane
    expect(result!.children[1].pane?.id).toBe('bottom')
    expect(result!.children[1].direction).toBeUndefined()
  })

  it('returns unchanged layout when pane not found', () => {
    const result = removePaneFromTree(twoChildLayout, 'nonexistent')
    expect(result!.children).toHaveLength(2)
  })
})

describe('findPaneById', () => {
  it('finds a top-level pane', () => {
    const result = findPaneById(simpleLayout, 'main')
    expect(result?.id).toBe('main')
  })

  it('finds a pane among siblings', () => {
    const result = findPaneById(twoChildLayout, 'right')
    expect(result?.id).toBe('right')
  })

  it('finds a deeply nested pane', () => {
    const nested: LayoutNode = {
      direction: 'horizontal',
      children: [
        { size: 50, pane: { id: 'left', type: 'local' } },
        {
          size: 50,
          direction: 'vertical',
          children: [
            { size: 50, pane: { id: 'top-right', type: 'local' } },
            { size: 50, pane: { id: 'bottom-right', type: 'local' } },
          ],
        },
      ],
    }
    const result = findPaneById(nested, 'bottom-right')
    expect(result?.id).toBe('bottom-right')
  })

  it('returns null when pane not found', () => {
    const result = findPaneById(simpleLayout, 'nonexistent')
    expect(result).toBeNull()
  })
})

describe('replacePaneInTree', () => {
  it('replaces a matching pane at root level', () => {
    const result = replacePaneInTree(simpleLayout, { id: 'main', type: 'ssh', connection: 'prod' })
    expect(result.children[0].pane?.type).toBe('ssh')
    expect(result.children[0].pane?.connection).toBe('prod')
  })

  it('replaces a matching pane nested inside a split', () => {
    const nested: LayoutNode = {
      direction: 'horizontal',
      children: [
        { size: 50, pane: { id: 'left', type: 'local' } },
        {
          size: 50,
          direction: 'vertical',
          children: [
            { size: 50, pane: { id: 'top-right', type: 'local' } },
            { size: 50, pane: { id: 'bottom-right', type: 'local' } },
          ],
        },
      ],
    }
    const result = replacePaneInTree(nested, { id: 'bottom-right', type: 'tmux', tmux_session: 'mysess' })
    const bottomRight = result.children[1].children![1]
    expect(bottomRight.pane?.type).toBe('tmux')
    expect(bottomRight.pane?.tmux_session).toBe('mysess')
  })

  it('returns tree unchanged when id not found', () => {
    const result = replacePaneInTree(simpleLayout, { id: 'nonexistent', type: 'ssh' })
    expect(result.children[0].pane?.id).toBe('main')
    expect(result.children[0].pane?.type).toBe('local')
  })
})

describe('generatePaneId', () => {
  it('returns a non-empty string', () => {
    const id = generatePaneId()
    expect(typeof id).toBe('string')
    expect(id.length).toBeGreaterThan(0)
  })

  it('returns unique ids on successive calls', () => {
    const ids = new Set(Array.from({ length: 10 }, () => generatePaneId()))
    expect(ids.size).toBe(10)
  })
})
