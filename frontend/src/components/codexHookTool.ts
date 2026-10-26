import type { HostToolItemProps } from './HostToolItem'
import type { HostMenuTool } from './HostTools'

type CodexHookActions = Pick<HostToolItemProps, 'load' | 'install' | 'setAuto'>

export function codexHookTool(actions: CodexHookActions): HostMenuTool {
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
