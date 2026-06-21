import React, { useEffect, useRef, useState } from 'react'
import type { TabPosition, Workspace } from '../types'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

export interface WorkspacePaneSummary {
  id: string
  title: string
  type: string
  state: 'connected' | 'disconnected' | 'exited' | 'pending'
  connection?: string
  repo?: string
  branch?: string
  prNumber?: number
  attention: boolean
}

export interface WorkspaceSummary {
  paneCount: number
  connectedCount: number
  disconnectedCount: number
  exitedCount: number
  pendingCount: number
  panes: WorkspacePaneSummary[]
}

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
  workspaceSummaries?: Record<string, WorkspaceSummary>
  onSelectPaneFromSummary?: (workspaceId: string, paneId: string) => void
  onStartPaneDragFromSummary?: (paneId: string) => void
  onEndPaneDragFromSummary?: () => void
  activePaneId?: string | null
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
  workspaceSummaries,
  onSelectPaneFromSummary,
  onStartPaneDragFromSummary,
  onEndPaneDragFromSummary,
  activePaneId,
}) => {
  const vertical = tabPosition === 'left' || tabPosition === 'right'
  const showTabs = workspaces.length > 1
  const showBar = showTabs || Boolean(onAdd || onDelete || onRename || onTabPositionChange)
  const [editingWorkspaceId, setEditingWorkspaceId] = useState<string | null>(null)
  const [draftTitle, setDraftTitle] = useState('')
  const [hoveredDropWorkspaceId, setHoveredDropWorkspaceId] = useState<string | null>(null)
  const [expandedWorkspaceId, setExpandedWorkspaceId] = useState<string | null>(null)
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

  useEffect(() => {
    if (editingWorkspaceId) {
      setExpandedWorkspaceId(editingWorkspaceId)
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

  const moveDraggedPaneToWorkspace = (workspaceId: string) => {
    if (!dragSourcePaneId || workspaceId === activeWorkspaceId) return
    onMovePaneToWorkspace?.(dragSourcePaneId, workspaceId)
    setHoveredDropWorkspaceId(null)
  }

  const workspaceDropHandlers = (workspaceId: string, enabled: boolean) => ({
    onDragOver: (event: React.DragEvent<HTMLElement>) => {
      if (!enabled) return
      event.preventDefault()
      event.dataTransfer.dropEffect = 'move'
      setHoveredDropWorkspaceId(workspaceId)
    },
    onDragLeave: () => setHoveredDropWorkspaceId((current) => current === workspaceId ? null : current),
    onDrop: (event: React.DragEvent<HTMLElement>) => {
      if (!enabled) return
      event.preventDefault()
      moveDraggedPaneToWorkspace(workspaceId)
    },
    onMouseEnter: () => {
      if (!enabled) return
      setHoveredDropWorkspaceId(workspaceId)
    },
    onMouseLeave: () => setHoveredDropWorkspaceId((current) => current === workspaceId ? null : current),
    onMouseUp: () => {
      if (!enabled) return
      moveDraggedPaneToWorkspace(workspaceId)
    },
  })

  const handleSummaryPaneDragStart = (event: React.DragEvent<HTMLButtonElement>, paneId: string) => {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', paneId)
    onStartPaneDragFromSummary?.(paneId)
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
        flexDirection: vertical ? 'column' : 'row',
        overflow: 'visible',
        maxWidth: vertical ? 360 : undefined,
        minWidth: vertical ? 280 : undefined,
        position: 'relative',
        zIndex: 20,
      }}
    >
      {showTabs && (
        <div
          role="tablist"
          aria-orientation="horizontal"
          style={{
            display: 'flex',
            flexDirection: 'row',
            flexWrap: 'wrap',
            alignContent: 'flex-start',
            flex: 1,
            minWidth: 0,
            minHeight: 0,
            overflow: 'visible',
          }}
        >
          {workspaces.map((workspace) => {
            const active = workspace.id === activeWorkspaceId
            const editing = editingWorkspaceId === workspace.id
            const hasAttention = !active && (attentionWorkspaceIds?.has(workspace.id) ?? false)
            const isWorkspaceDropTarget = Boolean(dragSourcePaneId) && !editing && workspace.id !== activeWorkspaceId
            const summary = workspaceSummaries?.[workspace.id]
            const overlaySummary = !vertical && expandedWorkspaceId === workspace.id && summary && !editing ? summary : null
            const inlineSummary = vertical && summary && !editing ? summary : null
            const dropHandlers = workspaceDropHandlers(workspace.id, isWorkspaceDropTarget)

            return (
              <div
                key={workspace.id}
                {...dropHandlers}
                onMouseEnter={() => {
                  dropHandlers.onMouseEnter()
                  setExpandedWorkspaceId(workspace.id)
                }}
                onMouseLeave={() => {
                  dropHandlers.onMouseLeave()
                  setExpandedWorkspaceId((current) => current === workspace.id ? null : current)
                }}
                onFocusCapture={() => setExpandedWorkspaceId(workspace.id)}
                onBlurCapture={(event) => {
                  const nextTarget = event.relatedTarget
                  if (nextTarget instanceof Node && event.currentTarget.contains(nextTarget)) return
                  setExpandedWorkspaceId((current) => current === workspace.id ? null : current)
                }}
                style={{
                  position: 'relative',
                  display: 'flex',
                  flex: vertical ? '1 1 100%' : '0 1 auto',
                  minWidth: vertical ? '100%' : 200,
                }}
              >
                <div
                  className={hasAttention ? 'panemux-workspace-tab-attention' : undefined}
                  style={{
                    borderRight: !vertical ? '1px solid #333842' : undefined,
                    borderBottom: vertical ? '1px solid #333842' : undefined,
                    borderTop: !vertical ? '1px solid #2a2f39' : undefined,
                    backgroundColor: dragSourcePaneId && hoveredDropWorkspaceId === workspace.id
                      ? 'rgba(86, 156, 214, 0.24)'
                      : active
                        ? '#2f3540'
                        : 'transparent',
                    boxShadow: dragSourcePaneId && hoveredDropWorkspaceId === workspace.id
                      ? 'inset 0 0 0 1px rgba(137, 196, 244, 0.45)'
                      : 'none',
                    display: 'flex',
                    alignItems: 'stretch',
                    minHeight: vertical ? 44 : 48,
                    minWidth: '100%',
                    flex: '1 1 auto',
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
                        height: vertical ? 28 : 34,
                        margin: '10px 8px',
                        minWidth: 0,
                        padding: '0 8px',
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
                        minWidth: 0,
                        padding: '8px 10px',
                        textAlign: 'left',
                      }}
                    >
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 0 }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                          <span style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', fontWeight: 700, fontSize: 13 }}>
                            {workspace.title}
                          </span>
                          {summary && (
                            <span style={{ color: active ? '#c4d0df' : '#8f98a8', fontSize: 10, whiteSpace: 'nowrap' }}>
                              {formatWorkspaceCounts(summary)}
                            </span>
                          )}
                        </div>
                        {summary && (
                          <span
                            style={{
                              color: active ? '#d7dce5' : '#9aa3b2',
                              fontSize: 11,
                              lineHeight: 1.3,
                              whiteSpace: 'nowrap',
                              overflow: 'hidden',
                              textOverflow: 'ellipsis',
                            }}
                          >
                            {summary.panes.map((pane) => pane.title).join(' · ')}
                          </span>
                        )}
                      </div>
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
                        flex: '0 0 32px',
                        fontFamily: TERMINAL_FONT_FAMILY,
                        fontSize: 13,
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
                        flex: '0 0 32px',
                        fontFamily: TERMINAL_FONT_FAMILY,
                        fontSize: 16,
                        padding: 0,
                        textAlign: 'center',
                      }}
                    >
                      ×
                    </InteractiveSurfaceButton>
                  )}
                </div>
                {overlaySummary && (
                  <section
                    aria-label={`${workspace.title} workspace details`}
                    style={{
                      position: 'absolute',
                      ...overlayPositionStyle(tabPosition),
                      width: vertical ? 320 : 360,
                      maxWidth: 'min(360px, calc(100vw - 32px))',
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 6,
                      padding: '8px',
                      border: '1px solid #30353f',
                      borderRadius: 10,
                      background: active ? 'linear-gradient(180deg, #212733 0%, #1a1f28 100%)' : 'linear-gradient(180deg, #181a1f 0%, #15171b 100%)',
                      boxShadow: '0 14px 28px rgba(0, 0, 0, 0.38)',
                      zIndex: 30,
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <span style={workspaceMarkerStyle(active)} />
                      <div style={{ color: active ? '#cfdced' : '#8f98a8', fontFamily: TERMINAL_FONT_FAMILY, fontSize: 10, lineHeight: 1.3 }}>
                        {formatWorkspaceCounts(overlaySummary, true)}
                      </div>
                    </div>
                    <div
                      style={{
                        display: 'grid',
                        gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))',
                        gap: 6,
                        alignItems: 'start',
                      }}
                    >
                      {overlaySummary.panes.map((pane) => (
                        <button
                          key={pane.id}
                          type="button"
                          aria-label={`Open pane ${pane.title} in ${workspace.title}`}
                          draggable
                          onDragStart={(event) => handleSummaryPaneDragStart(event, pane.id)}
                          onDragEnd={() => onEndPaneDragFromSummary?.()}
                          onClick={() => {
                            if (dragSourcePaneId) return
                            onSelectPaneFromSummary?.(workspace.id, pane.id)
                          }}
                          style={{
                            appearance: 'none',
                            border: activePaneId === pane.id ? '1px solid rgba(137, 196, 244, 0.75)' : '1px solid #2d323c',
                            borderRadius: 8,
                            background: activePaneId === pane.id
                              ? 'rgba(86, 156, 214, 0.16)'
                              : pane.attention
                                ? 'rgba(244, 191, 79, 0.08)'
                                : '#1b1e24',
                            color: '#d7dce5',
                            padding: '7px 8px',
                            textAlign: 'left',
                            cursor: onSelectPaneFromSummary ? 'pointer' : 'default',
                            display: 'flex',
                            flexDirection: 'column',
                            gap: 4,
                            fontFamily: TERMINAL_FONT_FAMILY,
                            boxShadow: activePaneId === pane.id ? '0 0 0 1px rgba(137, 196, 244, 0.18)' : 'none',
                            minHeight: 0,
                          }}
                        >
                          <div style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }}>
                            <span style={statusDotStyle(pane.state)} />
                            <span
                              style={{
                                fontSize: 11,
                                fontWeight: 700,
                                minWidth: 0,
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                whiteSpace: 'nowrap',
                                flex: '1 1 auto',
                              }}
                            >
                              {pane.title}
                            </span>
                            {pane.connection && <span style={pillStyle('#2d253f', '#cbb3ff', true)}>{pane.connection}</span>}
                            {pane.attention && <span style={pillStyle('#5a4311', '#f4bf4f', true)}>Input</span>}
                          </div>
                          <div
                            style={{
                              display: 'flex',
                              gap: 6,
                              flexWrap: 'nowrap',
                              color: '#8f98a8',
                              fontSize: 10,
                              minWidth: 0,
                              overflow: 'hidden',
                            }}
                          >
                            {pane.repo && (
                              <span style={{ color: '#9fcbff', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                {pane.repo}
                              </span>
                            )}
                            {pane.branch && (
                              <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{pane.branch}</span>
                            )}
                            {pane.connection && !pane.repo && (
                              <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{pane.type}</span>
                            )}
                            {!pane.repo && <span style={{ whiteSpace: 'nowrap', flexShrink: 0 }}>{pane.state}</span>}
                            {pane.prNumber && <span style={{ whiteSpace: 'nowrap', flexShrink: 0 }}>PR #{pane.prNumber}</span>}
                          </div>
                        </button>
                      ))}
                    </div>
                  </section>
                )}
                {inlineSummary && (
                  <section
                    aria-label={`${workspace.title} workspace details`}
                    style={{
                      flexBasis: 'auto',
                      width: '100%',
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 6,
                      padding: '6px 8px 8px',
                      borderTop: '1px solid #30353f',
                      borderBottom: '1px solid #333842',
                      background: active ? 'linear-gradient(180deg, #212733 0%, #1a1f28 100%)' : 'linear-gradient(180deg, #181a1f 0%, #15171b 100%)',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <span style={workspaceMarkerStyle(active)} />
                      <div style={{ color: active ? '#cfdced' : '#8f98a8', fontFamily: TERMINAL_FONT_FAMILY, fontSize: 10, lineHeight: 1.3 }}>
                        {formatWorkspaceCounts(inlineSummary, true)}
                      </div>
                    </div>
                    <div
                      style={{
                        display: 'grid',
                        gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))',
                        gap: 6,
                        alignItems: 'start',
                      }}
                    >
                      {inlineSummary.panes.map((pane) => (
                        <button
                          key={pane.id}
                          type="button"
                          aria-label={`Open pane ${pane.title} in ${workspace.title}`}
                          draggable
                          onDragStart={(event) => handleSummaryPaneDragStart(event, pane.id)}
                          onDragEnd={() => onEndPaneDragFromSummary?.()}
                          onClick={() => {
                            if (dragSourcePaneId) return
                            onSelectPaneFromSummary?.(workspace.id, pane.id)
                          }}
                          style={{
                            appearance: 'none',
                            border: activePaneId === pane.id ? '1px solid rgba(137, 196, 244, 0.75)' : '1px solid #2d323c',
                            borderRadius: 8,
                            background: activePaneId === pane.id
                              ? 'rgba(86, 156, 214, 0.16)'
                              : pane.attention
                                ? 'rgba(244, 191, 79, 0.08)'
                                : '#1b1e24',
                            color: '#d7dce5',
                            padding: '7px 8px',
                            textAlign: 'left',
                            cursor: onSelectPaneFromSummary ? 'pointer' : 'default',
                            display: 'flex',
                            flexDirection: 'column',
                            gap: 4,
                            fontFamily: TERMINAL_FONT_FAMILY,
                            boxShadow: activePaneId === pane.id ? '0 0 0 1px rgba(137, 196, 244, 0.18)' : 'none',
                            minHeight: 0,
                          }}
                        >
                          <div style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }}>
                            <span style={statusDotStyle(pane.state)} />
                            <span
                              style={{
                                fontSize: 11,
                                fontWeight: 700,
                                minWidth: 0,
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                whiteSpace: 'nowrap',
                                flex: '1 1 auto',
                              }}
                            >
                              {pane.title}
                            </span>
                            {pane.connection && <span style={pillStyle('#2d253f', '#cbb3ff', true)}>{pane.connection}</span>}
                            {pane.attention && <span style={pillStyle('#5a4311', '#f4bf4f', true)}>Input</span>}
                          </div>
                          <div
                            style={{
                              display: 'flex',
                              gap: 6,
                              flexWrap: 'nowrap',
                              color: '#8f98a8',
                              fontSize: 10,
                              minWidth: 0,
                              overflow: 'hidden',
                            }}
                          >
                            {pane.repo && (
                              <span style={{ color: '#9fcbff', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                {pane.repo}
                              </span>
                            )}
                            {pane.branch && (
                              <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{pane.branch}</span>
                            )}
                            {pane.connection && !pane.repo && (
                              <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{pane.type}</span>
                            )}
                            {!pane.repo && <span style={{ whiteSpace: 'nowrap', flexShrink: 0 }}>{pane.state}</span>}
                            {pane.prNumber && <span style={{ whiteSpace: 'nowrap', flexShrink: 0 }}>PR #{pane.prNumber}</span>}
                          </div>
                        </button>
                      ))}
                    </div>
                  </section>
                )}
              </div>
            )
          })}
        </div>
      )}
      {onAdd && (
        <div
          style={{
            borderLeft: !vertical ? '1px solid #333842' : undefined,
            borderTop: vertical ? '1px solid #333842' : undefined,
            backgroundColor: 'transparent',
            display: 'flex',
            alignItems: 'center',
            height: vertical ? 40 : 'auto',
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

function pillStyle(backgroundColor: string, color: string, compact = false): React.CSSProperties {
  return {
    display: 'inline-flex',
    alignItems: 'center',
    padding: compact ? '1px 5px' : '2px 6px',
    borderRadius: 999,
    backgroundColor,
    color,
    fontSize: compact ? 9 : 10,
    fontWeight: 700,
    letterSpacing: '0.02em',
    whiteSpace: 'nowrap',
    flexShrink: 0,
  }
}

function overlayPositionStyle(tabPosition: TabPosition): React.CSSProperties {
  if (tabPosition === 'bottom') {
    return { bottom: 'calc(100% + 8px)', left: 0 }
  }
  if (tabPosition === 'left') {
    return { left: 'calc(100% + 8px)', top: 0 }
  }
  if (tabPosition === 'right') {
    return { right: 'calc(100% + 8px)', top: 0 }
  }
  return { top: 'calc(100% + 8px)', left: 0 }
}

function formatWorkspaceCounts(summary: WorkspaceSummary, includeExited = false): string {
  const parts = [`${summary.paneCount} panes`, `${summary.connectedCount} up`]
  if (summary.disconnectedCount > 0) parts.push(`${summary.disconnectedCount} down`)
  if (includeExited || summary.exitedCount > 0) parts.push(`${summary.exitedCount} exited`)
  if (summary.pendingCount > 0) parts.push(`${summary.pendingCount} pending`)
  return parts.join(' · ')
}

function workspaceMarkerStyle(active: boolean): React.CSSProperties {
  return {
    width: 8,
    height: 8,
    borderRadius: '50%',
    backgroundColor: active ? '#89c4f4' : '#4b5565',
    boxShadow: active ? '0 0 0 1px rgba(137, 196, 244, 0.28)' : 'none',
    flexShrink: 0,
  }
}

function statusDotStyle(state: WorkspacePaneSummary['state']): React.CSSProperties {
  const color = state === 'disconnected'
    ? '#f0c674'
    : state === 'exited'
      ? '#f08b8b'
      : state === 'pending'
        ? '#7aa2f7'
        : '#7bd88f'

  return {
    width: 8,
    height: 8,
    borderRadius: '50%',
    backgroundColor: color,
    boxShadow: `0 0 0 1px ${color}33`,
    flexShrink: 0,
  }
}
