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
        bottom: '12px',
        right: '12px',
        zIndex: 1000,
        background: editMode ? '#1a3040' : '#3f3f46',
        border: `1px solid ${editMode ? '#569cd6' : '#52525b'}`,
        borderRadius: '4px',
        color: editMode ? '#569cd6' : '#888',
        cursor: 'pointer',
        fontSize: '12px',
        padding: '5px 10px',
        fontFamily: 'monospace',
        lineHeight: 1,
        display: 'flex',
        alignItems: 'center',
        gap: '5px',
        opacity: editMode ? 1 : 0.65,
        boxShadow: editMode
          ? '0 0 0 1px #569cd6, 0 2px 8px rgba(0,0,0,0.6)'
          : '0 2px 8px rgba(0,0,0,0.5)',
        transition: 'opacity 0.2s, box-shadow 0.2s, border-color 0.2s, color 0.2s',
      }}
    >
      <span>{editMode ? '🔓' : '🔒'}</span>
      <span>{editMode ? 'EDIT' : 'EDIT'}</span>
    </button>
  )
}
