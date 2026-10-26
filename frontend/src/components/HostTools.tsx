import React from 'react'
import { HostToolItem } from './HostToolItem'
import type { HostToolItemProps, ToolDefinition } from './HostToolItem'

export type HostMenuTool = ToolDefinition & Pick<HostToolItemProps, 'load' | 'install' | 'setAuto'>
export interface HostToolsProps {
  host: string
  label?: string
  alone?: boolean
  tools: HostMenuTool[]
}

export const HostTools: React.FC<HostToolsProps> = ({ tools, ...props }) => (
  <>{tools.map(tool => <HostToolItem key={tool.id} {...props} tool={tool} load={tool.load} install={tool.install} setAuto={tool.setAuto} />)}</>
)
