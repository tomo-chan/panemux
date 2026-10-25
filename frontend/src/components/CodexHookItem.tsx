import React from 'react'
import { HostTools } from './HostTools'
import type { HostToolItemProps } from './HostToolItem'
import type { HostMenuTool } from './HostTools'

export type CodexHookItemProps = Omit<HostToolItemProps, 'tool'>

export function codexHookTool(actions: Pick<CodexHookItemProps, 'load' | 'install' | 'setAuto'>): HostMenuTool {
  return {
    id: 'codex-hook',
    title: 'Codex input notifications',
    absent: label => `No Codex found on ${label}`,
    outdated: "Differs from this panemux's hook",
    installedNotice: "Trust it in codex's /hooks before it runs.",
    autoUpdatedNotice: "Updated automatically. Trust it in codex's /hooks before it runs.",
    ...actions,
  }
}

export const CodexHookItem: React.FC<CodexHookItemProps> = ({ load, install, setAuto, ...props }) => (
  <HostTools {...props} tools={[codexHookTool({ load, install, setAuto })]} />
)
