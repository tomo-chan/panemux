import { z } from 'zod'

export const DisplayConfigSchema = z.object({
  show_header: z.boolean(),
  show_status_bar: z.boolean(),
})

export type DisplayConfig = z.infer<typeof DisplayConfigSchema>

export const PaneConfigSchema = z.object({
  id: z.string().min(1),
  type: z.enum(['local', 'ssh', 'tmux', 'ssh_tmux']),
  shell: z.string().optional(),
  cwd: z.string().optional(),
  title: z.string().optional(),
  connection: z.string().optional(),
  tmux_session: z.string().optional(),
  show_header: z.boolean().optional(),
  show_status_bar: z.boolean().optional(),
})

export type PaneConfig = z.infer<typeof PaneConfigSchema>

export const CreateSessionRequestSchema = PaneConfigSchema

// LayoutChild is recursive, so we declare the type explicitly first.
export interface LayoutChild {
  size: number
  pane?: PaneConfig
  direction?: 'horizontal' | 'vertical'
  children?: LayoutChild[]
}

export const LayoutChildSchema: z.ZodType<LayoutChild> = z.lazy(() =>
  z.object({
    size: z.number().positive().max(100),
    pane: PaneConfigSchema.optional(),
    direction: z.enum(['horizontal', 'vertical']).optional(),
    children: z.array(LayoutChildSchema).optional(),
  })
)

export const LayoutNodeSchema = z.object({
  direction: z.enum(['horizontal', 'vertical']),
  children: z.array(LayoutChildSchema),
  pane: PaneConfigSchema.optional(),
})

export type LayoutNode = z.infer<typeof LayoutNodeSchema>

export const SessionInfoSchema = z.object({
  id: z.string(),
  type: z.string(),
  title: z.string(),
  state: z.enum(['connecting', 'connected', 'disconnected', 'exited']),
})

export type SessionInfo = z.infer<typeof SessionInfoSchema>

export const WSControlMessageSchema = z.discriminatedUnion('type', [
  z.object({
    type: z.literal('resize'),
    cols: z.number().positive(),
    rows: z.number().positive(),
  }),
  z.object({ type: z.literal('status'), state: z.string() }),
  z.object({ type: z.literal('error'), message: z.string() }),
])

export type WSControlMessage = z.infer<typeof WSControlMessageSchema>

export const GitInfoSchema = z.object({
  is_git: z.boolean(),
  branch: z.string().optional(),
  repo: z.string().optional(),
})

export type GitInfo = z.infer<typeof GitInfoSchema>

export const EditModeResponseSchema = z.object({
  editMode: z.boolean(),
})

export type EditModeResponse = z.infer<typeof EditModeResponseSchema>

export const SSHConnectionsResponseSchema = z.object({
  names: z.array(z.string()),
})

export type SSHConnectionsResponse = z.infer<typeof SSHConnectionsResponseSchema>

export const SSHConfigHostSchema = z.object({
  name: z.string().min(1),
  hostname: z.string().min(1),
  user: z.string().min(1),
  port: z.number().int().min(0).max(65535).optional(),
  identity_file: z.string().optional(),
})

export type SSHConfigHost = z.infer<typeof SSHConfigHostSchema>

export const SSHConfigHostsResponseSchema = z.object({
  hosts: z.array(SSHConfigHostSchema),
})

export type SSHConfigHostsResponse = z.infer<typeof SSHConfigHostsResponseSchema>

export const DetectShellResponseSchema = z.object({
  shell: z.string(),
})

export type DetectShellResponse = z.infer<typeof DetectShellResponseSchema>
