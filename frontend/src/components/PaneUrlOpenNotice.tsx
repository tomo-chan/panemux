import React from 'react'

interface PaneUrlOpenNoticeProps {
  /** URL a program inside the pane asked panemux to open. */
  pendingUrl: string | null
  /** Last port-forwarding failure for this pane. */
  error: string | null
  onConfirm: () => void
  onDismiss: () => void
  onDismissError: () => void
}

const barStyle: React.CSSProperties = {
  position: 'absolute',
  top: 0,
  left: 0,
  right: 0,
  zIndex: 12,
  display: 'flex',
  alignItems: 'center',
  gap: '8px',
  padding: '6px 10px',
  fontSize: '12px',
  color: '#d4d4d4',
  backgroundColor: 'rgba(38, 39, 43, 0.96)',
  borderBottom: '1px solid #3f3f46',
}

const urlStyle: React.CSSProperties = {
  flex: 1,
  minWidth: 0,
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
  color: '#89c4f4',
}

const buttonStyle: React.CSSProperties = {
  padding: '2px 10px',
  backgroundColor: '#3f3f46',
  color: '#d4d4d4',
  border: '1px solid #52525b',
  borderRadius: '4px',
  fontSize: '12px',
  cursor: 'pointer',
  flexShrink: 0,
}

/**
 * Pane-local strip for URL opening. A URL a pane asked to open always needs
 * the operator's approval first: terminal output is untrusted, so anything
 * able to write to the pane's terminal could otherwise drive the browser.
 */
export const PaneUrlOpenNotice: React.FC<PaneUrlOpenNoticeProps> = ({
  pendingUrl,
  error,
  onConfirm,
  onDismiss,
  onDismissError,
}) => {
  if (pendingUrl) {
    return (
      <div style={barStyle} data-pane-url-request="true">
        <span style={{ flexShrink: 0 }}>This pane wants to open</span>
        <span style={urlStyle} title={pendingUrl}>{pendingUrl}</span>
        <button style={buttonStyle} onClick={onConfirm}>Open</button>
        <button style={buttonStyle} onClick={onDismiss}>Ignore</button>
      </div>
    )
  }

  if (error) {
    return (
      <div style={{ ...barStyle, borderBottomColor: '#7f1d1d' }} data-pane-url-error="true">
        <span style={{ ...urlStyle, color: '#f4a9a9' }} title={error}>
          Port forwarding failed: {error}
        </span>
        <button style={buttonStyle} onClick={onDismissError}>Dismiss</button>
      </div>
    )
  }

  return null
}
