import type { TaskHost, TaskUnreadableStateFile } from '../schemas'
import { hostLabel } from './taskBoard'

/** Where the steps to take when a state file cannot be read are written. */
export const UNREADABLE_DOC_URL =
  'https://github.com/tomo-chan/panemux/blob/main/docs/behavior/tasks.md#unreadable-state-files'

/** The state file fields panemux reads, as the details footer lists them. */
export const READ_STATE_FIELDS = ['sessionId', 'pid', 'cwd', 'status'] as const

export interface UnreadableRow {
  host: string
  file: TaskUnreadableStateFile
}

/** Every host's unreadable state files, in host order. */
export function unreadableRows(hosts: TaskHost[]): UnreadableRow[] {
  return hosts.flatMap((host) => (host.unreadable_state_files ?? []).map((file) => ({ host: host.name, file })))
}

function plural(count: number, word: string): string {
  return `${count} ${word}${count === 1 ? '' : 's'}`
}

export function unreadableTitle(rows: UnreadableRow[]): string {
  const hosts = new Set(rows.map((row) => row.host)).size
  return `Unreadable session state · ${plural(rows.length, 'file')} on ${plural(hosts, 'host')}`
}

export function unreadableReasonLabel(reason: TaskUnreadableStateFile['reason']): string {
  switch (reason) {
    case 'not_json':
      return 'Not valid JSON'
    case 'invalid_pid':
      return 'pid missing or not positive'
    case 'invalid_session_id':
      return 'sessionId missing or not a session ID'
  }
}

/** The process column: the pid in the file name and where it runs. */
export function unreadableProcess(file: TaskUnreadableStateFile): { text: string; note: string } {
  if (file.pid === undefined) return { text: 'unknown', note: 'file name is not <pid>.json' }
  const text = `claude · pid ${file.pid}`
  const location = file.location
  if (location?.kind === 'tmux') return { text, note: `running in tmux ${location.tmux_session ?? ''}`.trimEnd() }
  if (location?.kind === 'outside') {
    return { text, note: location.pane_id ? `running, started from pane ${location.pane_id}` : 'running, not in a pane' }
  }
  return { text, note: 'running' }
}

/** The details as plain text, one tab-separated line per file, for an issue. */
export function unreadableDetailsText(rows: UnreadableRow[]): string {
  const lines = rows.map(({ host, file }) => {
    const reason = unreadableReasonLabel(file.reason) + (file.detail ? `: ${file.detail}` : '')
    const process = unreadableProcess(file)
    return [hostLabel(host), file.file, reason, `${process.text}, ${process.note}`].join('\t')
  })
  return [unreadableTitle(rows), ...lines].join('\n')
}
