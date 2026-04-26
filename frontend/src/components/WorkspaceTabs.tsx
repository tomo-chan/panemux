import React from 'react'
import type { TabPosition, Workspace } from '../types'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface WorkspaceTabsProps {
  workspaces: Workspace[]
  activeWorkspaceId: string
  tabPosition: TabPosition
  onSelect: (workspaceId: string) => void
  onAdd?: () => void
}

export const WorkspaceTabs: React.FC<WorkspaceTabsProps> = ({
  workspaces,
  activeWorkspaceId,
  tabPosition,
  onSelect,
  onAdd,
}) => {
  const vertical = tabPosition === 'left' || tabPosition === 'right'
  const showTabs = workspaces.length > 1
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
            return (
              <button
                key={workspace.id}
                role="tab"
                aria-selected={active}
                onClick={() => onSelect(workspace.id)}
                title={workspace.title}
                style={{
                  appearance: 'none',
                  border: 'none',
                  borderRight: !vertical ? '1px solid #333842' : undefined,
                  borderBottom: vertical ? '1px solid #333842' : undefined,
                  backgroundColor: active ? '#2f3540' : 'transparent',
                  color: active ? '#ffffff' : '#b8beca',
                  cursor: active ? 'default' : 'pointer',
                  fontFamily: TERMINAL_FONT_FAMILY,
                  fontSize: 13,
                  height: vertical ? 38 : 34,
                  minWidth: vertical ? '100%' : 96,
                  maxWidth: vertical ? '100%' : 180,
                  padding: '0 12px',
                  textAlign: 'left',
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}
              >
                {workspace.title}
              </button>
            )
          })}
        </div>
      )}
      {onAdd && (
        <button
          type="button"
          aria-label="Add workspace"
          title="Add workspace"
          onClick={onAdd}
          style={{
            appearance: 'none',
            border: 'none',
            borderRight: !vertical ? '1px solid #333842' : undefined,
            borderBottom: vertical ? '1px solid #333842' : undefined,
            backgroundColor: 'transparent',
            color: '#b8beca',
            cursor: 'pointer',
            fontFamily: TERMINAL_FONT_FAMILY,
            fontSize: 18,
            height: vertical ? 38 : 34,
            minWidth: vertical ? '100%' : 40,
            padding: 0,
            textAlign: 'center',
            lineHeight: vertical ? '38px' : '34px',
          }}
        >
          +
        </button>
      )}
    </div>
  )
}
