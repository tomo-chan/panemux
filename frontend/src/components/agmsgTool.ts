import { AgmsgToolSchema } from '../schemas'
import { postTaskAction, requestTaskAction } from '../hooks/useTasks'
import type { HostMenuTool } from './HostTools'

const load = (host: string) => requestTaskAction(`/api/hosts/agmsg?host=${encodeURIComponent(host)}`, undefined, AgmsgToolSchema)
const install = (host: string) => postTaskAction('/api/hosts/agmsg', { host }, AgmsgToolSchema)

export function agmsgTool(): HostMenuTool {
  return {
    id: 'agmsg', title: 'agmsg',
    absent: () => 'Not installed',
    outdated: 'Older than panemux’s tested version',
    consent: 'Install / Update runs panemux’s tested agmsg installer. It can write outside ~/.agents/skills, including agent settings. Existing agent scripts keep running during updates.',
    installedNotice: 'Restart agents to use the installed scripts. Agent Board joins from its next poll without restarting panemux.',
    autoUpdatedNotice: '',
    load, install,
  }
}
