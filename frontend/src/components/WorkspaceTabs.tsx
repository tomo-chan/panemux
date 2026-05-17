import React, { useEffect, useRef, useState } from 'react'
import type { TabPosition, Workspace } from '../types'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface WorkspaceTabsProps {
  workspaces: Workspace[]
  activeWorkspaceId: string
  tabPosition: TabPosition
  onSelect: (workspaceId: string) => void
  dragSourcePaneId?: string | null
  onMovePaneToWorkspace?: (sourcePaneId: string, workspaceId: string) => void
  onAdd?: () => void
  onDelete?: (workspaceId: string) => void
  onRename?: (workspaceId: string, title: string) => void
  onTabPositionChange?: (position: TabPosition) => void
  attentionWorkspaceIds?: ReadonlySet<string>
  onClearAttention?: (workspaceId: string) => void
}

interface InteractiveSurfaceButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  selected?: boolean
  danger?: boolean
}

const InteractiveSurfaceButton = React.forwardRef<HTMLButtonElement, InteractiveSurfaceButtonProps>(function InteractiveSurfaceButton(
  { selected = false, danger = false, style, children, onMouseEnter, onMouseLeave, onMouseDown, onMouseUp, onBlur, ...props },
  ref,
) {
  const [hovered, setHovered] = useState(false)
  const [pressed, setPressed] = useState(false)
  const mergedStyle = style ?? {}
  const baseColor = (mergedStyle.color as string | undefined) ?? '#b8beca'

  let backgroundColor = selected ? '#2f3540' : 'transparent'
  let boxShadow = 'none'
  let color = danger ? '#f08b8b' : baseColor
  let transform = 'translateY(0)'

  if (hovered) {
    backgroundColor = selected ? '#353d4a' : 'rgba(255, 255, 255, 0.07)'
    boxShadow = 'inset 0 0 0 1px rgba(255, 255, 255, 0.06)'
    color = selected ? '#ffffff' : '#d7dce5'
  }

  if (pressed) {
    backgroundColor = selected ? '#39414f' : 'rgba(255, 255, 255, 0.12)'
    boxShadow = 'inset 0 1px 2px rgba(0, 0, 0, 0.45)'
    color = '#ffffff'
    transform = 'translateY(1px)'
  }

  return (
    <button
      {...props}
      ref={ref}
      onMouseEnter={(event) => {
        setHovered(true)
        onMouseEnter?.(event)
      }}
      onMouseLeave={(event) => {
        setHovered(false)
        setPressed(false)
        onMouseLeave?.(event)
      }}
      onMouseDown={(event) => {
        if (event.button === 0) setPressed(true)
        onMouseDown?.(event)
      }}
      onMouseUp={(event) => {
        setPressed(false)
        onMouseUp?.(event)
      }}
      onBlur={(event) => {
        setPressed(false)
        onBlur?.(event)
      }}
      style={{
        ...mergedStyle,
        appearance: 'none',
        border: 'none',
        transition: 'background-color 0.12s ease, box-shadow 0.12s ease, color 0.12s ease, transform 0.12s ease',
        backgroundColor,
        boxShadow,
        color,
        transform,
      }}
    >
      {children}
    </button>
  )
})

export const WorkspaceTabs: React.FC<WorkspaceTabsProps> = ({
  workspaces,
  activeWorkspaceId,
  tabPosition,
  onSelect,
  dragSourcePaneId,
  onMovePaneToWorkspace,
  onAdd,
  onDelete,
  onRename,
  onTabPositionChange,
  attentionWorkspaceIds,
  onClearAttention,
}) => {
  const vertical = tabPosition === 'left' || tabPosition === 'right'
  const showTabs = workspaces.length > 1
  const showBar = showTabs || Boolean(onAdd || onDelete || onRename || onTabPositionChange)
  const [editingWorkspaceId, setEditingWorkspaceId] = useState<string | null>(null)
  const [draftTitle, setDraftTitle] = useState('')
  const [hoveredDropWorkspaceId, setHoveredDropWorkspaceId] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)
  const renameFinalizedRef = useRef(false)

  useEffect(() => {
    if (editingWorkspaceId) {
      inputRef.current?.focus()
      inputRef.current?.select()
    }
  }, [editingWorkspaceId])

  useEffect(() => {
    if (!dragSourcePaneId) {
      setHoveredDropWorkspaceId(null)
    }
  }, [dragSourcePaneId])

  const startRename = (workspace: Workspace) => {
    if (!onRename) return
    renameFinalizedRef.current = false
    setEditingWorkspaceId(workspace.id)
    setDraftTitle(workspace.title)
  }

  const commitRename = (workspace: Workspace) => {
    if (renameFinalizedRef.current) return
    if (editingWorkspaceId !== workspace.id) return
    renameFinalizedRef.current = true
    const title = draftTitle.trim()
    setEditingWorkspaceId(null)
    if (title && title !== workspace.title) {
      onRename?.(workspace.id, title)
    }
  }

  const cancelRename = () => {
    renameFinalizedRef.current = true
    setEditingWorkspaceId(null)
  }

  const moveDraggedPaneToWorkspace = (workspaceId: string) => {
    if (!dragSourcePaneId || workspaceId === activeWorkspaceId) return
    onMovePaneToWorkspace?.(dragSourcePaneId, workspaceId)
    setHoveredDropWorkspaceId(null)
  }

  const positionControls = onTabPositionChange ? (
    <div
      role="group"
      aria-label="Workspace tab position"
      style={{
        display: 'flex',
        borderLeft: !vertical ? '1px solid #333842' : undefined,
        borderTop: vertical ? '1px solid #333842' : undefined,
        flexShrink: 0,
        marginLeft: !vertical ? 'auto' : undefined,
        marginTop: vertical ? 'auto' : undefined,
        alignItems: 'center',
      }}
    >
      <div
        data-testid="workspace-tab-position-cluster"
        style={{
          display: 'flex',
          flexDirection: 'row',
          width: 136,
          height: 34,
        }}
      >
        {([
          ['top', '▲', 'Place workspace tabs at top'],
          ['bottom', '▼', 'Place workspace tabs at bottom'],
          ['left', '◀', 'Place workspace tabs on left'],
          ['right', '▶', 'Place workspace tabs on right'],
        ] as const).map(([position, label, ariaLabel], index) => {
          const selected = tabPosition === position
          const isLast = index === 3
          return (
            <InteractiveSurfaceButton
              key={position}
              type="button"
              aria-label={ariaLabel}
              aria-pressed={selected}
              title={ariaLabel}
              onClick={() => onTabPositionChange(position)}
              selected={selected}
              style={{
                border: 'none',
                borderRight: !isLast ? '1px solid #333842' : undefined,
                color: selected ? '#ffffff' : '#b8beca',
                cursor: selected ? 'default' : 'pointer',
                fontFamily: TERMINAL_FONT_FAMILY,
                fontSize: 12,
                width: 34,
                height: '100%',
                lineHeight: '34px',
                flex: '0 0 34px',
                padding: 0,
                textAlign: 'center',
              }}
            >
              {label}
            </InteractiveSurfaceButton>
          )
        })}
      </div>
    </div>
  ) : null

  if (!showBar) return null

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: vertical ? 'column' : 'row',
        flexShrink: 0,
        backgroundColor: '#202124',
        borderColor: '#333842',
        borderStyle: 'solid',
        borderWidth: tabPosition === 'top'
          ? '0 0 1px 0'
          : tabPosition === 'bottom'
            ? '1px 0 0 0'
            : tabPosition === 'left'
              ? '0 1px 0 0'
              : '0 0 0 1px',
        overflowX: vertical ? 'hidden' : 'auto',
        overflowY: vertical ? 'auto' : 'hidden',
        maxWidth: vertical ? 180 : undefined,
        minWidth: vertical ? 132 : undefined,
      }}
    >
      {showTabs && (
        <div
          role="tablist"
          aria-orientation={vertical ? 'vertical' : 'horizontal'}
          style={{
            display: 'flex',
            flexDirection: vertical ? 'column' : 'row',
            minWidth: 0,
            minHeight: 0,
          }}
        >
          {workspaces.map((workspace) => {
            const active = workspace.id === activeWorkspaceId
            const editing = editingWorkspaceId === workspace.id
            const hasAttention = !active && (attentionWorkspaceIds?.has(workspace.id) ?? false)
            const isWorkspaceDropTarget = Boolean(dragSourcePaneId) && !editing && workspace.id !== activeWorkspaceId
            return (
              <div
                key={workspace.id}
                className={hasAttention ? 'panemux-workspace-tab-attention' : undefined}
                onDragOver={(event) => {
                  if (!isWorkspaceDropTarget) return
                  event.preventDefault()
                  event.dataTransfer.dropEffect = 'move'
                  setHoveredDropWorkspaceId(workspace.id)
                }}
                onDragLeave={() => setHoveredDropWorkspaceId((current) => current === workspace.id ? null : current)}
                onDrop={(event) => {
                  if (!isWorkspaceDropTarget) return
                  event.preventDefault()
                  moveDraggedPaneToWorkspace(workspace.id)
                }}
                onMouseEnter={() => {
                  if (!isWorkspaceDropTarget) return
                  setHoveredDropWorkspaceId(workspace.id)
                }}
                onMouseLeave={() => setHoveredDropWorkspaceId((current) => current === workspace.id ? null : current)}
                onMouseUp={() => {
                  if (!isWorkspaceDropTarget) return
                  moveDraggedPaneToWorkspace(workspace.id)
                }}
                style={{
                  borderRight: !vertical ? '1px solid #333842' : undefined,
                  borderBottom: vertical ? '1px solid #333842' : undefined,
                  backgroundColor: dragSourcePaneId && hoveredDropWorkspaceId === workspace.id
                    ? 'rgba(86, 156, 214, 0.24)'
                    : active
                      ? '#2f3540'
                      : 'transparent',
                  boxShadow: dragSourcePaneId && hoveredDropWorkspaceId === workspace.id
                    ? 'inset 0 0 0 1px rgba(137, 196, 244, 0.45)'
                    : 'none',
                  display: 'flex',
                  alignItems: 'center',
                  height: vertical ? 38 : 34,
                  minWidth: vertical ? '100%' : 96,
                  maxWidth: vertical ? '100%' : 180,
                }}
              >
                {editing ? (
                  <input
                    ref={inputRef}
                    aria-label="Workspace name"
                    value={draftTitle}
                    onChange={(event) => setDraftTitle(event.target.value)}
                    onBlur={() => commitRename(workspace)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') {
                        commitRename(workspace)
                      } else if (event.key === 'Escape') {
                        cancelRename()
                      }
                    }}
                    style={{
                      appearance: 'none',
                      border: '1px solid #5f6b7a',
                      borderRadius: 3,
                      backgroundColor: '#15171a',
                      color: '#ffffff',
                      flex: 1,
                      fontFamily: TERMINAL_FONT_FAMILY,
                      fontSize: 14,
                      height: vertical ? 28 : 26,
                      margin: '0 6px',
                      minWidth: 0,
                      padding: '0 6px',
                    }}
                  />
                ) : (
                  <InteractiveSurfaceButton
                    role="tab"
                    aria-selected={active}
                    data-attention={hasAttention ? 'true' : undefined}
                    onClick={() => {
                      if (dragSourcePaneId) return
                      onClearAttention?.(workspace.id)
                      onSelect(workspace.id)
                    }}
                    onDoubleClick={() => startRename(workspace)}
                    title={workspace.title}
                    selected={active}
                    style={{
                      border: 'none',
                      color: active ? '#ffffff' : '#b8beca',
                      cursor: active ? 'default' : 'pointer',
                      flex: 1,
                      fontFamily: TERMINAL_FONT_FAMILY,
                      fontSize: 13,
                      height: '100%',
                      minWidth: 0,
                      padding: onDelete || onRename ? '0 6px 0 12px' : '0 12px',
                      textAlign: 'left',
                      whiteSpace: 'nowrap',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                    }}
                  >
                    {workspace.title}
                  </InteractiveSurfaceButton>
                )}
                {onRename && !editing && (
                  <InteractiveSurfaceButton
                    type="button"
                    aria-label={`Rename ${workspace.title} workspace`}
                    title="Rename workspace"
                    onClick={() => startRename(workspace)}
                    style={{
                      border: 'none',
                      color: active ? '#d7dce5' : '#8f96a3',
                      cursor: 'pointer',
                      flex: '0 0 28px',
                      fontFamily: TERMINAL_FONT_FAMILY,
                      fontSize: 13,
                      height: '100%',
                      lineHeight: vertical ? '38px' : '34px',
                      padding: 0,
                      textAlign: 'center',
                    }}
                  >
                    ✎
                  </InteractiveSurfaceButton>
                )}
                {onDelete && (
                  <InteractiveSurfaceButton
                    type="button"
                    aria-label={`Delete ${workspace.title} workspace`}
                    title="Delete workspace"
                    onClick={() => onDelete(workspace.id)}
                    danger
                    style={{
                      border: 'none',
                      cursor: 'pointer',
                      flex: '0 0 28px',
                      fontFamily: TERMINAL_FONT_FAMILY,
                      fontSize: 16,
                      height: '100%',
                      lineHeight: vertical ? '38px' : '34px',
                      padding: 0,
                      textAlign: 'center',
                    }}
                  >
                    ×
                  </InteractiveSurfaceButton>
                )}
              </div>
            )
          })}
        </div>
      )}
      {onAdd && (
        <div
          style={{
            borderRight: !vertical ? '1px solid #333842' : undefined,
            borderBottom: vertical ? '1px solid #333842' : undefined,
            backgroundColor: 'transparent',
            display: 'flex',
            alignItems: 'center',
            height: vertical ? 38 : 34,
            minWidth: vertical ? '100%' : 40,
            maxWidth: vertical ? '100%' : 40,
            flexShrink: 0,
          }}
        >
          <InteractiveSurfaceButton
            type="button"
            aria-label="Add workspace"
            title="Add workspace"
            onClick={onAdd}
            style={actionButtonStyle(vertical)}
          >
            +
          </InteractiveSurfaceButton>
        </div>
      )}
      {positionControls}
    </div>
  )
}

function actionButtonStyle(vertical: boolean): React.CSSProperties {
  return {
    appearance: 'none',
    border: 'none',
    backgroundColor: 'transparent',
    color: '#b8beca',
    cursor: 'pointer',
    fontFamily: TERMINAL_FONT_FAMILY,
    fontSize: 16,
    minWidth: vertical ? '100%' : 40,
    height: '100%',
    padding: 0,
    flex: 1,
    textAlign: 'center',
    whiteSpace: 'nowrap',
  }
}
