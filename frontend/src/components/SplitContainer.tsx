import React, { useCallback, useContext, useRef } from 'react'
import { DisplayConfig, LayoutChild, LayoutNode } from '../types'
import { SplitDivider } from './SplitDivider'
import { TerminalPane } from './TerminalPane'

export interface LayoutActionsContextValue {
  onSplit: (paneId: string, direction: 'horizontal' | 'vertical') => void
  onClose: (paneId: string) => void
  onMaximize: (paneId: string | null) => void
  maximizedPaneId: string | null
  displayConfig: DisplayConfig
  editMode: boolean
}

export const LayoutActionsContext = React.createContext<LayoutActionsContextValue | null>(null)

interface SplitContainerProps {
  layout: LayoutNode
  onLayoutChange: (updated: LayoutNode) => void
}

export const SplitContainer: React.FC<SplitContainerProps> = ({ layout, onLayoutChange }) => {
  return (
    <LayoutRenderer
      direction={layout.direction}
      children={layout.children}
      onChildrenChange={(children) => onLayoutChange({ ...layout, children })}
    />
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
    const dividerPx = 4 * (children.length - 1)
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
              <SplitDivider direction={direction} onDrag={(d) => handleDrag(index, d)} />
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
