import { z } from 'zod'

export const DisplayConfigSchema = z.object({
  show_header: z.boolean(),
  show_status_bar: z.boolean(),
  // The effective Cmd/Ctrl+Shift letter that switches between the task
  // dashboard and the workspaces. GET /api/display always sends it, already
  // upper-cased and defaulted; it is optional only for the display defaults
  // the frontend builds itself before that response arrives.
  task_dashboard_shortcut: z.string().regex(/^[A-Z]$/).optional(),
})

export type DisplayConfig = z.infer<typeof DisplayConfigSchema>

export const BoardModeSchema = z.enum(['monitor', 'turn', 'both', 'off'])

export type BoardMode = z.infer<typeof BoardModeSchema>

export const PaneAgentBoardConfigSchema = z.object({
  enabled: z.boolean().optional(),
  mode: BoardModeSchema.optional(),
})

export type PaneAgentBoardConfig = z.infer<typeof PaneAgentBoardConfigSchema>

// Every field the backend's config.PaneConfig carries must appear here.
// Zod strips unknown keys, and the layout tree is read, parsed, and PUT back
// wholesale on any edit — a split, a move, even a debounced resize — so a
// field missing from this schema is silently deleted from the user's
// config.yaml by an unrelated action. agent_board was lost exactly that way.
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
  agent_board: PaneAgentBoardConfigSchema.optional(),
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

// `pane` is declared even though nothing here renders it — SplitContainer,
// App.tsx and taskBoard.ts all read child.pane off a
// LayoutChild, never the root's own. It is declared because the server can
// still emit it: normalizeLayoutNode relocates a root pane only when the node
// has no children, leaving the `{pane, children}` shape as the operator wrote
// it. An undeclared key is not merely unread — parse() strips it, useLayout
// stores the stripped tree, and the next split PUTs it back, deleting the pane
// from config.yaml. See PaneConfigSchema's comment above: agent_board was lost
// exactly that way.
//
// direction and children, by contrast, are required: normalizeLayoutNode
// guarantees both on every response (issue #198).
export const LayoutNodeSchema = z.object({
  pane: PaneConfigSchema.optional(),
  direction: z.enum(['horizontal', 'vertical']),
  children: z.array(LayoutChildSchema),
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

// An ssh_connections entry in config.yaml: a host the task dashboard
// collects from, which panes can also use (issue #272). The server never
// sends the password, only whether one is saved. Fields left out are taken
// from the ~/.ssh/config Host block of the same name, when there is one.
export const SSHConnectionEntrySchema = z.object({
  name: z.string().min(1),
  host: z.string().optional(),
  user: z.string().optional(),
  // Not range-checked: config.yaml is not checked on load, and a hand-written
  // port out of range must still list so it can be corrected here.
  port: z.number().int().optional(),
  key_file: z.string().optional(),
  known_hosts_file: z.string().optional(),
  has_password: z.boolean(),
  in_ssh_config: z.boolean(),
  panes: z.array(z.string()),
})

export type SSHConnectionEntry = z.infer<typeof SSHConnectionEntrySchema>

export const SSHConnectionEntriesResponseSchema = z.object({
  connections: z.array(SSHConnectionEntrySchema),
})

export type SSHConnectionEntriesResponse = z.infer<typeof SSHConnectionEntriesResponseSchema>

// The body of POST /api/config/ssh-connections and of
// PUT /api/config/ssh-connections/{name}. On an update an empty password
// keeps the saved one, and clear_password removes it; name, when sent,
// must be the entry's own.
export const SSHConnectionRequestSchema = z.object({
  name: z.string().optional(),
  host: z.string().optional(),
  user: z.string().optional(),
  port: z.number().int().min(0).max(65535).optional(),
  key_file: z.string().optional(),
  known_hosts_file: z.string().optional(),
  password: z.string().optional(),
  clear_password: z.boolean().optional(),
})

export type SSHConnectionRequest = z.infer<typeof SSHConnectionRequestSchema>

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

export const BoardSessionTokenResponseSchema = z.object({
  token: z.string(),
  command_center_enabled: z.boolean(),
  agent_board_enabled: z.boolean(),
})

export type BoardSessionTokenResponse = z.infer<typeof BoardSessionTokenResponseSchema>

// `warnings` carries what went wrong *around* a query rather than to it —
// today only a failed history write. It is optional because the server omits
// the key entirely when there is none, and it rides whichever terminal frame
// ends the query rather than getting a frame of its own, so that exactly one
// frame still ends it: see docs/behavior.md's command center WS protocol, and
// #214. It is on `error` as well as `done` because a turn that failed for its
// own reasons can also have lost its history record, and reporting it only on
// the successful path hides it from precisely the operator who needs it.
export const BoardCommandFrameSchema = z.discriminatedUnion('type', [
  z.object({ type: z.literal('line'), raw: z.unknown() }),
  z.object({ type: z.literal('error'), message: z.string(), warnings: z.array(z.string()).optional() }),
  z.object({ type: z.literal('done'), warnings: z.array(z.string()).optional() }),
  z.object({ type: z.literal('busy') }),
])

export type BoardCommandFrame = z.infer<typeof BoardCommandFrameSchema>

export const BoardCommandHistoryEntrySchema = z.object({
  at: z.string(),
  raw: z.unknown(),
})

export type BoardCommandHistoryEntry = z.infer<typeof BoardCommandHistoryEntrySchema>

export const BoardCommandHistoryResponseSchema = z.object({
  entries: z.array(BoardCommandHistoryEntrySchema),
})

export type BoardCommandHistoryResponse = z.infer<typeof BoardCommandHistoryResponseSchema>

// Response from POST /api/sessions/{id}/open-url: whether panemux published
// the URL's loopback callback port on this host, and why not when it did not.
export const OpenUrlResponseSchema = z.object({
  url: z.string(),
  forwarded: z.boolean(),
  port: z.number().int().min(1).max(65535).optional(),
  reason: z.string().max(512).optional(),
})

export type OpenUrlResponse = z.infer<typeof OpenUrlResponseSchema>
// Deliberately no .max() on any field, unlike most schemas in this file.
// Every value here is free text an agent wrote about itself, and the Go side
// (internal/board's ParseStatus) imposes no length limit of its own, so a
// cap here could only ever reject a payload the server considers valid. Zod
// rejects rather than truncates, and because these entries live inside a
// z.record, one over-long summary would fail the whole response — blanking
// every other pane's status too, on every poll, until that one pane happened
// to report something shorter. Same reasoning as BoardMessageSchema.body.
export const BoardStatusEntrySchema = z.object({
  updated_at: z.string(),
  state: z.string().optional(),
  cwd: z.string().optional(),
  branch: z.string().optional(),
  repo: z.string().optional(),
  pr_url: z.string().optional(),
  last_tool: z.string().optional(),
  summary: z.string().optional(),
})

export type BoardStatusEntry = z.infer<typeof BoardStatusEntrySchema>

export const BoardStatusResponseSchema = z.object({
  statuses: z.record(z.string(), BoardStatusEntrySchema),
})

export type BoardStatusResponse = z.infer<typeof BoardStatusResponseSchema>

export const BoardMessageSchema = z.object({
  at: z.string(),
  host: z.string(),
  team: z.string(),
  from: z.string(),
  to: z.string(),
  // body deliberately has no .max(): Zod's .max() rejects rather than
  // truncates, so capping it would let a single oversized message fail
  // parsing for the entire feed response. See useBoardStatus for how a
  // single malformed row is tolerated instead of failing the whole batch.
  body: z.string(),
  seq: z.number().int(),
  // Computed server-side by internal/board's IsStatusRow. Re-deriving it
  // here by parsing body in JavaScript would be a second implementation of a
  // rule Go already owns, and the two diverge on real inputs: Go's
  // json.Unmarshal matches field names case-insensitively and errors on a
  // type mismatch, JSON.parse does neither.
  is_status: z.boolean(),
})

export type BoardMessage = z.infer<typeof BoardMessageSchema>

export const BoardMessagesResponseSchema = z.object({
  messages: z.array(BoardMessageSchema),
  // Identifies the server-side cache these seq values were assigned by. The
  // cache is in-memory only, so a panemux restart renumbers from 1 and a
  // cursor held across it would never match anything again. See
  // useBoardStatus for the reset this drives.
  epoch: z.string(),
})

export type BoardMessagesResponse = z.infer<typeof BoardMessagesResponseSchema>

// ── Task dashboard: GET /api/tasks ─────────────────────────────────────────
//
// Free text that a remote host reports about its own processes (cwd,
// waiting_for, tmux_session, a host's error) carries no .max(): Zod rejects
// rather than truncates, so one long value from one host would fail the whole
// response and blank every other host's tasks. Same reasoning as
// BoardStatusEntrySchema.

// A link the dashboard opens in a new tab. z.string().url() alone accepts
// any scheme new URL() parses, javascript: included, so the scheme is pinned.
const HttpUrlSchema = z
  .string()
  .url()
  .refine((value) => /^https?:\/\//i.test(value), { message: 'must be an http(s) URL' })

export const TaskStateSchema = z.enum(['busy', 'wait', 'idle', 'run', 'unknown', 'stop'])

export type TaskState = z.infer<typeof TaskStateSchema>

export const TaskLocationSchema = z.object({
  // daemon: a codex session run by codex's shared app-server daemon, which
  // no pane can be told to show (issue #264).
  kind: z.enum(['tmux', 'outside', 'daemon', 'none']),
  tmux_session: z.string().optional(),
  // The pane an agent outside tmux was started from, as its PANEMUX_PANE_ID
  // names it. Only a claim: findTaskPane matches it against the workspaces.
  pane_id: z.string().optional(),
  // Whether a tmux / ssh_tmux pane can attach to tmux_session: pane configs
  // accept only a restricted set of session-name characters.
  attachable: z.boolean(),
})

export type TaskLocation = z.infer<typeof TaskLocationSchema>

// An issue the task's pull request closes. repo is the issue's owner/name,
// which can differ from the pull request's repository.
export const TaskIssueLinkSchema = z.object({
  number: z.number().int().positive(),
  url: HttpUrlSchema,
  repo: z.string().optional(),
})

export type TaskIssueLink = z.infer<typeof TaskIssueLinkSchema>

// A reference in the task's branch name or pull request title (JIRA-123) that
// a task_dashboard.autolinks entry turned into a link.
export const TaskAutolinkSchema = z.object({
  text: z.string().min(1),
  url: HttpUrlSchema,
})

export type TaskAutolink = z.infer<typeof TaskAutolinkSchema>

export const TaskGitSchema = z.object({
  repo: z.string().optional(),
  repo_url: HttpUrlSchema.optional(),
  branch: z.string().optional(),
  pr_url: HttpUrlSchema.optional(),
  pr_number: z.number().int().positive().optional(),
  issues: z.array(TaskIssueLinkSchema).optional(),
  autolinks: z.array(TaskAutolinkSchema).optional(),
})

export type TaskGit = z.infer<typeof TaskGitSchema>

// What `claude -p` on the panemux host made of a task's conversation log
// (issue #258). text, remaining and summarized_at are the last answer, when
// there is one, whatever state says; outdated means the log changed since.
// done_candidate is a ready, current answer with nothing remaining — a person
// still decides whether the task is done. Like the rest of the task, the
// text carries no .max(): the server bounds it.
export const TaskSummarySchema = z.object({
  state: z.enum(['pending', 'ready', 'error', 'unreadable']),
  text: z.string().optional(),
  remaining: z.array(z.string()).optional(),
  summarized_at: z.string().optional(),
  outdated: z.boolean().optional(),
  done_candidate: z.boolean().optional(),
  error: z.string().optional(),
})

export type TaskSummary = z.infer<typeof TaskSummarySchema>

export const TaskSchema = z.object({
  id: z.string().min(1),
  // The ssh_connections key, or '' for the panemux host itself.
  host: z.string(),
  agent: z.string(),
  session_id: z.string().optional(),
  cwd: z.string().optional(),
  state: TaskStateSchema,
  waiting_for: z.string().optional(),
  // Identifies the wait a 'wait' task is in (issue #278): the same wait keeps
  // it across collections, a new wait gets another. Opaque — compare it, do
  // not parse it. Absent when the agent did not record when the wait began.
  wait_signature: z.string().min(1).optional(),
  status_since: z.string().optional(),
  started_at: z.string().optional(),
  pid: z.number().int().positive().optional(),
  location: TaskLocationSchema,
  git: TaskGitSchema.optional(),
  // What a person recorded on the dashboard (issue #256). Only a task with a
  // session_id can carry them; done does not change state.
  done: z.boolean().optional(),
  labels: z.array(z.string()).optional(),
  // Present only while summaries are enabled and once the task has one.
  summary: TaskSummarySchema.optional(),
})

export type Task = z.infer<typeof TaskSchema>

// A Claude Code state file the host could not read (issue #313). It is not a
// task; the dashboard shows it as a diagnostic. pid and location are the live
// claude process the file name (<pid>.json) names, absent when it names none.
export const TaskUnreadableStateFileSchema = z.object({
  file: z.string(),
  reason: z.enum(['not_json', 'invalid_pid', 'invalid_session_id']),
  detail: z.string().optional(),
  pid: z.number().int().positive().optional(),
  location: TaskLocationSchema.optional(),
})

export type TaskUnreadableStateFile = z.infer<typeof TaskUnreadableStateFileSchema>

export const TaskHostSchema = z.object({
  name: z.string(),
  status: z.enum(['ok', 'error', 'connecting']),
  error: z.string().optional(),
  collected_at: z.string().optional(),
  unreadable_state_files: z.array(TaskUnreadableStateFileSchema).optional(),
})

export type TaskHost = z.infer<typeof TaskHostSchema>

export const TasksResponseSchema = z.object({
  hosts: z.array(TaskHostSchema),
  tasks: z.array(TaskSchema),
  // Why the task record file could not be read; the tasks come without records.
  records_error: z.string().optional(),
  // Every label the task record file holds, once each, in case-insensitive
  // alphabetical order: the label suggestions (issue #310). Absent when there
  // are none or the file could not be read.
  known_labels: z.array(z.string()).optional(),
  // task_dashboard.summary.enabled. The server always sends it; absent is off.
  summaries_enabled: z.boolean().optional(),
  // Why summaries are not being saved across restarts (issue #352); they are
  // still made and shown. Present only while summaries are enabled.
  summaries_error: z.string().optional(),
})

export type TasksResponse = z.infer<typeof TasksResponseSchema>

// ── Task events: GET /ws/tasks/events ──────────────────────────────────────
//
// The task event stream (docs/behavior/task-events.md): a snapshot, then one
// frame per change. A frame that fails these schemas is treated like a gap
// in seq: the receiver discards what it holds and reconnects.

// A running task's state: `stop` is never published.
export const TaskEventStateSchema = z.enum(['busy', 'wait', 'idle', 'run', 'unknown'])

export type TaskEventState = z.infer<typeof TaskEventStateSchema>

export const TaskEventTaskSchema = z.object({
  id: z.string(),
  host: z.string(),
  agent: z.string(),
  session_id: z.string().optional(),
  cwd: z.string().optional(),
  state: TaskEventStateSchema,
  // Only on `wait`.
  waiting_for: z.string().optional(),
  // Only on `wait`: the wait signature (`w1-…`), or `e1-<epoch>-<seq>` for a
  // wait the agent did not record the start of.
  wait_id: z.string().min(1).optional(),
  // As of this frame; never compared.
  status_since: z.string().optional(),
  location: TaskLocationSchema,
})

export type TaskEventTask = z.infer<typeof TaskEventTaskSchema>

export const TaskEventHostSchema = z.object({
  name: z.string(),
  status: z.enum(['pending', 'ok', 'connecting', 'error']),
  error: z.string().optional(),
})

export type TaskEventHost = z.infer<typeof TaskEventHostSchema>

const TaskEventOpSchema = z.enum(['added', 'changed', 'removed'])

const taskEventPosition = {
  epoch: z.string(),
  seq: z.number().int().nonnegative(),
}

export const TaskEventFrameSchema = z.discriminatedUnion('type', [
  z.object({
    type: z.literal('snapshot'),
    ...taskEventPosition,
    hosts: z.array(TaskEventHostSchema),
    tasks: z.array(TaskEventTaskSchema),
  }),
  z.object({
    type: z.literal('task'),
    ...taskEventPosition,
    op: TaskEventOpSchema,
    // The state before a `changed` frame, or the last observed one of a
    // `removed` task.
    prev_state: TaskEventStateSchema.optional(),
    task: TaskEventTaskSchema,
  }),
  z.object({
    type: z.literal('host'),
    ...taskEventPosition,
    op: TaskEventOpSchema,
    host: TaskEventHostSchema,
  }),
])

export type TaskEventFrame = z.infer<typeof TaskEventFrameSchema>

// ── Task dashboard: PUT /api/tasks/records ─────────────────────────────────
//
// The request body and the response have the same shape. The response always
// carries done and labels, so the dashboard applies it to the task as it is.

export const TaskRecordSchema = z.object({
  host: z.string(),
  agent: z.string(),
  session_id: z.string().min(1),
  done: z.boolean(),
  labels: z.array(z.string()),
})

export type TaskRecord = z.infer<typeof TaskRecordSchema>

// ── Task dashboard: POST /api/tasks, POST /api/tasks/resume ────────────────
//
// A task started or resumed (issue #257). The id is the one GET /api/tasks
// will list the task under once the agent has written its state, which is how
// the dashboard selects it. A new codex task has neither id nor session_id:
// codex picks its session ID once it has its first instruction, so the task
// is found by its tmux session instead (issue #264).

export const TaskLaunchedSchema = z.object({
  id: z.string().min(1).optional(),
  session_id: z.string().min(1).optional(),
  tmux_session: z.string().min(1),
})

export type TaskLaunched = z.infer<typeof TaskLaunchedSchema>

// POST /api/tasks/attach (issue #283): the board's temporary tmux client on a
// task's running tmux session. `session_id` is served by `/ws/{session_id}`.
export const TaskAttachSchema = z.object({
  session_id: z.string().startsWith('board-'),
  tmux_session: z.string().min(1),
})

export type TaskAttach = z.infer<typeof TaskAttachSchema>

// A tmux session name: the server's guard (`validTmuxSessionName`) accepts no
// other character.
const TmuxSessionNameSchema = z.string().regex(/^[a-zA-Z0-9_.-]+$/)

// POST /api/hosts/session-name (issue #314): a new tmux session name for a
// host, generated by the server for both Open and Type in pane.
export const HostSessionNameSchema = z.object({
  tmux_session: TmuxSessionNameSchema,
})

export type HostSessionName = z.infer<typeof HostSessionNameSchema>

// POST /api/hosts/terminal (issue #314): a host terminal the task dashboard
// opens, served by `/ws/{session_id}` like a board attach. `tmux_session` is
// empty for an ssh terminal.
export const HostTerminalSchema = z.object({
  session_id: z.string().startsWith('board-'),
  tmux_session: z.union([z.literal(''), TmuxSessionNameSchema]),
})

export type HostTerminal = z.infer<typeof HostTerminalSchema>

// A new task's response adds the labels recorded for it, or why they could
// not be — the task was started either way. A codex task's labels are held
// until a collection finds its session, and come back as pending_labels.
export const TaskLaunchResponseSchema = TaskLaunchedSchema.extend({
  labels: z.array(z.string()).optional(),
  pending_labels: z.array(z.string()).optional(),
  records_error: z.string().optional(),
})

export type TaskLaunchResponse = z.infer<typeof TaskLaunchResponseSchema>
