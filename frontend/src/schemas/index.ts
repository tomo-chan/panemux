import { z } from 'zod'

export const DisplayConfigSchema = z.object({
  show_header: z.boolean(),
  show_status_bar: z.boolean(),
})

export type DisplayConfig = z.infer<typeof DisplayConfigSchema>

export const PaneConfigSchema = z.object({
  id: z.string().min(1),
  type: z.enum(['local', 'ssh', 'tmux', 'ssh_tmux']),
  shell: z.string().max(512).optional(),
  cwd: z.string().max(4096).optional(),
  title: z.string().max(256).optional(),
  connection: z.string().max(256).optional(),
  tmux_session: z.string().max(256).optional(),
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
    children: z.array(LayoutChildSchema).max(50).optional(),
  })
)

export const LayoutNodeSchema = z.object({
  direction: z.enum(['horizontal', 'vertical']),
  children: z.array(LayoutChildSchema),
  pane: PaneConfigSchema.optional(),
})

export type LayoutNode = z.infer<typeof LayoutNodeSchema>

export const TabPositionSchema = z.enum(['top', 'bottom', 'left', 'right'])

export type TabPosition = z.infer<typeof TabPositionSchema>

export const WorkspaceTabPositionRequestSchema = z.object({
  tab_position: TabPositionSchema,
})

export type WorkspaceTabPositionRequest = z.infer<typeof WorkspaceTabPositionRequestSchema>

export const WorkspaceVerticalBarWidthSchema = z.number().int().min(180).max(520)

export const WorkspaceVerticalBarWidthRequestSchema = z.object({
  vertical_bar_width: WorkspaceVerticalBarWidthSchema,
})

export type WorkspaceVerticalBarWidthRequest = z.infer<typeof WorkspaceVerticalBarWidthRequestSchema>

export const WorkspaceSchema = z.object({
  id: z.string().min(1),
  title: z.string().min(1),
  layout: LayoutNodeSchema,
})

export type Workspace = z.infer<typeof WorkspaceSchema>

export const WorkspacesResponseSchema = z.object({
  active: z.string().min(1),
  tab_position: TabPositionSchema,
  vertical_bar_width: WorkspaceVerticalBarWidthSchema,
  items: z.array(WorkspaceSchema).min(1),
})

export type WorkspacesResponse = z.infer<typeof WorkspacesResponseSchema>

export const SessionInfoSchema = z.object({
  id: z.string(),
  type: z.string(),
  title: z.string(),
  state: z.enum(['connecting', 'connected', 'disconnected', 'exited']),
})

export type SessionInfo = z.infer<typeof SessionInfoSchema>

export const SessionInfoListSchema = z.array(SessionInfoSchema)

export type SessionInfoList = z.infer<typeof SessionInfoListSchema>

export const SessionStateSchema = z.enum(['connected', 'disconnected', 'exited'])

export type SessionState = z.infer<typeof SessionStateSchema>

export const WSControlMessageSchema = z.discriminatedUnion('type', [
  z.object({
    type: z.literal('resize'),
    cols: z.number().positive(),
    rows: z.number().positive(),
  }),
  z.object({ type: z.literal('status'), state: SessionStateSchema }),
  z.object({ type: z.literal('replay'), state: z.enum(['start', 'end']) }),
  z.object({ type: z.literal('error'), message: z.string().max(2000) }),
])

export type WSControlMessage = z.infer<typeof WSControlMessageSchema>

export const WorktreeInfoSchema = z.object({
  branch: z.string().optional(),
  repo: z.string().optional(),
  repo_url: z.string().url().optional(),
  pr_number: z.number().int().positive().optional(),
  pr_url: z.string().url().optional(),
})

export type WorktreeInfo = z.infer<typeof WorktreeInfoSchema>

export const GitInfoSchema = z.object({
  is_git: z.boolean(),
  branch: z.string().optional(),
  repo: z.string().optional(),
  repo_url: z.string().url().optional(),
  pr_number: z.number().int().positive().optional(),
  pr_url: z.string().url().optional(),
  worktrees: z.array(WorktreeInfoSchema).optional(),
})

export type GitInfo = z.infer<typeof GitInfoSchema>

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

export const DirectoryEntrySchema = z.object({
  name: z.string().min(1),
  path: z.string().min(1),
  has_children: z.boolean(),
})

export type DirectoryEntry = z.infer<typeof DirectoryEntrySchema>

export const DirectoryBrowserResponseSchema = z.object({
  path: z.string().min(1),
  entries: z.array(DirectoryEntrySchema),
})

export type DirectoryBrowserResponse = z.infer<typeof DirectoryBrowserResponseSchema>
