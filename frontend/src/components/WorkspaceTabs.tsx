import React from 'react'
import type { TabPosition, Workspace } from '../types'
import { TERMINAL_FONT_FAMILY } from '../utils/fonts'

interface WorkspaceTabsProps {
  workspaces: Workspace[]
  activeWorkspaceId: string
  tabPosition: TabPosition
  onSelect: (workspaceId: string) => void
  onAdd?: () => void
  onDelete?: (workspaceId: string) => void
}

export const WorkspaceTabs: React.FC<WorkspaceTabsProps> = ({
  workspaces,
  activeWorkspaceId,
  tabPosition,
  onSelect,
  onAdd,
  onDelete,
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
              <div
                key={workspace.id}
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
                <button
                  role="tab"
                  aria-selected={active}
                  onClick={() => onSelect(workspace.id)}
                  title={workspace.title}
                  style={{
                    appearance: 'none',
                    border: 'none',
                    backgroundColor: 'transparent',
                    color: active ? '#ffffff' : '#b8beca',
                    cursor: active ? 'default' : 'pointer',
                    flex: 1,
                    fontFamily: TERMINAL_FONT_FAMILY,
                    fontSize: 13,
                    height: '100%',
                    minWidth: 0,
                    padding: onDelete ? '0 6px 0 12px' : '0 12px',
                    textAlign: 'left',
                    whiteSpace: 'nowrap',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                  }}
                >
                  {workspace.title}
                </button>
                {onDelete && (
                  <button
                    type="button"
                    aria-label={`Delete ${workspace.title} workspace`}
                    title="Delete workspace"
                    onClick={() => onDelete(workspace.id)}
                    style={{
                      appearance: 'none',
                      border: 'none',
                      backgroundColor: 'transparent',
                      color: active ? '#d7dce5' : '#8f96a3',
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
                  </button>
                )}
              </div>
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
