import { LayoutChild, LayoutNode, PaneConfig } from '../schemas'

export type PaneEdge = 'top' | 'bottom' | 'left' | 'right'

function edgeAxis(edge: PaneEdge): 'horizontal' | 'vertical' {
  return edge === 'left' || edge === 'right' ? 'horizontal' : 'vertical'
}

/**
 * Splits the pane with `targetPaneId` in the layout tree by replacing it with
 * a new split node containing the original pane (50%) and `newPane` (50%).
 */
export function splitPaneInTree(
  layout: LayoutNode,
  targetPaneId: string,
  direction: 'horizontal' | 'vertical',
  newPane: PaneConfig,
): LayoutNode {
  return {
    ...layout,
    children: splitChildren(layout.children, targetPaneId, direction, newPane),
  }
}

function splitChildren(
  children: LayoutChild[],
  targetPaneId: string,
  direction: 'horizontal' | 'vertical',
  newPane: PaneConfig,
): LayoutChild[] {
  return children.map((child) => {
    if (child.pane?.id === targetPaneId) {
      return {
        size: child.size,
        direction,
        children: [
          { size: 50, pane: child.pane },
          { size: 50, pane: newPane },
        ],
      }
    }
    if (child.children?.length) {
      return {
        ...child,
        children: splitChildren(child.children, targetPaneId, direction, newPane),
      }
    }
    return child
  })
}

/**
 * Removes the pane with `targetPaneId` from the layout tree.
 * Returns null if the last pane is removed.
 * Collapses parent splits with only one remaining child.
 */
export function removePaneFromTree(
  layout: LayoutNode,
  targetPaneId: string,
): LayoutNode | null {
  const newChildren = removeFromChildren(layout.children, targetPaneId)
  if (newChildren.length === 0) return null
  return { ...layout, children: newChildren }
}

function removeFromChildren(
  children: LayoutChild[],
  targetPaneId: string,
): LayoutChild[] {
  const filtered: LayoutChild[] = []

  for (const child of children) {
    if (child.pane?.id === targetPaneId) {
      continue
    }
    if (child.children?.length) {
      const sub = removeFromChildren(child.children, targetPaneId)
      if (sub.length === 0) {
        continue
      }
      if (sub.length === 1) {
        // Collapse single remaining child upward, preserving parent size.
        filtered.push({ ...sub[0], size: child.size })
      } else {
        filtered.push({ ...child, children: sub })
      }
    } else {
      filtered.push(child)
    }
  }

  // Normalize sizes so they sum to 100.
  if (filtered.length > 0) {
    const total = filtered.reduce((s, c) => s + c.size, 0)
    if (total > 0) {
      return filtered.map((c) => ({ ...c, size: (c.size / total) * 100 }))
    }
  }
  return filtered
}

/**
 * Finds and returns the PaneConfig with the given id in the layout tree.
 * Returns null if not found.
 */
export function findPaneById(layout: LayoutNode, paneId: string): PaneConfig | null {
  for (const child of layout.children) {
    if (child.pane?.id === paneId) return child.pane
    if (child.children?.length) {
      const found = findPaneById({ ...layout, children: child.children }, paneId)
      if (found) return found
    }
  }
  return null
}

export function layoutContainsPane(layout: LayoutNode, paneId: string): boolean {
  return findPaneById(layout, paneId) !== null
}

export function collectPanes(layout: LayoutNode): PaneConfig[] {
  const panes: PaneConfig[] = []
  collectPaneChildren(layout.children, panes)
  return panes
}

function collectPaneChildren(children: LayoutChild[], panes: PaneConfig[]) {
  for (const child of children) {
    if (child.pane) panes.push(child.pane)
    if (child.children?.length) collectPaneChildren(child.children, panes)
  }
}

/**
 * Replaces the pane matching `updated.id` in the layout tree with `updated`.
 * Returns the tree unchanged if the id is not found.
 */
export function replacePaneInTree(layout: LayoutNode, updated: PaneConfig): LayoutNode {
  return { ...layout, children: replaceInChildren(layout.children, updated) }
}

function replaceInChildren(children: LayoutChild[], updated: PaneConfig): LayoutChild[] {
  return children.map((child) => {
    if (child.pane?.id === updated.id) return { ...child, pane: updated }
    if (child.children?.length) return { ...child, children: replaceInChildren(child.children, updated) }
    return child
  })
}

/**
 * Swaps the pane configs at the two given pane IDs in the layout tree.
 * Tree structure (sizes, split directions) is preserved; only the pane
 * configs at each position are exchanged.
 * Returns the original layout unchanged if either ID is not found or both IDs are the same.
 */
export function swapPanesInTree(layout: LayoutNode, paneIdA: string, paneIdB: string): LayoutNode {
  if (paneIdA === paneIdB) return layout
  const paneA = findPaneById(layout, paneIdA)
  const paneB = findPaneById(layout, paneIdB)
  if (!paneA || !paneB) return layout
  return { ...layout, children: swapInChildren(layout.children, paneIdA, paneB, paneIdB, paneA) }
}

export function normalizeChildrenSizes(children: LayoutChild[]): LayoutChild[] {
  if (children.length === 0) return children
  const size = 100 / children.length
  return children.map((child) => ({ ...child, size }))
}

export function insertPaneAtWorkspaceEdge(layout: LayoutNode, edge: PaneEdge, newPane: PaneConfig): LayoutNode {
  const axis = edgeAxis(edge)
  const newChild: LayoutChild = { size: 0, pane: newPane }

  if (layout.direction === axis) {
    const nextChildren = edge === 'left' || edge === 'top'
      ? [newChild, ...layout.children]
      : [...layout.children, newChild]
    return {
      ...layout,
      children: normalizeChildrenSizes(nextChildren),
    }
  }

  return {
    direction: axis,
    children: edge === 'left' || edge === 'top'
      ? [
          { size: 50, pane: newPane },
          { size: 50, direction: layout.direction, children: layout.children },
        ]
      : [
          { size: 50, direction: layout.direction, children: layout.children },
          { size: 50, pane: newPane },
        ],
  }
}

export function movePaneToWorkspaceEdge(layout: LayoutNode, sourcePaneId: string, edge: PaneEdge): LayoutNode {
  const sourcePane = findPaneById(layout, sourcePaneId)
  if (!sourcePane) return layout
  const withoutSource = removePaneFromTree(layout, sourcePaneId)
  if (!withoutSource) return layout
  return insertPaneAtWorkspaceEdge(withoutSource, edge, sourcePane)
}

export function insertPaneBesideTargetPane(
  layout: LayoutNode,
  targetPaneId: string,
  edge: PaneEdge,
  newPane: PaneConfig,
): LayoutNode {
  const inserted = insertInChildren(layout.children, layout.direction, targetPaneId, edge, newPane)
  return inserted.inserted ? { ...layout, children: inserted.children } : layout
}

export function movePaneBesideTargetPane(
  layout: LayoutNode,
  sourcePaneId: string,
  targetPaneId: string,
  edge: PaneEdge,
): LayoutNode {
  if (sourcePaneId === targetPaneId) return layout
  const sourcePane = findPaneById(layout, sourcePaneId)
  if (!sourcePane) return layout
  const withoutSource = removePaneFromTree(layout, sourcePaneId)
  if (!withoutSource) return layout
  return insertPaneBesideTargetPane(withoutSource, targetPaneId, edge, sourcePane)
}

function insertInChildren(
  children: LayoutChild[],
  parentDirection: 'horizontal' | 'vertical',
  targetPaneId: string,
  edge: PaneEdge,
  newPane: PaneConfig,
): { children: LayoutChild[]; inserted: boolean } {
  const directIndex = children.findIndex((child) => child.pane?.id === targetPaneId)
  if (directIndex >= 0) {
    return {
      children: insertRelativeToDirectChild(children, parentDirection, directIndex, edge, newPane),
      inserted: true,
    }
  }

  for (let i = 0; i < children.length; i++) {
    const child = children[i]
    if (!child.children?.length || !child.direction) continue
    const nested = insertInChildren(child.children, child.direction, targetPaneId, edge, newPane)
    if (nested.inserted) {
      const nextChildren = [...children]
      nextChildren[i] = { ...child, children: nested.children }
      return { children: nextChildren, inserted: true }
    }
  }

  return { children, inserted: false }
}

function insertRelativeToDirectChild(
  children: LayoutChild[],
  parentDirection: 'horizontal' | 'vertical',
  directIndex: number,
  edge: PaneEdge,
  newPane: PaneConfig,
): LayoutChild[] {
  const axis = edgeAxis(edge)
  const before = edge === 'left' || edge === 'top'

  if (parentDirection === axis) {
    const insertionIndex = before ? directIndex : directIndex + 1
    const nextChildren = [...children]
    nextChildren.splice(insertionIndex, 0, { size: 0, pane: newPane })
    return normalizeChildrenSizes(nextChildren)
  }

  const targetChild = children[directIndex]
  const wrapped: LayoutChild = {
    size: targetChild.size,
    direction: axis,
    children: before
      ? [
          { size: 50, pane: newPane },
          { size: 50, pane: targetChild.pane },
        ]
      : [
          { size: 50, pane: targetChild.pane },
          { size: 50, pane: newPane },
        ],
  }

  const nextChildren = [...children]
  nextChildren[directIndex] = wrapped
  return nextChildren
}

function swapInChildren(
  children: LayoutChild[],
  idA: string,
  paneB: PaneConfig,
  idB: string,
  paneA: PaneConfig,
): LayoutChild[] {
  return children.map((child) => {
    if (child.pane?.id === idA) return { ...child, pane: paneB }
    if (child.pane?.id === idB) return { ...child, pane: paneA }
    if (child.children?.length) return { ...child, children: swapInChildren(child.children, idA, paneB, idB, paneA) }
    return child
  })
}

/**
 * Generates a unique pane ID.
 */
export function generatePaneId(): string {
  return `pane-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
}

/**
 * Generates a unique tmux session name derived from a base name.
 * Result matches ^[a-zA-Z0-9_.-]+$.
 */
export function generateTmuxSessionName(base: string): string {
  return `${base}-${Math.random().toString(36).slice(2, 7)}`
}
