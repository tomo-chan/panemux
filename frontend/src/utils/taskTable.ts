import type { AgentInfo, Task, TaskSort } from '../schemas'
import { TASK_COLUMNS, columnForTask, elapsedSeconds, hostLabel, taskTitle, type TaskLane } from './taskBoard'

const collator = new Intl.Collator('en', { numeric: true, sensitivity: 'base' })

export function nextTaskSort(current: TaskSort, key: TaskSort['key']): TaskSort {
  return { key, direction: current.key === key ? (current.direction === 'asc' ? 'desc' : 'asc') : key === 'elapsed' ? 'desc' : 'asc' }
}

/** One clock per data/filter/sort update; callers need not re-sort on display ticks. */
export function sortTasks(tasks: Task[], sort: TaskSort, now: number, agents: AgentInfo[] = []): Task[] {
  const value = (task: Task): string | number | null => {
    switch (sort.key) {
      case 'session': return taskTitle(task)
      case 'state': return TASK_COLUMNS.findIndex((column) => column.id === columnForTask(task))
      case 'elapsed': return elapsedSeconds(task.status_since, now)
      case 'host': return hostLabel(task.host)
      case 'agent': return agents.find((agent) => agent.id === task.agent)?.display_name || task.agent || null
      case 'repository': return task.git?.repo || null
      case 'branch': return task.git?.branch || null
    }
  }
  const fixedIdOrder = (a: Task, b: Task) => a.id < b.id ? -1 : a.id > b.id ? 1 : 0
  return [...tasks].sort((a, b) => {
    const av = value(a)
    const bv = value(b)
    if (av === null || bv === null) {
      return av === bv ? fixedIdOrder(a, b) : av === null ? 1 : -1
    }
    const compared = typeof av === 'number' && typeof bv === 'number' ? av - bv : collator.compare(String(av), String(bv))
    return compared ? compared * (sort.direction === 'asc' ? 1 : -1) : fixedIdOrder(a, b)
  })
}

/** Keep group identity/order, including label duplicates and catch-all groups. */
export function sortTaskGroups(groups: TaskLane[], sort: TaskSort, now: number, agents: AgentInfo[] = []): TaskLane[] {
  return groups.map((group) => ({ ...group, tasks: sortTasks(group.tasks, sort, now, agents) }))
}
