import React from 'react'

interface IconProps {
  size?: number
  strokeWidth?: number
}

const iconStyle: React.CSSProperties = {
  display: 'block',
  flexShrink: 0,
}

function BaseIcon({
  size = 14,
  strokeWidth = 1.6,
  children,
}: React.PropsWithChildren<IconProps>) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 16 16"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      style={iconStyle}
    >
      {children}
    </svg>
  )
}

export function SettingsIcon() {
  return (
    <BaseIcon>
      <circle cx="8" cy="8" r="2.2" />
      <path d="M8 1.9v1.4" />
      <path d="M8 12.7v1.4" />
      <path d="M14.1 8h-1.4" />
      <path d="M3.3 8H1.9" />
      <path d="M12.3 3.7l-1 1" />
      <path d="M4.7 11.3l-1 1" />
      <path d="M12.3 12.3l-1-1" />
      <path d="M4.7 4.7l-1-1" />
    </BaseIcon>
  )
}

export function CodeIcon() {
  return (
    <BaseIcon>
      <path d="M6 4.2 3 8l3 3.8" />
      <path d="M10 4.2 13 8l-3 3.8" />
    </BaseIcon>
  )
}

export function MaximizeIcon() {
  return (
    <BaseIcon>
      <path d="M6.3 2.5H2.5v3.8" />
      <path d="M9.7 2.5h3.8v3.8" />
      <path d="M6.3 13.5H2.5V9.7" />
      <path d="M9.7 13.5h3.8V9.7" />
    </BaseIcon>
  )
}

export function RestoreIcon() {
  return (
    <BaseIcon>
      <path d="M5 3.2h7.8V11" />
      <path d="M3.2 5H11v7.8H3.2z" />
    </BaseIcon>
  )
}

export function SplitHorizontalIcon() {
  return (
    <BaseIcon>
      <rect x="2.5" y="3" width="11" height="10" rx="1.5" />
      <path d="M8 3v10" />
    </BaseIcon>
  )
}

export function SplitVerticalIcon() {
  return (
    <BaseIcon>
      <rect x="2.5" y="3" width="11" height="10" rx="1.5" />
      <path d="M2.5 8h11" />
    </BaseIcon>
  )
}

export function AddPaneRightIcon() {
  return (
    <BaseIcon>
      <rect x="2.3" y="4.1" width="7.4" height="7.4" rx="1.3" />
      <path d="M12.4 5.1v5.8" />
      <path d="M9.5 8h5.8" />
    </BaseIcon>
  )
}

export function AddPaneBottomIcon() {
  return (
    <BaseIcon>
      <rect x="4.1" y="2.3" width="7.4" height="7.4" rx="1.3" />
      <path d="M5.1 12.4h5.8" />
      <path d="M8 9.5v5.8" />
    </BaseIcon>
  )
}

export function CloseIcon() {
  return (
    <BaseIcon>
      <path d="m4 4 8 8" />
      <path d="m12 4-8 8" />
    </BaseIcon>
  )
}
