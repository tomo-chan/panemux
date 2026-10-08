import { describe, expect, it } from 'vitest'
import type { TaskHost } from '../schemas'
import {
  unreadableDetailsText,
  unreadableProcess,
  unreadableReasonLabel,
  unreadableRows,
  unreadableTitle,
} from './unreadableState'

const hosts: TaskHost[] = [
  {
    name: '',
    status: 'ok',
    unreadable_state_files: [
      {
        file: '48213.json',
        reason: 'not_json',
        detail: 'unexpected end of JSON input',
        pid: 48213,
        location: { kind: 'outside', attachable: false },
      },
      {
        file: '51007.json',
        reason: 'invalid_session_id',
        detail: 'sessionId: "../x"',
        pid: 51007,
        location: { kind: 'tmux', tmux_session: 'task-1c7b8d2f', attachable: true },
      },
    ],
  },
  { name: 'build-box', status: 'ok' },
  { name: 'gpu-1', status: 'ok', unreadable_state_files: [{ file: 'legacy.json', reason: 'invalid_pid' }] },
]

describe('unreadableRows', () => {
  it('lists every host’s unreadable files in host order', () => {
    expect(unreadableRows(hosts).map((row) => `${row.host}/${row.file.file}`)).toEqual([
      '/48213.json',
      '/51007.json',
      'gpu-1/legacy.json',
    ])
  })

  it('is empty when no host reports one', () => {
    expect(unreadableRows([{ name: '', status: 'ok' }])).toEqual([])
  })
})

describe('unreadableTitle', () => {
  it('counts the files and the hosts they are on', () => {
    expect(unreadableTitle(unreadableRows(hosts))).toBe('Unreadable session state · 3 files on 2 hosts')
    expect(unreadableTitle(unreadableRows(hosts).slice(0, 1))).toBe('Unreadable session state · 1 file on 1 host')
  })
})

describe('unreadableReasonLabel', () => {
  it('names each reason the server reports', () => {
    expect(unreadableReasonLabel('not_json')).toBe('Not valid JSON')
    expect(unreadableReasonLabel('invalid_pid')).toBe('pid missing or not positive')
    expect(unreadableReasonLabel('invalid_session_id')).toBe('sessionId missing or not a session ID')
  })
})

describe('unreadableProcess', () => {
  it('says where the pid in the file name runs', () => {
    const [outside, tmux] = hosts[0].unreadable_state_files ?? []
    expect(unreadableProcess(outside)).toEqual({ text: 'claude · pid 48213', note: 'running, not in a pane' })
    expect(unreadableProcess(tmux)).toEqual({ text: 'claude · pid 51007', note: 'running in tmux task-1c7b8d2f' })
    expect(
      unreadableProcess({
        file: '7.json',
        reason: 'not_json',
        pid: 7,
        location: { kind: 'outside', pane_id: 'p-1', attachable: false },
      }),
    ).toEqual({ text: 'claude · pid 7', note: 'running, started from pane p-1' })
  })

  it('says the process is unknown when the file name carries no pid', () => {
    expect(unreadableProcess({ file: 'legacy.json', reason: 'invalid_pid' })).toEqual({
      text: 'unknown',
      note: 'file name is not <pid>.json',
    })
  })
})

describe('unreadableDetailsText', () => {
  it('is one line per file, ready to paste into an issue', () => {
    expect(unreadableDetailsText(unreadableRows(hosts))).toBe(
      [
        'Unreadable session state · 3 files on 2 hosts',
        'Local\t48213.json\tNot valid JSON: unexpected end of JSON input\tclaude · pid 48213, running, not in a pane',
        'Local\t51007.json\tsessionId missing or not a session ID: sessionId: "../x"\tclaude · pid 51007, running in tmux task-1c7b8d2f',
        'gpu-1\tlegacy.json\tpid missing or not positive\tunknown, file name is not <pid>.json',
      ].join('\n'),
    )
  })
})
