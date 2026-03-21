import { LayoutChild, LayoutNode, PaneConfig } from '../schemas'

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

/**
 * Generates a unique pane ID.
 */
export function generatePaneId(): string {
  return `pane-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
}
