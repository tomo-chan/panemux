import React from 'react'

interface EditModeToggleProps {
  editMode: boolean
  onToggle: () => void
}

export const EditModeToggle: React.FC<EditModeToggleProps> = ({ editMode, onToggle }) => {
  return (
    <button
      onClick={onToggle}
      title={editMode ? 'Edit mode ON: changes are saved to config' : 'Edit mode OFF: changes are not saved to config'}
      data-testid="edit-mode-toggle"
      style={{
        position: 'fixed',
        bottom: '8px',
        right: '8px',
        zIndex: 1000,
        background: editMode ? '#2d4a2d' : '#2d2d2d',
        border: `1px solid ${editMode ? '#4a8a4a' : '#555'}`,
        borderRadius: '4px',
        color: editMode ? '#7ec87e' : '#aaa',
        cursor: 'pointer',
        fontSize: '14px',
        padding: '4px 8px',
        fontFamily: 'monospace',
        lineHeight: 1,
        opacity: editMode ? 1 : 0.4,
        transition: 'opacity 0.2s',
      }}
    >
      {editMode ? '🔓' : '🔒'}
    </button>
  )
}
