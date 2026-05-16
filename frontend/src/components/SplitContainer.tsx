import React, { useCallback, useContext, useRef } from 'react'
import { DisplayConfig, LayoutChild, LayoutNode } from '../types'
import type { PaneEdge } from '../utils/layoutTree'
import { SplitDivider } from './SplitDivider'
import { TerminalPane } from './TerminalPane'

const SPLIT_DIVIDER_THICKNESS = 4
export const WORKSPACE_DROP_ZONE_THICKNESS = 20
export const DIVIDER_DROP_ZONE_THICKNESS = 24

export interface LayoutActionsContextValue {
  onSplit: (paneId: string, direction: 'horizontal' | 'vertical') => void
  onClose: (paneId: string) => void
  onMaximize: (paneId: string | null) => void
  onSettings: (paneId: string) => void
  onSwapPanes: (paneIdA: string, paneIdB: string) => void
  onCreatePaneAtWorkspaceEdge: (edge: PaneEdge) => void
  onCreatePaneBeside: (targetPaneId: string, edge: PaneEdge) => void
  onMovePaneToWorkspaceEdge: (sourcePaneId: string, edge: PaneEdge) => void
  onMovePaneBeside: (sourcePaneId: string, targetPaneId: string, edge: PaneEdge) => void
  maximizedPaneId: string | null
  dragSourcePaneId: string | null
  setDragSourcePaneId: (id: string | null) => void
  displayConfig: DisplayConfig
  onPaneAttention: (paneId: string) => void
  clearPaneAttention: (paneId: string) => void
  hasPaneAttention: (paneId: string) => boolean
}

export const LayoutActionsContext = React.createContext<LayoutActionsContextValue | null>(null)

interface SplitContainerProps {
  layout: LayoutNode
  onLayoutChange: (updated: LayoutNode) => void
}

export const SplitContainer: React.FC<SplitContainerProps> = ({ layout, onLayoutChange }) => {
  const layoutCtx = useContext(LayoutActionsContext)
  const [hoveredWorkspaceEdge, setHoveredWorkspaceEdge] = React.useState<PaneEdge | null>(null)
  const [workspaceInsertEdge, setWorkspaceInsertEdge] = React.useState<PaneEdge | null>(null)
  return (
    <div
      onMouseLeave={() => setWorkspaceInsertEdge(null)}
      onMouseMove={(e) => {
        if (layoutCtx?.dragSourcePaneId || layoutCtx?.maximizedPaneId) {
          setWorkspaceInsertEdge(null)
          return
        }
        const rect = e.currentTarget.getBoundingClientRect()
        const edge = resolveWorkspaceInsertEdge(rect, e.clientX, e.clientY)
        setWorkspaceInsertEdge(edge)
      }}
      style={{ position: 'relative', width: '100%', height: '100%' }}
    >
      <LayoutRenderer
        direction={layout.direction}
        children={layout.children}
        onChildrenChange={(children) => onLayoutChange({ ...layout, children })}
      />
      {!layoutCtx?.dragSourcePaneId && !layoutCtx?.maximizedPaneId && workspaceInsertEdge && (
        <button
          type="button"
          aria-label={`Add pane on workspace ${workspaceInsertEdge}`}
          title="Add pane"
          onClick={() => layoutCtx?.onCreatePaneAtWorkspaceEdge(workspaceInsertEdge)}
          style={workspaceInsertButtonStyle(workspaceInsertEdge)}
        >
          +
        </button>
      )}
      {layoutCtx?.dragSourcePaneId && (
        <>
          {(['top', 'bottom', 'left', 'right'] as PaneEdge[]).map((edge) => (
            <div
              key={edge}
              data-workspace-drop-edge={edge}
              onDragOver={(e) => {
                e.preventDefault()
                e.dataTransfer.dropEffect = 'move'
              }}
              onDrop={(e) => {
                e.preventDefault()
                layoutCtx.onMovePaneToWorkspaceEdge(layoutCtx.dragSourcePaneId!, edge)
                layoutCtx.setDragSourcePaneId(null)
              }}
              onMouseEnter={() => {
                if (!layoutCtx?.dragSourcePaneId) return
                setHoveredWorkspaceEdge(edge)
              }}
              onMouseLeave={() => setHoveredWorkspaceEdge((current) => (current === edge ? null : current))}
              onMouseUp={() => {
                if (!layoutCtx?.dragSourcePaneId) return
                layoutCtx.onMovePaneToWorkspaceEdge(layoutCtx.dragSourcePaneId, edge)
                layoutCtx.setDragSourcePaneId(null)
                setHoveredWorkspaceEdge(null)
              }}
              style={workspaceDropZoneStyle(edge, hoveredWorkspaceEdge === edge)}
            />
          ))}
        </>
      )}
    </div>
  )
}

interface LayoutRendererProps {
  direction: 'horizontal' | 'vertical'
  children: LayoutChild[]
  onChildrenChange: (children: LayoutChild[]) => void
}

export const LayoutRenderer: React.FC<LayoutRendererProps> = ({ direction, children, onChildrenChange }) => {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const isHorizontal = direction === 'horizontal'
  const layoutCtx = useContext(LayoutActionsContext)

  const handleDrag = useCallback((index: number, delta: number) => {
    if (!containerRef.current) return

    const containerSize = isHorizontal
      ? containerRef.current.offsetWidth
      : containerRef.current.offsetHeight
    const dividerPx = SPLIT_DIVIDER_THICKNESS * (children.length - 1)
    const usableSize = containerSize - dividerPx
    if (usableSize <= 0) return

    const deltaPercent = (delta / usableSize) * 100
    const newChildren = children.map((c) => ({ ...c }))
    const newA = newChildren[index].size + deltaPercent
    const newB = newChildren[index + 1].size - deltaPercent

    if (newA < 5 || newB < 5) return
    newChildren[index].size = newA
    newChildren[index + 1].size = newB
    onChildrenChange(newChildren)
  }, [children, isHorizontal, onChildrenChange])

  return (
    <div
      ref={containerRef}
      style={{
        display: 'flex',
        flexDirection: isHorizontal ? 'row' : 'column',
        width: '100%',
        height: '100%',
        overflow: 'hidden',
      }}
    >
      {children.map((child, index) => {
        const key = child.pane?.id ?? `split-${direction}-${index}`
        const isLast = index === children.length - 1
        const isMaximized = child.pane?.id !== undefined && child.pane.id === layoutCtx?.maximizedPaneId
        return (
          <React.Fragment key={key}>
            <div
              style={{
                flexBasis: `${child.size}%`,
                flexShrink: 0,
                flexGrow: 0,
                overflow: 'hidden',
                ...(isHorizontal ? { minWidth: 50 } : { minHeight: 50 }),
                ...(isMaximized ? {
                  position: 'absolute',
                  inset: 0,
                  zIndex: 10,
                  backgroundColor: '#1a1b1e',
                } : {}),
              }}
            >
              <ChildRenderer
                child={child}
                onChildChange={(updated) => {
                  const newChildren = [...children]
                  newChildren[index] = updated
                  onChildrenChange(newChildren)
                }}
              />
            </div>
            {!isLast && !layoutCtx?.maximizedPaneId && (
              <DividerDropZone
                direction={direction}
                beforeChild={child}
                afterChild={children[index + 1]}
                onResize={(d) => handleDrag(index, d)}
              />
            )}
          </React.Fragment>
        )
      })}
    </div>
  )
}

interface ChildRendererProps {
  child: LayoutChild
  onChildChange: (updated: LayoutChild) => void
}

const ChildRenderer: React.FC<ChildRendererProps> = ({ child, onChildChange }) => {
  if (child.pane && (!child.children || child.children.length === 0)) {
    return <TerminalPane pane={child.pane} />
  }

  if (child.direction && child.children?.length) {
    return (
      <LayoutRenderer
        direction={child.direction}
        children={child.children}
        onChildrenChange={(newChildren) =>
          onChildChange({ ...child, children: newChildren })
        }
      />
    )
  }

  return null
}

interface DividerDropZoneProps {
  direction: 'horizontal' | 'vertical'
  beforeChild: LayoutChild
  afterChild: LayoutChild
  onResize: (delta: number) => void
}

const DividerDropZone: React.FC<DividerDropZoneProps> = ({ direction, beforeChild, afterChild, onResize }) => {
  const ctx = useContext(LayoutActionsContext)
  const [hovered, setHovered] = React.useState(false)
  const [showInsertButton, setShowInsertButton] = React.useState(false)
  const edge: PaneEdge = direction === 'horizontal' ? 'right' : 'bottom'
  const targetPaneId = findBoundaryPaneId(beforeChild, 'end') ?? findBoundaryPaneId(afterChild, 'start')

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    const sourceId = ctx?.dragSourcePaneId
    if (!sourceId) return
    if (!targetPaneId) return
    ctx?.onMovePaneBeside(sourceId, targetPaneId, edge)
    ctx?.setDragSourcePaneId(null)
    setHovered(false)
  }

  return (
    <div
      onMouseEnter={() => {
        if (ctx?.dragSourcePaneId || ctx?.maximizedPaneId || !targetPaneId) return
        setShowInsertButton(true)
      }}
      onMouseLeave={() => setShowInsertButton(false)}
      style={{ position: 'relative', flexShrink: 0 }}
    >
      {!ctx?.dragSourcePaneId && !ctx?.maximizedPaneId && showInsertButton && targetPaneId && (
        <button
          type="button"
          aria-label={`Add pane ${edge} of divider`}
          title="Add pane"
          onClick={() => ctx?.onCreatePaneBeside(targetPaneId, edge)}
          style={dividerInsertButtonStyle(direction)}
        >
          +
        </button>
      )}
      <div
        data-divider-drop-zone={direction}
        onDragOver={(e) => {
          if (!ctx?.dragSourcePaneId) return
          e.preventDefault()
          e.dataTransfer.dropEffect = 'move'
          setHovered(true)
        }}
        onDragLeave={() => setHovered(false)}
        onDrop={handleDrop}
        onMouseEnter={() => {
          if (!ctx?.dragSourcePaneId) return
          setHovered(true)
        }}
        onMouseLeave={() => setHovered(false)}
        onMouseUp={() => {
          if (!ctx?.dragSourcePaneId) return
          const sourceId = ctx.dragSourcePaneId
          const targetPaneId = findBoundaryPaneId(beforeChild, 'end') ?? findBoundaryPaneId(afterChild, 'start')
          if (!targetPaneId) return
          ctx.onMovePaneBeside(sourceId, targetPaneId, edge)
          ctx.setDragSourcePaneId(null)
          setHovered(false)
        }}
        style={dividerHitAreaStyle(direction, Boolean(ctx?.dragSourcePaneId))}
      />
      <SplitDivider direction={direction} onDrag={onResize} />
      {ctx?.dragSourcePaneId && (
        <div
          style={dividerOverlayStyle(direction, hovered)}
        />
      )}
    </div>
  )
}

function findBoundaryPaneId(child: LayoutChild, side: 'start' | 'end'): string | null {
  if (child.pane?.id) return child.pane.id
  if (!child.children?.length) return null
  const next = side === 'start' ? child.children[0] : child.children[child.children.length - 1]
  return findBoundaryPaneId(next, side)
}

export function workspaceDropZoneStyle(edge: PaneEdge, active = false): React.CSSProperties {
  const common: React.CSSProperties = {
    position: 'absolute',
    zIndex: 20,
    backgroundColor: active ? 'rgba(86, 156, 214, 0.2)' : 'transparent',
    transition: 'background-color 0.15s ease',
  }
  switch (edge) {
    case 'top':
      return { ...common, top: 0, left: 0, right: 0, height: WORKSPACE_DROP_ZONE_THICKNESS }
    case 'bottom':
      return { ...common, bottom: 0, left: 0, right: 0, height: WORKSPACE_DROP_ZONE_THICKNESS }
    case 'left':
      return { ...common, top: 0, bottom: 0, left: 0, width: WORKSPACE_DROP_ZONE_THICKNESS }
    case 'right':
      return { ...common, top: 0, bottom: 0, right: 0, width: WORKSPACE_DROP_ZONE_THICKNESS }
  }
}

export function dividerOverlayStyle(direction: 'horizontal' | 'vertical', active: boolean): React.CSSProperties {
  const offset = -((DIVIDER_DROP_ZONE_THICKNESS - SPLIT_DIVIDER_THICKNESS) / 2)
  return direction === 'horizontal'
    ? {
        position: 'absolute',
        inset: 0,
        width: DIVIDER_DROP_ZONE_THICKNESS,
        marginLeft: offset,
        backgroundColor: active ? 'rgba(86, 156, 214, 0.35)' : 'transparent',
        pointerEvents: 'none',
      }
    : {
        position: 'absolute',
        inset: 0,
        height: DIVIDER_DROP_ZONE_THICKNESS,
        marginTop: offset,
        backgroundColor: active ? 'rgba(86, 156, 214, 0.35)' : 'transparent',
        pointerEvents: 'none',
      }
}

export function dividerHitAreaStyle(direction: 'horizontal' | 'vertical', active: boolean): React.CSSProperties {
  const offset = -((DIVIDER_DROP_ZONE_THICKNESS - SPLIT_DIVIDER_THICKNESS) / 2)
  const common: React.CSSProperties = {
    position: 'absolute',
    zIndex: 15,
    pointerEvents: active ? 'auto' : 'none',
    backgroundColor: 'transparent',
  }

  return direction === 'horizontal'
    ? {
        ...common,
        top: 0,
        bottom: 0,
        left: offset,
        width: DIVIDER_DROP_ZONE_THICKNESS,
      }
    : {
        ...common,
        left: 0,
        right: 0,
        top: offset,
        height: DIVIDER_DROP_ZONE_THICKNESS,
      }
}

function workspaceInsertButtonStyle(edge: PaneEdge): React.CSSProperties {
  const common: React.CSSProperties = {
    position: 'absolute',
    zIndex: 12,
    width: 20,
    height: 20,
    border: '1px solid #4e5968',
    borderRadius: 4,
    backgroundColor: '#2f3540',
    color: '#d7dce5',
    cursor: 'pointer',
    padding: 0,
    lineHeight: '18px',
    textAlign: 'center',
    fontSize: 14,
    boxShadow: '0 4px 12px rgba(0, 0, 0, 0.25)',
  }
  switch (edge) {
    case 'top':
      return { ...common, top: 4, left: '50%', transform: 'translateX(-50%)' }
    case 'bottom':
      return { ...common, bottom: 4, left: '50%', transform: 'translateX(-50%)' }
    case 'left':
      return { ...common, left: 4, top: '50%', transform: 'translateY(-50%)' }
    case 'right':
      return { ...common, right: 4, top: '50%', transform: 'translateY(-50%)' }
  }
}

function resolveWorkspaceInsertEdge(
  rect: DOMRect | Pick<DOMRect, 'left' | 'top' | 'right' | 'bottom' | 'width' | 'height'>,
  clientX: number,
  clientY: number,
): PaneEdge | null {
  const edgeThreshold = 18
  const withinHorizontalBand = clientX >= rect.left + rect.width * 0.25 && clientX <= rect.right - rect.width * 0.25
  const withinVerticalBand = clientY >= rect.top + rect.height * 0.25 && clientY <= rect.bottom - rect.height * 0.25

  if (clientY <= rect.top + edgeThreshold && withinHorizontalBand) return 'top'
  if (clientY >= rect.bottom - edgeThreshold && withinHorizontalBand) return 'bottom'
  if (clientX <= rect.left + edgeThreshold && withinVerticalBand) return 'left'
  if (clientX >= rect.right - edgeThreshold && withinVerticalBand) return 'right'
  return null
}

function dividerInsertButtonStyle(direction: 'horizontal' | 'vertical'): React.CSSProperties {
  return {
    position: 'absolute',
    zIndex: 16,
    width: 20,
    height: 20,
    border: '1px solid #4e5968',
    borderRadius: 4,
    backgroundColor: '#2f3540',
    color: '#d7dce5',
    cursor: 'pointer',
    padding: 0,
    lineHeight: '18px',
    textAlign: 'center',
    fontSize: 14,
    boxShadow: '0 4px 12px rgba(0, 0, 0, 0.25)',
    top: direction === 'horizontal' ? '50%' : 'calc(50% - 10px)',
    left: direction === 'horizontal' ? 'calc(50% - 10px)' : '50%',
    transform: 'translate(-50%, -50%)',
  }
}
