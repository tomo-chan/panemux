import React, { useEffect, useRef, useState } from 'react'
import type { TabPosition, Workspace } from '../types'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface WorkspaceTabsProps {
  workspaces: Workspace[]
  activeWorkspaceId: string
  tabPosition: TabPosition
  onSelect: (workspaceId: string) => void
  onAdd?: () => void
  onDelete?: (workspaceId: string) => void
  onRename?: (workspaceId: string, title: string) => void
  onTabPositionChange?: (position: TabPosition) => void
  attentionWorkspaceIds?: ReadonlySet<string>
  onClearAttention?: (workspaceId: string) => void
}

export const WorkspaceTabs: React.FC<WorkspaceTabsProps> = ({
  workspaces,
  activeWorkspaceId,
  tabPosition,
  onSelect,
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
  const inputRef = useRef<HTMLInputElement | null>(null)
  const renameFinalizedRef = useRef(false)

  useEffect(() => {
    if (editingWorkspaceId) {
      inputRef.current?.focus()
      inputRef.current?.select()
    }
  }, [editingWorkspaceId])

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

  const surfaceButtonStyle = (base: React.CSSProperties, options?: { selected?: boolean; danger?: boolean }): React.CSSProperties => {
    const selected = options?.selected ?? false
    const danger = options?.danger ?? false
    const baseColor = (base.color as string | undefined) ?? '#b8beca'
    const selectedBackground = selected ? '#2f3540' : 'transparent'
    return {
      ...base,
      appearance: 'none',
      border: 'none',
      transition: 'background-color 0.12s ease, box-shadow 0.12s ease, color 0.12s ease, transform 0.12s ease',
      backgroundColor: selectedBackground,
      color: danger ? '#f08b8b' : baseColor,
      ['--pmx-base-color' as string]: danger ? '#f08b8b' : baseColor,
      ['--pmx-selected-background' as string]: selectedBackground,
    }
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
            <button
              key={position}
              type="button"
              aria-label={ariaLabel}
              aria-pressed={selected}
              title={ariaLabel}
              onClick={() => onTabPositionChange(position)}
              onMouseEnter={(event) => applyInteractiveButtonState(event.currentTarget, 'hover', selected)}
              onMouseLeave={(event) => applyInteractiveButtonState(event.currentTarget, 'rest', selected)}
              onMouseDown={(event) => {
                if (event.button !== 0) return
                applyInteractiveButtonState(event.currentTarget, 'pressed', selected)
              }}
              onMouseUp={(event) => applyInteractiveButtonState(event.currentTarget, 'hover', selected)}
              onBlur={(event) => applyInteractiveButtonState(event.currentTarget, 'rest', selected)}
              style={surfaceButtonStyle({
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
              }, { selected })}
            >
              {label}
            </button>
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
            return (
              <div
                key={workspace.id}
                className={hasAttention ? 'panemux-workspace-tab-attention' : undefined}
                style={{
                  borderRight: !vertical ? '1px solid #333842' : undefined,
                  borderBottom: vertical ? '1px solid #333842' : undefined,
                  backgroundColor: active ? '#2f3540' : 'transparent',
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
                  <button
                    role="tab"
                    aria-selected={active}
                    data-attention={hasAttention ? 'true' : undefined}
                    onClick={() => {
                      onClearAttention?.(workspace.id)
                      onSelect(workspace.id)
                    }}
                    onDoubleClick={() => startRename(workspace)}
                    title={workspace.title}
                    onMouseEnter={(event) => applyInteractiveButtonState(event.currentTarget, 'hover', active)}
                    onMouseLeave={(event) => applyInteractiveButtonState(event.currentTarget, 'rest', active)}
                    onMouseDown={(event) => {
                      if (event.button !== 0) return
                      applyInteractiveButtonState(event.currentTarget, 'pressed', active)
                    }}
                    onMouseUp={(event) => applyInteractiveButtonState(event.currentTarget, 'hover', active)}
                    onBlur={(event) => applyInteractiveButtonState(event.currentTarget, 'rest', active)}
                    style={surfaceButtonStyle({
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
                    }, { selected: active })}
                  >
                    {workspace.title}
                  </button>
                )}
                {onRename && !editing && (
                  <button
                    type="button"
                    aria-label={`Rename ${workspace.title} workspace`}
                    title="Rename workspace"
                    onClick={() => startRename(workspace)}
                    onMouseEnter={(event) => applyInteractiveButtonState(event.currentTarget, 'hover', false)}
                    onMouseLeave={(event) => applyInteractiveButtonState(event.currentTarget, 'rest', false)}
                    onMouseDown={(event) => {
                      if (event.button !== 0) return
                      applyInteractiveButtonState(event.currentTarget, 'pressed', false)
                    }}
                    onMouseUp={(event) => applyInteractiveButtonState(event.currentTarget, 'hover', false)}
                    onBlur={(event) => applyInteractiveButtonState(event.currentTarget, 'rest', false)}
                    style={surfaceButtonStyle({
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
                    })}
                  >
                    ✎
                  </button>
                )}
                {onDelete && (
                  <button
                    type="button"
                    aria-label={`Delete ${workspace.title} workspace`}
                    title="Delete workspace"
                    onClick={() => onDelete(workspace.id)}
                    onMouseEnter={(event) => applyInteractiveButtonState(event.currentTarget, 'hover', false)}
                    onMouseLeave={(event) => applyInteractiveButtonState(event.currentTarget, 'rest', false)}
                    onMouseDown={(event) => {
                      if (event.button !== 0) return
                      applyInteractiveButtonState(event.currentTarget, 'pressed', false)
                    }}
                    onMouseUp={(event) => applyInteractiveButtonState(event.currentTarget, 'hover', false)}
                    onBlur={(event) => applyInteractiveButtonState(event.currentTarget, 'rest', false)}
                    style={surfaceButtonStyle({
                      border: 'none',
                      color: active ? '#d7dce5' : '#8f96a3',
                      cursor: 'pointer',
                      flex: '0 0 28px',
                      fontFamily: TERMINAL_FONT_FAMILY,
                      fontSize: 16,
                      height: '100%',
                      lineHeight: vertical ? '38px' : '34px',
                      padding: 0,
                      textAlign: 'center',
                    }, { danger: true })}
                  >
                    ×
                  </button>
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
          <button
            type="button"
            aria-label="Add workspace"
            title="Add workspace"
            onClick={onAdd}
            onMouseEnter={(event) => applyInteractiveButtonState(event.currentTarget, 'hover', false)}
            onMouseLeave={(event) => applyInteractiveButtonState(event.currentTarget, 'rest', false)}
            onMouseDown={(event) => {
              if (event.button !== 0) return
              applyInteractiveButtonState(event.currentTarget, 'pressed', false)
            }}
            onMouseUp={(event) => applyInteractiveButtonState(event.currentTarget, 'hover', false)}
            onBlur={(event) => applyInteractiveButtonState(event.currentTarget, 'rest', false)}
            style={surfaceButtonStyle(actionButtonStyle(vertical))}
          >
            +
          </button>
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

type InteractiveState = 'rest' | 'hover' | 'pressed'

function applyInteractiveButtonState(button: HTMLButtonElement, state: InteractiveState, selected: boolean) {
  const selectedBackground = selected ? '#2f3540' : 'transparent'
  const baseColor = button.style.getPropertyValue('--pmx-base-color') || '#b8beca'

  if (state === 'pressed') {
    button.style.backgroundColor = selected ? '#39414f' : 'rgba(255, 255, 255, 0.12)'
    button.style.boxShadow = 'inset 0 1px 2px rgba(0, 0, 0, 0.45)'
    button.style.color = '#ffffff'
    button.style.transform = 'translateY(1px)'
    return
  }

  if (state === 'hover') {
    button.style.backgroundColor = selected ? '#353d4a' : 'rgba(255, 255, 255, 0.07)'
    button.style.boxShadow = 'inset 0 0 0 1px rgba(255, 255, 255, 0.06)'
    button.style.color = selected ? '#ffffff' : '#d7dce5'
    button.style.transform = 'translateY(0)'
    return
  }

  button.style.backgroundColor = selectedBackground
  button.style.boxShadow = 'none'
  button.style.color = selected ? '#ffffff' : baseColor
  button.style.transform = 'translateY(0)'
}
